package repository

import (
	"context"
	"shortlink/internal/model"

	"gorm.io/gorm"
)

type DomainRepository struct{ db *gorm.DB }

func NewDomainRepository(db *gorm.DB) *DomainRepository { return &DomainRepository{db: db} }

func (r *DomainRepository) List(ctx context.Context) ([]model.Domain, error) {
	var domains []model.Domain
	err := r.db.WithContext(ctx).Order("is_default DESC, created_at DESC").Find(&domains).Error
	return domains, err
}

func (r *DomainRepository) Create(ctx context.Context, d *model.Domain) error {
	return r.db.WithContext(ctx).Create(d).Error
}
func (r *DomainRepository) Update(ctx context.Context, id uint64, values map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&model.Domain{}).Where("id = ?", id).Updates(values).Error
}
func (r *DomainRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Domain{}).Error
}
