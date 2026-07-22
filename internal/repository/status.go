package repository

import (
	"context"
	"shortlink/internal/model"
	"time"

	"gorm.io/gorm"
)

type StatusRepository struct {
	db *gorm.DB
}

func NewStatusRepository(db *gorm.DB) *StatusRepository {
	return &StatusRepository{db: db}
}

func (r *StatusRepository) Create(ctx context.Context, sample *model.AvailabilitySample) error {
	return r.db.WithContext(ctx).Create(sample).Error
}

func (r *StatusRepository) ListSince(ctx context.Context, since time.Time) ([]model.AvailabilitySample, error) {
	var samples []model.AvailabilitySample
	err := r.db.WithContext(ctx).Where("checked_at >= ?", since).Order("checked_at ASC").Find(&samples).Error
	return samples, err
}

func (r *StatusRepository) DeleteBefore(ctx context.Context, before time.Time) error {
	return r.db.WithContext(ctx).Where("checked_at < ?", before).Delete(&model.AvailabilitySample{}).Error
}
