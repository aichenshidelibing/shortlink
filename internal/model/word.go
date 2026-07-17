package model

import "time"

type WordRule struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	Word      string    `gorm:"size:255;not null" json:"word"`
	Level     int       `gorm:"default:1" json:"level"` // 1:light 2:medium 3:heavy
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

type WhiteList struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	Pattern   string    `gorm:"size:255;not null" json:"pattern"`
	IsRegex   bool      `gorm:"default:false" json:"is_regex"`
	Type      string    `gorm:"size:20;not null" json:"type"` // ip or word
	CreatedAt time.Time `json:"created_at"`
}
