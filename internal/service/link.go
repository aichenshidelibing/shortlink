package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"shortlink/internal/config"
	"shortlink/internal/crypto"
	"shortlink/internal/model"
	"shortlink/internal/repository"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type LinkService struct {
	cfg       *config.ShortlinkConfig
	repo      *repository.LinkRepository
	cache     *repository.CacheRepository
	adminRepo *repository.AdminRepository
	strong    *crypto.StrongCrypto
	log       *zap.Logger
}

func NewLinkService(cfg *config.ShortlinkConfig, repo *repository.LinkRepository, cache *repository.CacheRepository, adminRepo *repository.AdminRepository, strong *crypto.StrongCrypto, log *zap.Logger) *LinkService {
	return &LinkService{
		cfg:       cfg,
		repo:      repo,
		cache:     cache,
		adminRepo: adminRepo,
		strong:    strong,
		log:       log,
	}
}

// getCodeLength reads the current shortlink length from DB settings (dynamic),
// falling back to config default.
func (s *LinkService) getCodeLength(ctx context.Context) int {
	length := s.cfg.DefaultLength
	settings, err := s.adminRepo.GetSettings(ctx)
	if err == nil && settings != nil && settings.ShortlinkLength >= 4 {
		length = settings.ShortlinkLength
	}
	return clampCodeLength(length)
}

func clampCodeLength(length int) int {
	if length < 4 {
		return 4
	}
	if length > 12 {
		return 12
	}
	return length
}

func (s *LinkService) currentAdminSuffix(ctx context.Context) string {
	if s.adminRepo == nil {
		return ""
	}
	settings, err := s.adminRepo.GetSettings(ctx)
	if err != nil || settings == nil {
		return ""
	}
	return settings.Suffix
}

func isSafeShortCode(code string) bool {
	if code == "" || strings.TrimSpace(code) != code {
		return false
	}
	for _, r := range code {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func isReservedShortCode(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "api", "manage", "help", "index.html", "__admin", "dashboard", "favicon.ico", "robots.txt", "assets", "static":
		return true
	default:
		return false
	}
}

func normalizeQRTemplate(template string) string {
	switch strings.ToLower(strings.TrimSpace(template)) {
	case "classic", "card", "compact":
		return strings.ToLower(strings.TrimSpace(template))
	default:
		return "classic"
	}
}

func (s *LinkService) validateCustomCode(ctx context.Context, code string) error {
	if !isSafeShortCode(code) {
		return fmt.Errorf("custom code may only contain letters, numbers, hyphen, and underscore")
	}
	if isReservedShortCode(code) {
		return fmt.Errorf("custom code conflicts with reserved route")
	}
	if suffix := s.currentAdminSuffix(ctx); suffix != "" && strings.EqualFold(code, suffix) {
		return fmt.Errorf("custom code conflicts with admin suffix")
	}
	if len(code) < s.cfg.MinCustomLength || len(code) > s.cfg.MaxCustomLength {
		return fmt.Errorf("custom code length must be between %d and %d", s.cfg.MinCustomLength, s.cfg.MaxCustomLength)
	}
	exists, err := s.repo.CodeExists(ctx, code)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("custom code already exists")
	}
	return nil
}

func generateEditToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randomUint64() (uint64, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return 0, err
	}
	// Ensure high bit is 0 for SQLite compatibility
	b[0] &= 0x7f
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7]), nil
}

// CreateResult holds data returned after link creation.
type CreateResult struct {
	Link      *model.Link
	EditToken string // only returned once
}

type CreateOptions struct {
	OriginalURL       string
	CustomCode        string
	Password          string
	ExpiresAt         *time.Time
	IsOnce            bool
	Visibility        int
	CreatedByAPIKeyID *uint64
	CreatedByIPHash   string
	DomainID          *uint64
}

