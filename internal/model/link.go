package model

import "time"

type Link struct {
	ID                uint64     `gorm:"primaryKey" json:"id"`
	ShortCode         string     `gorm:"size:32;uniqueIndex;not null" json:"short_code"`
	OriginalURLEnc    []byte     `gorm:"type:blob;not null" json:"-"`
	Nonce             []byte     `gorm:"type:blob;not null" json:"-"`
	ExpiresAt         *time.Time `json:"expires_at"`
	PasswordHash      string     `gorm:"size:255" json:"-"`
	IsOnce            bool       `gorm:"default:false" json:"is_once"`
	UsedAt            *time.Time `json:"used_at,omitempty"`
	Status            int        `gorm:"default:1" json:"status"`     // 1=active, 0=disabled
	Visibility        int        `gorm:"default:1" json:"visibility"` // 0=private, 1=public, 2=password
	EditToken         string     `gorm:"size:64;index" json:"-"`      // secret token for user to edit/delete
	NormalizedHost    string     `gorm:"size:255;index" json:"normalized_host,omitempty"`
	RiskScore         int        `gorm:"default:0" json:"risk_score"`
	RiskLevel         string     `gorm:"size:20;index" json:"risk_level,omitempty"`
	RiskReasons       string     `gorm:"type:text" json:"risk_reasons,omitempty"`
	RequiresConfirm   bool       `gorm:"default:false" json:"requires_confirm"`
	QRText            string     `gorm:"size:255" json:"qr_text,omitempty"`
	QRTemplate        string     `gorm:"size:64" json:"qr_template,omitempty"`
	CreatedByAPIKeyID *uint64    `gorm:"index" json:"created_by_api_key_id,omitempty"`
	CreatedByIPHash   string     `gorm:"size:255;index" json:"-"`
	DomainID          *uint64    `gorm:"index" json:"domain_id,omitempty"`
	ClickCount        int64      `gorm:"default:0" json:"click_count"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// RecycledCode holds a deleted short code during its cooldown period.
type RecycledCode struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`
	ShortCode  string    `gorm:"size:32;uniqueIndex;not null" json:"short_code"`
	ClickCount int64     `json:"click_count"` // usage at time of deletion
	DeletedAt  time.Time `json:"deleted_at"`
	ReleasesAt time.Time `json:"releases_at"` // when the code becomes available again
}

// Report represents a user report on a link.
type Report struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`
	ShortCode  string    `gorm:"size:32;index;not null" json:"short_code"`
	Reason     string    `gorm:"size:100;not null" json:"reason"`
	CustomText string    `gorm:"size:500" json:"custom_text,omitempty"`
	ReporterIP string    `gorm:"size:64" json:"-"`
	Status     int       `gorm:"default:0" json:"status"`             // 0=pending, 1=approved(deleted), 2=rejected
	HandledBy  string    `gorm:"size:50" json:"handled_by,omitempty"` // "manual" or "ai"
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ReportBan tracks IPs banned from submitting reports.
type ReportBan struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	IPHash    string    `gorm:"size:255;uniqueIndex;not null" json:"-"`
	Reason    string    `gorm:"size:100" json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}
