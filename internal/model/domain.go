package model

import "time"

type Domain struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`
	Hostname   string    `gorm:"size:255;uniqueIndex;not null" json:"hostname"`
	Purpose    string    `gorm:"size:32;index" json:"purpose"`
	IsDefault  bool      `gorm:"default:false;index" json:"is_default"`
	ForceHTTPS bool      `gorm:"default:true" json:"force_https"`
	Enabled    bool      `gorm:"default:true;index" json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