func (s *LinkService) Create(ctx context.Context, originalURL, customCode, password string, expiresAt *time.Time, isOnce bool, visibility int) (*CreateResult, error) {
	return s.CreateWithOptions(ctx, CreateOptions{OriginalURL: originalURL, CustomCode: customCode, Password: password, ExpiresAt: expiresAt, IsOnce: isOnce, Visibility: visibility})
}

func (s *LinkService) CreateWithOptions(ctx context.Context, opts CreateOptions) (*CreateResult, error) {
	normalized, err := NormalizeDestinationURL(opts.OriginalURL, false)
	if err != nil {
		return nil, err
	}

	visibility := opts.Visibility
	if visibility < 0 || visibility > 2 {
		visibility = 1 // default public
	}

	var code string
	if opts.CustomCode != "" {
		if err := s.validateCustomCode(ctx, opts.CustomCode); err != nil {
			return nil, err
		}
		code = opts.CustomCode
	} else {
		// Generate random code of exact length (dynamic from DB settings)
		const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		length := s.getCodeLength(ctx)
		if length < 4 {
			length = 4
		}
		if length > 12 {
			length = 12
		}
		for i := 0; i < 10; i++ {
			b := make([]byte, length)
			for j := range b {
				n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
				b[j] = chars[n.Int64()]
			}
			candidate := string(b)
			if isReservedShortCode(candidate) {
				continue
			}
			if suffix := s.currentAdminSuffix(ctx); suffix != "" && strings.EqualFold(candidate, suffix) {
				continue
			}
			exists, err := s.repo.CodeExists(ctx, candidate)
			if err != nil {
				return nil, err
			}
			if !exists {
				code = candidate
				break
			}
		}
		if code == "" {
			return nil, fmt.Errorf("failed to generate unique code")
		}
	}

	urlEnc, nonce, err := s.strong.Encrypt([]byte(normalized.URL))
	if err != nil {
		return nil, fmt.Errorf("encrypt url: %w", err)
	}

	var pwHash string
	if opts.Password != "" {
		h, err := bcrypt.GenerateFromPassword([]byte(opts.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("hash password: %w", err)
		}
		pwHash = string(h)
		visibility = 2 // password-protected
	}

	editToken, err := generateEditToken()
	if err != nil {
		return nil, fmt.Errorf("generate edit token: %w", err)
	}
	id, err := randomUint64()
	if err != nil {
		return nil, fmt.Errorf("generate link id: %w", err)
	}

	link := &model.Link{
		ID:                id,
		ShortCode:         code,
		OriginalURLEnc:    urlEnc,
		Nonce:             nonce,
		ExpiresAt:         opts.ExpiresAt,
		PasswordHash:      pwHash,
		IsOnce:            opts.IsOnce,
		Visibility:        visibility,
		EditToken:         editToken,
		NormalizedHost:    normalized.Host,
		RiskLevel:         "safe",
		CreatedByAPIKeyID: opts.CreatedByAPIKeyID,
		CreatedByIPHash:   opts.CreatedByIPHash,
		DomainID:          opts.DomainID,
	}

	if err := s.repo.Create(ctx, link); err != nil {
		return nil, err
	}

	return &CreateResult{Link: link, EditToken: editToken}, nil
}

func (s *LinkService) Resolve(ctx context.Context, code string) (string, *model.Link, error) {
	// Cache only stores the decrypted URL; we still fetch the row on cache hits
	// so expiration/status checks and click accounting remain correct.
	cachedURL, cacheErr := s.cache.GetLink(ctx, code)

	link, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return "", nil, fmt.Errorf("link not found")
	}

	if link.Status != 1 {
		return "", nil, fmt.Errorf("link disabled")
	}

	if link.ExpiresAt != nil && time.Now().After(*link.ExpiresAt) {
		go func(code string) {
			_ = s.repo.Delete(context.Background(), code)
			s.cache.DeleteLink(context.Background(), code)
		}(code)
		return "", nil, fmt.Errorf("link expired")
	}

	// A previously-consumed one-shot link is left in the DB with status=0
	// (see MarkUsed) — but if status was flipped some other way we still
	// want to reject already-used ones cleanly.
	if link.IsOnce && link.UsedAt != nil {
		return "", nil, fmt.Errorf("link already used")
	}

	if cacheErr == nil && cachedURL != "" {
		return cachedURL, link, nil
	}

	url, err := s.strong.Decrypt(link.OriginalURLEnc, link.Nonce)
	if err != nil {
		return "", nil, fmt.Errorf("decrypt failed")
	}

	// Only cache links that don't need per-request gating and never expire.
	// Expiring links must always hit the DB so they stop redirecting on time.
	if !link.IsOnce && link.PasswordHash == "" && link.ExpiresAt == nil {
		s.cache.SetLink(ctx, code, string(url), 24*time.Hour)
	}
	return string(url), link, nil
}

