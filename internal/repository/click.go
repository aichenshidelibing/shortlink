package repository

import (
	"context"
	"shortlink/internal/model"
	"time"

	"gorm.io/gorm"
)

type ClickRepository struct {
	db *gorm.DB
}

func NewClickRepository(db *gorm.DB) *ClickRepository {
	return &ClickRepository{db: db}
}

func (r *ClickRepository) CreateBatch(ctx context.Context, clicks []*model.Click) error {
	if len(clicks) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(clicks, 100).Error
}

func (r *ClickRepository) GetStats(ctx context.Context, linkID uint64, days int) ([]model.DailyStat, []model.GeoStat, error) {
	var timeline []model.DailyStat
	var geo []model.GeoStat

	startDate := time.Now().AddDate(0, 0, -days).Format("2006-01-02")

	err := r.db.WithContext(ctx).Raw(`
		SELECT DATE(clicked_at) as date, COUNT(*) as count
		FROM clicks
		WHERE link_id = ? AND DATE(clicked_at) >= ?
		GROUP BY DATE(clicked_at)
		ORDER BY date
	`, linkID, startDate).Scan(&timeline).Error
	if err != nil {
		return nil, nil, err
	}

	startTime := time.Now().AddDate(0, 0, -days).Format("2006-01-02 15:04:05")
	err = r.db.WithContext(ctx).Raw(`
		SELECT country as country, COUNT(*) as count
		FROM clicks
		WHERE link_id = ? AND clicked_at >= ?
		GROUP BY country
		ORDER BY count DESC
		LIMIT 20
	`, linkID, startTime).Scan(&geo).Error

	return timeline, geo, err
}

func (r *ClickRepository) GetTotalClicks(ctx context.Context, linkID uint64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Click{}).Where("link_id = ?", linkID).Count(&count).Error
	return count, err
}

func (r *ClickRepository) CountUniqueVisitors(ctx context.Context, linkID uint64, days int) (int64, error) {
	var count int64
	startTime := time.Now().AddDate(0, 0, -days)
	err := r.db.WithContext(ctx).Model(&model.Click{}).Where("link_id = ? AND clicked_at >= ?", linkID, startTime).Distinct("visitor_hash").Count(&count).Error
	return count, err
}

func (r *ClickRepository) GroupByField(ctx context.Context, linkID uint64, days int, field string) ([]model.GroupedStat, error) {
	allowed := map[string]bool{"referer_host": true, "device": true, "browser": true, "os": true}
	if !allowed[field] {
		return nil, nil
	}
	var out []model.GroupedStat
	startTime := time.Now().AddDate(0, 0, -days).Format("2006-01-02 15:04:05")
	err := r.db.WithContext(ctx).Raw("SELECT "+field+" as name, COUNT(*) as count FROM clicks WHERE link_id = ? AND clicked_at >= ? AND "+field+" <> '' GROUP BY "+field+" ORDER BY count DESC LIMIT 20", linkID, startTime).Scan(&out).Error
	return out, err
}

func (r *ClickRepository) GroupByHour(ctx context.Context, linkID uint64, days int) ([]model.HourlyStat, error) {
	var out []model.HourlyStat
	startTime := time.Now().AddDate(0, 0, -days).Format("2006-01-02 15:04:05")
	err := r.db.WithContext(ctx).Raw("SELECT hour as hour, COUNT(*) as count FROM clicks WHERE link_id = ? AND clicked_at >= ? GROUP BY hour ORDER BY hour", linkID, startTime).Scan(&out).Error
	return out, err
}
