package model

import "time"

type AdminAuditLog struct {
	ID            uint64    `gorm:"primaryKey" json:"id"`
	ActorType     string    `gorm:"size:32;index" json:"actor_type"`
	ActorID       string    `gorm:"size:128;index" json:"actor_id"`
	Action        string    `gorm:"size:100;index" json:"action"`
	Resource      string    `gorm:"size:100;index" json:"resource"`
	ResourceID    string    `gorm:"size:128;index" json:"resource_id"`
	IPHash        string    `gorm:"size:255" json:"-"`
	UserAgentHash string    `gorm:"size:64" json:"-"`
	MetadataJSON  string    `gorm:"type:text" json:"metadata,omitempty"`
	CreatedAt     time.Time `gorm:"index" json:"created_at"`
}
