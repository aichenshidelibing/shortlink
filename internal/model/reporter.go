package model

import "time"

// ReporterStats tracks a reporter's accuracy for bot trust scoring.
type ReporterStats struct {
	IPHash         string    `gorm:"size:255;primaryKey" json:"-"`
	TotalReports   int64     `gorm:"default:0" json:"total_reports"`
	ApprovedCount  int64     `gorm:"default:0" json:"approved_count"`
	RejectedCount  int64     `gorm:"default:0" json:"rejected_count"`
	TrustScore     float64   `gorm:"default:0.5" json:"trust_score"` // 0.0-1.0
	AutoApproved   int64     `gorm:"default:0" json:"auto_approved"`
	AutoRejected   int64     `gorm:"default:0" json:"auto_rejected"`
	LastReportAt   time.Time `json:"last_report_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
