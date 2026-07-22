package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"math/big"
	"shortlink/internal/auth"
	"shortlink/internal/config"
	"shortlink/internal/crypto"
	"shortlink/internal/model"
	"shortlink/internal/repository"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	maxAdminSuffixLength        = 32
	scheduledSuffixRotationHour = 12
)

func localNoon(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, scheduledSuffixRotationHour, 0, 0, 0, t.Location())
}

func NextLocalNoonAfter(t time.Time) time.Time {
	noon := localNoon(t)
	if !t.Before(noon) {
		return noon.AddDate(0, 0, 1)
	}
	return noon
}

func suffixRotationDue(changedAt, now time.Time) bool {
	lastNoon := localNoon(now)
	if now.Before(lastNoon) {
		lastNoon = lastNoon.AddDate(0, 0, -1)
	}
	if changedAt.IsZero() {
		return !now.Before(lastNoon)
	}
	return changedAt.Before(lastNoon)
}

type AdminService struct {
	cfg     *config.AdminConfig
	repo    *repository.AdminRepository
	totp    *auth.TOTP
	session *auth.SessionManager
	crypto  *crypto.CryptoManager
	db      *gorm.DB
	rdb     *redis.Client
	log     *zap.Logger

	// suffixListener is invoked (synchronously) whenever the admin suffix
	// changes, so the HTTP dispatcher can start routing the new prefix
	// without a restart. Nil is fine — the setter is optional.
	suffixListener func(string)
}

func NewAdminService(cfg *config.AdminConfig, repo *repository.AdminRepository, totp *auth.TOTP, session *auth.SessionManager, crypto *crypto.CryptoManager, db *gorm.DB, rdb *redis.Client, log *zap.Logger) *AdminService {
	return &AdminService{
		cfg:     cfg,
		repo:    repo,
		totp:    totp,
		session: session,
		crypto:  crypto,
		db:      db,
		rdb:     rdb,
		log:     log,
	}
}

func (s *AdminService) GetDB() *gorm.DB {
	return s.db
}

// OnSuffixChange registers a callback invoked whenever the admin suffix
// changes (either via RotateSuffix or the initial GetOrCreateSuffix).
// Passing nil clears the listener.
func (s *AdminService) OnSuffixChange(fn func(string)) {
	s.suffixListener = fn
}

func (s *AdminService) notifySuffix(suffix string) {
	if s.suffixListener != nil && suffix != "" {
		s.suffixListener(suffix)
	}
}

func (s *AdminService) CheckRedis(ctx context.Context) (bool, string) {
	if s.rdb == nil {
		return false, "not configured"
	}
	if err := s.rdb.Ping(ctx).Err(); err != nil {
		return false, "disconnected: " + err.Error()
	}
	return true, "connected"
}

func (s *AdminService) InitAdmin(ctx context.Context) error {
	admin, err := s.repo.Get(ctx)
	if err != nil {
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(s.cfg.Password), bcrypt.DefaultCost)
		if hashErr != nil {
			return fmt.Errorf("hash password: %w", hashErr)
		}
		admin = &model.Admin{
			Username:     s.cfg.Username,
			PasswordHash: string(hash),
		}
		// Use FirstOrCreate to avoid race when multiple instances start simultaneously
		if err := s.repo.FirstOrCreate(ctx, admin); err != nil {
			return fmt.Errorf("create admin: %w", err)
		}
		// Re-fetch to get the actual record (might be from another instance)
		admin, err = s.repo.Get(ctx)
		if err != nil {
			return fmt.Errorf("get admin after create: %w", err)
		}
		secret, qrBytes, err := s.totp.GenerateSecret(admin.Username)
		if err != nil {
			return fmt.Errorf("generate totp: %w", err)
		}
		if err := s.repo.UpdateTOTPSecret(ctx, s.encryptTOTP(secret)); err != nil {
			return err
		}
		s.log.Info("Admin initialized",
			zap.String("username", admin.Username),
			zap.Bool("totp_setup_pending", true),
			zap.Int("qr_size", len(qrBytes)),
		)
	} else {
		if s.cfg.Password != "" {
			if hashErr := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(s.cfg.Password)); hashErr != nil {
				hash, err := bcrypt.GenerateFromPassword([]byte(s.cfg.Password), bcrypt.DefaultCost)
				if err != nil {
					return fmt.Errorf("hash password: %w", err)
				}
				if err := s.repo.UpdatePasswordHash(ctx, admin.ID, string(hash)); err != nil {
					return fmt.Errorf("update admin password: %w", err)
				}
				s.log.Info("admin password hash updated from config")
			}
		}
		if admin.TOTPSecret == "" {
			secret, qrBytes, err := s.totp.GenerateSecret(admin.Username)
			if err != nil {
				return fmt.Errorf("generate totp: %w", err)
			}
			if err := s.repo.UpdateTOTPSecret(ctx, s.encryptTOTP(secret)); err != nil {
				return err
			}
			s.log.Info("TOTP generated",
				zap.Bool("totp_setup_pending", true),
				zap.Int("qr_size", len(qrBytes)),
			)
		} else if !isEncryptedTOTP(admin.TOTPSecret) {
			// Legacy row from before TOTP secrets were encrypted at rest.
			// Silently upgrade so subsequent reads all go through decrypt.
			if err := s.repo.UpdateTOTPSecret(ctx, s.encryptTOTP(admin.TOTPSecret)); err != nil {
				s.log.Warn("upgrade legacy totp_secret failed", zap.Error(err))
			} else {
				s.log.Info("upgraded legacy TOTP secret to encrypted storage")
			}
		}
	}
	return nil
}

