package model

import "time"

type Admin struct {
	ID           int    `gorm:"primaryKey;autoIncrement" json:"-"`
	Username     string `gorm:"size:64;not null" json:"username"`
	PasswordHash string `gorm:"size:255;not null" json:"-"`
	// Stored as CryptoManager v2 payload ("v2:<nonce_b64>:<ct_b64>"), so
	// the raw 32-char base32 secret balloons to ~110 chars. Give it
	// plenty of headroom — legacy plaintext values still fit.
	TOTPSecret   string    `gorm:"size:255" json:"-"`
	TOTPVerified bool      `gorm:"default:false" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type AdminSetting struct {
	ID                int       `gorm:"primaryKey;autoIncrement" json:"-"`
	Suffix            string    `gorm:"size:32" json:"suffix"`
	SuffixChangedAt   time.Time `json:"suffix_changed_at"`
	ShortlinkLength   int       `gorm:"default:6" json:"shortlink_length"`
	SettingsEnc       string    `gorm:"type:text" json:"-"`
	UpdatedAt         time.Time `json:"updated_at"`
}