// ConsumeOnce atomically marks a one-shot link as used. Returns true iff
// this call was the one that flipped it (i.e. the caller "won" the race).
// The redirect handler must invoke this ONLY after any password gate has
// been passed, so unsuccessful password attempts don't burn the link.
func (s *LinkService) ConsumeOnce(ctx context.Context, code string) (bool, error) {
	return s.repo.MarkUsed(ctx, code)
}

type UserUpdateOptions struct {
	URL           string
	Password      *string
	ClearPassword bool
	ExpiresAt     *time.Time
	SetExpiresAt  bool
	IsOnce        *bool
	Visibility    *int
	QRText        *string
	QRTemplate    *string
}

// EditURL updates the destination URL of a link, verified by edit token.
func (s *LinkService) EditURL(ctx context.Context, code, editToken, newURL string) error {
	return s.UpdateByUser(ctx, code, editToken, UserUpdateOptions{URL: newURL})
}

func (s *LinkService) GetByEditToken(ctx context.Context, code, editToken string) (*model.Link, string, error) {
	link, err := s.repo.GetByCodeAndToken(ctx, code, editToken)
	if err != nil {
		return nil, "", fmt.Errorf("link not found or edit token invalid")
	}
	return link, s.DecryptURL(link), nil
}

func (s *LinkService) UpdateByUser(ctx context.Context, code, editToken string, opts UserUpdateOptions) error {
	link, err := s.repo.GetByCodeAndToken(ctx, code, editToken)
	if err != nil {
		return fmt.Errorf("link not found or edit token invalid")
	}

	updates := map[string]interface{}{}
	if strings.TrimSpace(opts.URL) != "" {
		normalized, err := NormalizeDestinationURL(opts.URL, false)
		if err != nil {
			return err
		}
		urlEnc, nonce, err := s.strong.Encrypt([]byte(normalized.URL))
		if err != nil {
			return fmt.Errorf("encrypt url: %w", err)
		}
		updates["original_url_enc"] = urlEnc
		updates["nonce"] = nonce
		updates["normalized_host"] = normalized.Host
		updates["risk_score"] = 0
		updates["risk_level"] = "safe"
		updates["risk_reasons"] = ""
		updates["requires_confirm"] = false
	}
	if opts.Password != nil {
		pw := strings.TrimSpace(*opts.Password)
		if pw == "" || opts.ClearPassword {
			updates["password_hash"] = ""
			if link.Visibility == 2 {
				updates["visibility"] = 1
			}
		} else {
			h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("hash password: %w", err)
			}
			updates["password_hash"] = string(h)
			updates["visibility"] = 2
		}
	} else if opts.ClearPassword {
		updates["password_hash"] = ""
		if link.Visibility == 2 {
			updates["visibility"] = 1
		}
	}
	if opts.SetExpiresAt {
		updates["expires_at"] = opts.ExpiresAt
	}
	if opts.IsOnce != nil {
		updates["is_once"] = *opts.IsOnce
		if !*opts.IsOnce {
			updates["used_at"] = nil
			updates["status"] = 1
		}
	}
	if opts.Visibility != nil {
		v := *opts.Visibility
		if v < 0 || v > 2 {
			v = 1
		}
		updates["visibility"] = v
	}
	if opts.QRText != nil {
		text := strings.TrimSpace(*opts.QRText)
		if len([]rune(text)) > 120 {
			return fmt.Errorf("qr text too long")
		}
		updates["qr_text"] = text
	}
	if opts.QRTemplate != nil {
		updates["qr_template"] = normalizeQRTemplate(*opts.QRTemplate)
	}
	if len(updates) == 0 {
		return nil
	}
	if err := s.repo.UpdateByCode(ctx, link.ShortCode, updates); err != nil {
		return err
	}
	s.cache.DeleteLink(ctx, code)
	return nil
}