// encryptTOTP wraps a raw base32 TOTP secret with the "v2:" prefix so
// isEncryptedTOTP() can distinguish encrypted rows from legacy plaintext
// rows without a schema change. Falls back to plaintext on encrypt error
// so we never lose the secret entirely.
func (s *AdminService) encryptTOTP(secret string) string {
	if secret == "" {
		return ""
	}
	return s.crypto.Encrypt(secret) // CryptoManager already adds "v2:" prefix
}

// decryptTOTP is the reverse; also transparently handles a legacy
// plaintext value (no prefix) so existing deployments keep working during
// the one-shot upgrade path in InitAdmin.
func (s *AdminService) decryptTOTP(stored string) string {
	if stored == "" {
		return ""
	}
	if !isEncryptedTOTP(stored) {
		return stored
	}
	plain, err := s.crypto.Decrypt(stored)
	if err != nil {
		s.log.Warn("decrypt totp_secret failed", zap.Error(err))
		return ""
	}
	return plain
}

// isEncryptedTOTP returns true iff the stored value is a CryptoManager
// v2 payload (see internal/crypto/manager.go). Bare base32 secrets never
// contain the "v2:" prefix or colons.
func isEncryptedTOTP(stored string) bool {
	return len(stored) > 3 && stored[:3] == "v2:"
}

func (s *AdminService) GetAdmin(ctx context.Context) (*model.Admin, error) {
	return s.repo.Get(ctx)
}

func (s *AdminService) Login(ctx context.Context, username, password, totpCode string) (*model.Admin, error) {
	admin, err := s.repo.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin not found")
	}

	if subtle.ConstantTimeCompare([]byte(username), []byte(admin.Username)) != 1 {
		return nil, fmt.Errorf("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	// TOTP: only required if verified. The stored secret may be encrypted
	// (v2 payload) so we always route through decryptTOTP before handing
	// it to the OTP library.
	if admin.TOTPVerified && admin.TOTPSecret != "" {
		if !s.totp.Validate(totpCode, s.decryptTOTP(admin.TOTPSecret)) {
			return nil, fmt.Errorf("invalid credentials")
		}
	}
	return admin, nil
}

func (s *AdminService) VerifyTOTP(ctx context.Context, code string) error {
	admin, err := s.repo.Get(ctx)
	if err != nil {
		return fmt.Errorf("admin not found")
	}
	if admin.TOTPSecret == "" {
		return fmt.Errorf("no TOTP secret configured")
	}
	if !s.totp.Validate(code, s.decryptTOTP(admin.TOTPSecret)) {
		return fmt.Errorf("invalid TOTP code")
	}
	return s.repo.UpdateTOTPVerified(ctx, true)
}

func (s *AdminService) GenerateSuffix(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			// Fallback to a simple shuffle on error (should never happen)
			b[i] = chars[i%len(chars)]
			continue
		}
		b[i] = chars[n.Int64()]
	}
	return string(b)
}

func (s *AdminService) generateRandomInt(max int) int {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0
	}
	return int(n.Int64())
}

func (s *AdminService) EnsureSuffixDifferent(ctx context.Context, suffix string, shortlinkLen int) string {
	if suffix == "" {
		suffix = s.GenerateSuffix(8)
	}
	for len(suffix) == shortlinkLen && len(suffix) < maxAdminSuffixLength {
		remaining := maxAdminSuffixLength - len(suffix)
		extra := 1 + s.generateRandomInt(6)
		if extra > remaining {
			extra = remaining
		}
		suffix += s.GenerateSuffix(extra)
	}
	return suffix
}

