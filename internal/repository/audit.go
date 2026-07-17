package repository

import (
	"context"
	"shortlink/internal/model"

	"gorm.io/gorm"
)

type AuditRepository struct{ db *gorm.DB }

func NewAuditRepository(db *gorm.DB) *AuditRepository { return &AuditRepository{db: db} }

func (r *AuditRepository) Create(ctx context.Context, log *model.AdminAuditLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

func (r *AuditRepository) List(ctx context.Context, offset, limit int) ([]model.AdminAuditLog, int64, error) {
	var logs []model.AdminAuditLog
	var total int64
	q := r.db.WithContext(ctx).Model(&model.AdminAuditLog{})
	q.Count(&total)
	err := q.Order("created_at DESC").Offset(offset).Limit(limit).Find(&logs).Error
	return logs, total, err
}
