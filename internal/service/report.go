package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"shortlink/internal/model"
	"shortlink/internal/repository"
	"sync"
	"time"

	"go.uber.org/zap"
)

type ReportService struct {
	repo    *repository.LinkRepository
	scanner *SafeScanner
	modSvc  *ModerationService
	bot     *ReportBot
	log     *zap.Logger

	mu         sync.Mutex
	lastReport map[string]time.Time
}

func NewReportService(repo *repository.LinkRepository, scanner *SafeScanner, modSvc *ModerationService, bot *ReportBot, log *zap.Logger) *ReportService {
	return &ReportService{
		repo:       repo,
		scanner:    scanner,
		modSvc:     modSvc,
		bot:        bot,
		log:        log,
		lastReport: make(map[string]time.Time),
	}
}

func hashReporterIP(ip string) string {
	h := sha256.Sum256([]byte(ip))
	return hex.EncodeToString(h[:])
}

// ReportConfig — stored in admin settings JSON.
type ReportConfig struct {
	DailyLimit   int `json:"report_daily_limit"`
	MinInterval  int `json:"report_min_interval"`
	AutoBanAfter int `json:"report_auto_ban"`
}

var DefaultReportConfig = ReportConfig{
	DailyLimit:   20,
	MinInterval:  5,
	AutoBanAfter: 10,
}

func (s *ReportService) Submit(ctx context.Context, shortCode, reason, customText, ip string) error {
	ipHash := hashReporterIP(ip)

	banned, err := s.repo.IsReportBanned(ctx, ipHash)
	if err != nil {
		return err
	}
	if banned {
		return fmt.Errorf("your report privilege has been revoked")
	}

	count, err := s.repo.CountReportsToday(ctx, ipHash)
	if err != nil {
		return err
	}
	if count >= int64(DefaultReportConfig.DailyLimit) {
		return fmt.Errorf("daily report limit reached")
	}

	s.mu.Lock()
	last, exists := s.lastReport[ip]
	if exists && time.Since(last) < time.Duration(DefaultReportConfig.MinInterval)*time.Second {
		s.mu.Unlock()
		return fmt.Errorf("please wait before submitting another report")
	}
	s.lastReport[ip] = time.Now()
	s.mu.Unlock()

	link, err := s.repo.GetByCode(ctx, shortCode)
	if err != nil {
		return fmt.Errorf("link not found or already removed")
	}
	_ = link

	report := &model.Report{
		ShortCode:  shortCode,
		Reason:     reason,
		CustomText: customText,
		ReporterIP: ipHash,
	}

	if err := s.repo.CreateReport(ctx, report); err != nil {
		return err
	}

	s.log.Info("report submitted", zap.String("code", shortCode), zap.String("reason", reason))
	return nil
}

func (s *ReportService) List(ctx context.Context, status int) ([]model.Report, error) {
	return s.repo.ListReports(ctx, status)
}

func (s *ReportService) Handle(ctx context.Context, id uint64, approved bool, handledBy string) error {
	status := 2
	if approved {
		status = 1
	}
	// Update reporter stats for bot learning
	reports, _ := s.repo.ListReports(ctx, -1)
	for _, r := range reports {
		if r.ID == id {
			s.bot.UpdateStatsOnManual(ctx, r.ReporterIP, approved)
			break
		}
	}
	return s.repo.UpdateReportStatus(ctx, id, status, handledBy)
}

func (s *ReportService) ApproveAndDelete(ctx context.Context, id uint64, linkSvc *LinkService) error {
	reports, err := s.repo.ListReports(ctx, 0)
	if err != nil {
		return err
	}
	var target *model.Report
	for i := range reports {
		if reports[i].ID == id {
			target = &reports[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("report not found")
	}

	if err := linkSvc.Delete(ctx, target.ShortCode); err != nil {
		s.log.Warn("delete reported link failed", zap.Error(err))
	}

	s.bot.UpdateStatsOnManual(ctx, target.ReporterIP, true)
	return s.repo.UpdateReportStatus(ctx, id, 1, "manual")
}

func (s *ReportService) BanReporterIP(ctx context.Context, ip string, reason string) error {
	ipHash := hashReporterIP(ip)
	return s.repo.BanReporter(ctx, ipHash, reason)
}

// BanReporterByHash bans a reporter whose IP hash is already known
// (e.g. from a stored Report record). Use this instead of BanReporterIP
// when the caller does not hold the plaintext IP.
func (s *ReportService) BanReporterByHash(ctx context.Context, ipHash string, reason string) error {
	return s.repo.BanReporter(ctx, ipHash, reason)
}

// AutoProcessReport runs the bot, scanner, and AI on a pending report.
// Returns the bot's decision and reason.
func (s *ReportService) AutoProcessReport(ctx context.Context, report *model.Report, decryptedURL string, cfg *BotConfig) (string, string) {
	// Run safe scanner
	scan := s.scanner.ScanURL(decryptedURL)

	// Run AI moderation
	aiFlagged, _ := s.modSvc.CheckURL(ctx, decryptedURL)

	// Let the bot decide
	return s.bot.Decide(ctx, cfg, report, decryptedURL, scan, aiFlagged)
}

func (s *ReportService) GetBot() *ReportBot {
	return s.bot
}
