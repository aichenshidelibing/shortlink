package repository

import (
	"context"
	"shortlink/internal/model"
	"time"

	"gorm.io/gorm"
)

type BanRepository struct {
	db *gorm.DB
}

func NewBanRepository(db *gorm.DB) *BanRepository {
	return &BanRepository{db: db}
}

func (r *BanRepository) Create(ctx context.Context, ban *model.BannedIP) error {
	return r.db.WithContext(ctx).Create(ban).Error
}

func (r *BanRepository) IsBanned(ctx context.Context, ipHash string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.BannedIP{}).
		Where("ip_hash = ? AND ban_until > ?", ipHash, time.Now()).
		Count(&count).Error
	return count > 0, err
}

// CountActive returns the count of currently-active (not yet expired) bans.
func (r *BanRepository) CountActive(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.BannedIP{}).
		Where("ban_until > ?", time.Now()).Count(&n).Error
	return n, err
}

func (r *BanRepository) List(ctx context.Context) ([]model.BannedIP, error) {
	var bans []model.BannedIP
	err := r.db.WithContext(ctx).Order("created_at DESC").Find(&bans).Error
	return bans, err
}

func (r *BanRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.BannedIP{}).Error
}

func (r *BanRepository) CleanupExpired(ctx context.Context) error {
	return r.db.WithContext(ctx).Where("ban_until < ?", time.Now()).Delete(&model.BannedIP{}).Error
}
