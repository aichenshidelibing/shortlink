package repository

import (
	"context"
	"shortlink/internal/model"

	"gorm.io/gorm"
)

type APIKeyRepository struct {
	db *gorm.DB
}

func NewAPIKeyRepository(db *gorm.DB) *APIKeyRepository {
	return &APIKeyRepository{db: db}
}

func (r *APIKeyRepository) Create(ctx context.Context, key *model.APIKey) error {
	return r.db.WithContext(ctx).Create(key).Error
}

func (r *APIKeyRepository) GetByHash(ctx context.Context, hash string) (*model.APIKey, error) {
	var key model.APIKey
	if err := r.db.WithContext(ctx).Where("key_hash = ? AND revoked = ?", hash, false).First(&key).Error; err != nil {
		return nil, err
	}
	return &key, nil
}

func (r *APIKeyRepository) GetByID(ctx context.Context, id uint64) (*model.APIKey, error) {
	var key model.APIKey
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&key).Error; err != nil {
		return nil, err
	}
	return &key, nil
}

func (r *APIKeyRepository) List(ctx context.Context) ([]model.APIKey, error) {
	var keys []model.APIKey
	err := r.db.WithContext(ctx).Order("created_at DESC").Find(&keys).Error
	return keys, err
}

func (r *APIKeyRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.APIKey{}).Error
}

func (r *APIKeyRepository) Update(ctx context.Context, id uint64, values map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&model.APIKey{}).Where("id = ?", id).Updates(values).Error
}

func (r *APIKeyRepository) Revoke(ctx context.Context, id uint64) error {
	return r.Update(ctx, id, map[string]interface{}{"revoked": true})
}

func (r *APIKeyRepository) UpdateLastUsed(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&model.APIKey{}).Where("id = ?", id).UpdateColumn("last_used_at", gorm.Expr("NOW()")).Error
}
