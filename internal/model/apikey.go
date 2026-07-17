package model

import "time"

type APIKey struct {
	ID              uint64     `gorm:"primaryKey" json:"id"`
	KeyHash         string     `gorm:"size:255;uniqueIndex;not null" json:"-"`
	Name            string     `gorm:"size:100" json:"name"`
	Purpose         string     `gorm:"size:255" json:"purpose"`
	PermissionsJSON string     `gorm:"type:text" json:"permissions_json"`
	QuotaPerMinute  int        `gorm:"default:0" json:"quota_per_minute"`
	QuotaPerDay     int        `gorm:"default:0" json:"quota_per_day"`
	QuotaPerMonth   int        `gorm:"default:0" json:"quota_per_month"`
	AllowedDomains  string     `gorm:"type:text" json:"allowed_domains,omitempty"`
	DeniedDomains   string     `gorm:"type:text" json:"denied_domains,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at"`
	Revoked         bool       `gorm:"default:false" json:"revoked"`
	LastUsedAt      *time.Time `json:"last_used_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
