package model

import "time"

type BannedIP struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	IPHash    string    `gorm:"size:255;not null" json:"-"`
	IPEnc     []byte    `gorm:"type:blob" json:"-"`
	IPNonce   []byte    `gorm:"type:blob" json:"-"`
	Reason    string    `gorm:"size:50" json:"reason"`
	BanUntil  time.Time `json:"ban_until"`
	CreatedAt time.Time `json:"created_at"`
}
