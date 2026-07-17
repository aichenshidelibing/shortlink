package model

import "time"

type Click struct {
	ID            uint64    `gorm:"primaryKey" json:"id"`
	LinkID        uint64    `gorm:"not null;index:idx_link_time,priority:1" json:"link_id"`
	Country       string    `gorm:"size:10" json:"country"`
	Region        string    `gorm:"size:50" json:"region"`
	IPEnc         []byte    `gorm:"type:blob" json:"-"`
	IPNonce       []byte    `gorm:"type:blob" json:"-"`
	RefererHost   string    `gorm:"size:255;index" json:"referer_host,omitempty"`
	UserAgentHash string    `gorm:"size:64;index" json:"-"`
	VisitorHash   string    `gorm:"size:64;index" json:"-"`
	Browser       string    `gorm:"size:50" json:"browser,omitempty"`
	OS            string    `gorm:"size:50" json:"os,omitempty"`
	Device        string    `gorm:"size:50" json:"device,omitempty"`
	IsBot         bool      `gorm:"default:false" json:"is_bot"`
	Hour          int       `gorm:"index" json:"hour"`
	EventType     string    `gorm:"size:32;index" json:"event_type,omitempty"`
	ClickedAt     time.Time `gorm:"index:idx_link_time,priority:2" json:"clicked_at"`
}