// DeleteByUser deletes a link verified by edit token, with recycling cooldown.
func (s *LinkService) DeleteByUser(ctx context.Context, code, editToken string) error {
	link, err := s.repo.GetByCodeAndToken(ctx, code, editToken)
	if err != nil {
		return fmt.Errorf("link not found or edit token invalid")
	}

	// Calculate cooldown: 30–180 days based on click count and age
	cooldown := s.calculateCooldown(link)

	if err := s.repo.Delete(ctx, code); err != nil {
		return err
	}
	s.cache.DeleteLink(ctx, code)

	// Recycle the code
	if err := s.repo.AddRecycled(ctx, code, link.ClickCount, cooldown); err != nil {
		s.log.Warn("recycle code failed", zap.String("code", code), zap.Error(err))
	}
	return nil
}

func (s *LinkService) calculateCooldown(link *model.Link) int {
	// Base: 30 days
	days := 30
	// +1 day per 10 clicks, max +60
	clickBonus := int(link.ClickCount / 10)
	if clickBonus > 60 {
		clickBonus = 60
	}
	days += clickBonus
	// +1 day per week of existence, max +30
	ageWeeks := int(time.Since(link.CreatedAt).Hours() / (24 * 7))
	if ageWeeks > 30 {
		ageWeeks = 30
	}
	days += ageWeeks
	if days > 180 {
		days = 180
	}
	return days
}

// CleanupExpired deletes all expired links.
func (s *LinkService) CleanupExpired(ctx context.Context) (int64, error) {
	n, err := s.repo.DeleteExpired(ctx)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		s.log.Info("cleaned expired links", zap.Int64("count", n))
	}
	return n, nil
}

// ReleaseCodes releases short codes past their cooldown period.
func (s *LinkService) ReleaseCodes(ctx context.Context) (int64, error) {
	n, err := s.repo.ReleaseExpiredCodes(ctx)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		s.log.Info("released recycled codes", zap.Int64("count", n))
	}
	return n, nil
}

func (s *LinkService) VerifyPassword(link *model.Link, password string) bool {
	if link.PasswordHash == "" {
		return true
	}
	return bcrypt.CompareHashAndPassword([]byte(link.PasswordHash), []byte(password)) == nil
}

func (s *LinkService) MarkRisk(ctx context.Context, code string, score int, level string, reasons string, requiresConfirm bool) error {
	return s.repo.UpdateByCode(ctx, code, map[string]interface{}{
		"risk_score": score, "risk_level": level, "risk_reasons": reasons, "requires_confirm": requiresConfirm,
	})
}

func (s *LinkService) Delete(ctx context.Context, code string) error {
	if err := s.repo.Delete(ctx, code); err != nil {
		return err
	}
	s.cache.DeleteLink(ctx, code)
	return nil
}

func (s *LinkService) List(ctx context.Context, page, size int) ([]model.Link, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	return s.repo.List(ctx, (page-1)*size, size)
}

func (s *LinkService) DecryptURL(link *model.Link) string {
	url, _ := s.strong.Decrypt(link.OriginalURLEnc, link.Nonce)
	return string(url)
}

// SumClicks — total click_count across all links (dashboard tile).
func (s *LinkService) SumClicks(ctx context.Context) (int64, error) {
	return s.repo.SumClicks(ctx)
}
