package repository

import (
	"context"
	"shortlink/internal/model"

	"gorm.io/gorm"
)

type WordRepository struct {
	db *gorm.DB
}

func NewWordRepository(db *gorm.DB) *WordRepository {
	return &WordRepository{db: db}
}

func (r *WordRepository) ListRules(ctx context.Context) ([]model.WordRule, error) {
	var rules []model.WordRule
	err := r.db.WithContext(ctx).Where("enabled = ?", true).Find(&rules).Error
	return rules, err
}

func (r *WordRepository) CreateRule(ctx context.Context, rule *model.WordRule) error {
	return r.db.WithContext(ctx).Create(rule).Error
}

func (r *WordRepository) BatchCreate(ctx context.Context, rules []model.WordRule) error {
	if len(rules) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(rules, 500).Error
}

func (r *WordRepository) DeleteRule(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.WordRule{}).Error
}

func (r *WordRepository) ListWhiteList(ctx context.Context) ([]model.WhiteList, error) {
	var list []model.WhiteList
	err := r.db.WithContext(ctx).Find(&list).Error
	return list, err
}

func (r *WordRepository) CreateWhiteList(ctx context.Context, item *model.WhiteList) error {
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *WordRepository) DeleteWhiteList(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.WhiteList{}).Error
}
