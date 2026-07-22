package model

import "time"

type AvailabilitySample struct {
	ID        uint64    `gorm:"primaryKey" json:"-"`
	CheckedAt time.Time `gorm:"index;not null" json:"checked_at"`
	Available bool      `gorm:"index;not null" json:"available"`
	DBOK      bool      `gorm:"not null" json:"-"`
	RedisOK   bool      `gorm:"not null" json:"-"`
	LatencyMs int       `gorm:"not null" json:"latency_ms"`
}