func (s *AdminService) suffixCodeExists(ctx context.Context, suffix string) (bool, error) {
	if suffix == "" || s.db == nil {
		return false, nil
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.Link{}).Where("short_code = ?", suffix).Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}
	if err := s.db.WithContext(ctx).Model(&model.RecycledCode{}).Where("short_code = ? AND releases_at > ?", suffix, time.Now()).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *AdminService) ensureSuffixSafe(ctx context.Context, suffix string, shortlinkLen int) (string, error) {
	if suffix == "" {
		suffix = s.GenerateSuffix(8)
	}
	for i := 0; i < maxAdminSuffixLength; i++ {
		lengthConflict := len(suffix) == shortlinkLen
		codeConflict, err := s.suffixCodeExists(ctx, suffix)
		if err != nil {
			return "", err
		}
		if !lengthConflict && !codeConflict {
			return suffix, nil
		}
		if len(suffix) >= maxAdminSuffixLength {
			return s.GenerateSuffix(8) + s.GenerateSuffix(8), nil
		}
		remaining := maxAdminSuffixLength - len(suffix)
		extra := 1 + s.generateRandomInt(6)
		if extra > remaining {
			extra = remaining
		}
		suffix += s.GenerateSuffix(extra)
	}
	return suffix, nil
}

// ReconcileSuffixForShortlinkLength enforces the admin/shortlink separation
// rule after operators change public short-code length. It also avoids a real
// code collision where a custom short code equals the admin suffix.
func (s *AdminService) ReconcileSuffixForShortlinkLength(ctx context.Context, shortlinkLen int) (string, bool, error) {
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return "", false, err
	}
	suffix := settings.Suffix
	if suffix == "" {
		suffix = s.cfg.Username
		if suffix == "" {
			suffix = "admin"
		}
	}
	newSuffix, err := s.ensureSuffixSafe(ctx, suffix, shortlinkLen)
	if err != nil {
		return "", false, err
	}
	if newSuffix == suffix {
		return suffix, false, nil
	}
	settings.Suffix = newSuffix
	settings.SuffixChangedAt = time.Now()
	if err := s.repo.SaveSettings(ctx, settings); err != nil {
		return "", false, err
	}
	s.notifySuffix(newSuffix)
	return newSuffix, true, nil
}

func (s *AdminService) GetOrCreateSuffix(ctx context.Context) (string, error) {
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return "", err
	}

	if settings.Suffix != "" {
		return settings.Suffix, nil
	}

	// Default suffix = random on first init to avoid obvious admin paths.
	suffix := s.GenerateSuffix(8)
	suffix, err = s.ensureSuffixSafe(ctx, suffix, settings.ShortlinkLength)
	if err != nil {
		return "", err
	}

	settings.Suffix = suffix
	settings.SuffixChangedAt = time.Now()
	if err := s.repo.SaveSettings(ctx, settings); err != nil {
		return "", err
	}
	s.notifySuffix(suffix)
	return suffix, nil
}

func (s *AdminService) RotateSuffix(ctx context.Context) (string, error) {
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return "", err
	}

	suffix := s.GenerateSuffix(8)
	suffix, err = s.ensureSuffixSafe(ctx, suffix, settings.ShortlinkLength)
	if err != nil {
		return "", err
	}

	settings.Suffix = suffix
	settings.SuffixChangedAt = time.Now()
	if err := s.repo.SaveSettings(ctx, settings); err != nil {
		return "", err
	}
	s.notifySuffix(suffix)
	return suffix, nil
}

func (s *AdminService) RotateSuffixIfDue(ctx context.Context, now time.Time) (string, bool, error) {
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return "", false, err
	}
	if !suffixRotationDue(settings.SuffixChangedAt, now) {
		return settings.Suffix, false, nil
	}
	suffix, err := s.RotateSuffix(ctx)
	if err != nil {
		return "", false, err
	}
	return suffix, true, nil
}

func (s *AdminService) LoginTOTPRequired(ctx context.Context) (bool, error) {
	admin, err := s.repo.Get(ctx)
	if err != nil {
		return false, err
	}
	return admin.TOTPVerified && admin.TOTPSecret != "", nil
}

func (s *AdminService) GetSettings(ctx context.Context) (*model.AdminSetting, error) {
	return s.repo.GetSettings(ctx)
}

func (s *AdminService) SaveSettings(ctx context.Context, settings *model.AdminSetting) error {
	return s.repo.SaveSettings(ctx, settings)
}

func (s *AdminService) GetTOTPURI(ctx context.Context) string {
	admin, err := s.repo.Get(ctx)
	if err != nil || admin.TOTPSecret == "" {
		return ""
	}
	return s.totp.ProvisioningURI(admin.Username, s.decryptTOTP(admin.TOTPSecret))
}

func (s *AdminService) DecryptSettings(enc string) (string, error) {
	return s.crypto.Decrypt(enc)
}

func (s *AdminService) EncryptSettings(plain string) string {
	return s.crypto.Encrypt(plain)
}
