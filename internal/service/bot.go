package service

import (
	"context"
	"fmt"
	"shortlink/internal/model"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// BotMode defines how the auto-moderation bot handles reports.
type BotMode string

const (
	BotOff         BotMode = "off"
	BotApproveAll  BotMode = "approve_all"
	BotRejectAll   BotMode = "reject_all"
	BotSmart       BotMode = "smart"
)

// BotConfig is stored in admin settings JSON.
type BotConfig struct {
	Mode          BotMode `json:"bot_mode"`
	MinTrust      float64 `json:"bot_min_trust"`      // minimum trust score to auto-approve
	MinReports    int64   `json:"bot_min_reports"`     // min reports before auto-deciding
	MaxAutoDaily  int64   `json:"bot_max_auto_daily"`  // max auto-decisions per day
}

var DefaultBotConfig = BotConfig{
	Mode:         BotOff,
	MinTrust:     0.7,
	MinReports:   5,
	MaxAutoDaily: 100,
}

// ReportBot handles automated report decisions with trust-based scoring.
type ReportBot struct {
	db  *gorm.DB
	log *zap.Logger
}

func NewReportBot(db *gorm.DB, log *zap.Logger) *ReportBot {
	return &ReportBot{db: db, log: log}
}

// Decide determines what to do with a report.
// Returns: action ("approve", "reject", "pending"), reason string
func (b *ReportBot) Decide(ctx context.Context, cfg *BotConfig, report *model.Report, decryptedURL string, safeScan *ScanResult, aiFlagged bool) (string, string) {
	if cfg == nil {
		cfg = &DefaultBotConfig
	}

	switch cfg.Mode {
	case BotOff:
		return "pending", "bot disabled"

	case BotApproveAll:
		// Auto-approve everything — dangerous, admin explicitly chose this
		return "approve", "bot: approve_all mode"

	case BotRejectAll:
		return "reject", "bot: reject_all mode"

	case BotSmart:
		return b.smartDecide(ctx, cfg, report, decryptedURL, safeScan, aiFlagged)
	}

	return "pending", "unknown mode"
}

func (b *ReportBot) smartDecide(ctx context.Context, cfg *BotConfig, report *model.Report, decryptedURL string, safeScan *ScanResult, aiFlagged bool) (string, string) {
	// Get reporter stats
	stats := b.getOrCreateStats(ctx, report.ReporterIP)

	// Check daily auto-decision limit
	todayAuto := stats.AutoApproved + stats.AutoRejected
	if todayAuto >= cfg.MaxAutoDaily {
		return "pending", "bot: daily auto limit reached"
	}

	// === HIGH CONFIDENCE: auto-approve ===

	// Safe scanner found issues AND AI flagged → auto-approve (delete)
	if safeScan != nil && !safeScan.Safe && aiFlagged {
		b.updateStats(ctx, report.ReporterIP, true)
		return "approve", fmt.Sprintf("bot: scanner+ai both flagged (score=%d)", safeScan.Score)
	}

	// Safe scanner found severe issues → auto-approve
	if safeScan != nil && safeScan.Score >= 60 {
		b.updateStats(ctx, report.ReporterIP, true)
		return "approve", fmt.Sprintf("bot: high risk score %d", safeScan.Score)
	}

	// AI flagged → auto-approve
	if aiFlagged {
		b.updateStats(ctx, report.ReporterIP, true)
		return "approve", "bot: AI moderation flagged"
	}

	// === TRUST-BASED ===

	// High-trust reporter → auto-approve
	if stats.TotalReports >= cfg.MinReports && stats.TrustScore >= cfg.MinTrust {
		b.updateStats(ctx, report.ReporterIP, true)
		return "approve", fmt.Sprintf("bot: trusted reporter (score=%.0f%%)", stats.TrustScore*100)
	}

	// Low-trust reporter → auto-reject
	if stats.TotalReports >= cfg.MinReports && stats.TrustScore <= 0.2 {
		b.updateStats(ctx, report.ReporterIP, false)
		return "reject", fmt.Sprintf("bot: low-trust reporter (score=%.0f%%)", stats.TrustScore*100)
	}

	// First-time reporter with no evidence → pending
	return "pending", "bot: insufficient evidence, needs review"
}

func (b *ReportBot) getOrCreateStats(ctx context.Context, ipHash string) *model.ReporterStats {
	var stats model.ReporterStats
	err := b.db.WithContext(ctx).Where("ip_hash = ?", ipHash).First(&stats).Error
	if err != nil {
		stats = model.ReporterStats{
			IPHash:     ipHash,
			TrustScore: 0.5, // neutral starting trust
		}
		b.db.WithContext(ctx).Create(&stats)
	}
	return &stats
}

func (b *ReportBot) updateStats(ctx context.Context, ipHash string, approved bool) {
	updates := map[string]interface{}{
		"total_reports": gorm.Expr("total_reports + 1"),
	}
	if approved {
		updates["approved_count"] = gorm.Expr("approved_count + 1")
		updates["auto_approved"] = gorm.Expr("auto_approved + 1")
	} else {
		updates["rejected_count"] = gorm.Expr("rejected_count + 1")
		updates["auto_rejected"] = gorm.Expr("auto_rejected + 1")
	}
	// Update trust score: approved / (approved + rejected)
	updates["trust_score"] = gorm.Expr(
		"CASE WHEN (approved_count + rejected_count) > 0 THEN CAST(approved_count AS REAL) / (approved_count + rejected_count) ELSE 0.5 END")

	b.db.WithContext(ctx).Model(&model.ReporterStats{}).Where("ip_hash = ?", ipHash).Updates(updates)
}

// UpdateStatsOnManual allows admin manual actions to affect reporter trust.
func (b *ReportBot) UpdateStatsOnManual(ctx context.Context, ipHash string, approved bool) {
	updates := map[string]interface{}{
		"total_reports": gorm.Expr("total_reports + 1"),
	}
	if approved {
		updates["approved_count"] = gorm.Expr("approved_count + 1")
	} else {
		updates["rejected_count"] = gorm.Expr("rejected_count + 1")
	}
	// Recalculate trust
	updates["trust_score"] = gorm.Expr(
		"CASE WHEN (approved_count + rejected_count) > 0 THEN CAST(approved_count AS REAL) / (approved_count + rejected_count) ELSE 0.5 END")

	result := b.db.WithContext(ctx).Model(&model.ReporterStats{}).Where("ip_hash = ?", ipHash).Updates(updates)
	if result.RowsAffected == 0 {
		// First-time — create with initial trust
		trust := 0.5
		if approved {
			trust = 0.7
		} else {
			trust = 0.3
		}
		b.db.WithContext(ctx).Create(&model.ReporterStats{
			IPHash:        ipHash,
			TotalReports:  1,
			TrustScore:    trust,
			ApprovedCount: boolToInt(approved),
			RejectedCount: boolToInt(!approved),
		})
	}
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
