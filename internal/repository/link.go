package repository

import (
	"context"
	"shortlink/internal/model"
	"time"

	"gorm.io/gorm"
)

type LinkRepository struct {
	db *gorm.DB
}

func NewLinkRepository(db *gorm.DB) *LinkRepository {
	return &LinkRepository{db: db}
}

func (r *LinkRepository) Create(ctx context.Context, link *model.Link) error {
	return r.db.WithContext(ctx).Create(link).Error
}

func (r *LinkRepository) GetByCode(ctx context.Context, code string) (*model.Link, error) {
	var link model.Link
	if err := r.db.WithContext(ctx).Where("short_code = ?", code).First(&link).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

func (r *LinkRepository) GetByCodeAndToken(ctx context.Context, code, token string) (*model.Link, error) {
	var link model.Link
	if err := r.db.WithContext(ctx).Where("short_code = ? AND edit_token = ?", code, token).First(&link).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

func (r *LinkRepository) CodeExists(ctx context.Context, code string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Link{}).Where("short_code = ?", code).Count(&count).Error
	if err != nil {
		return false, err
	}
	// Also check recycled codes
	var rcount int64
	r.db.WithContext(ctx).Model(&model.RecycledCode{}).Where("short_code = ? AND releases_at > ?", code, time.Now()).Count(&rcount)
	return count > 0 || rcount > 0, err
}

func (r *LinkRepository) UpdateURL(ctx context.Context, code string, urlEnc, nonce []byte) error {
	return r.db.WithContext(ctx).Model(&model.Link{}).
		Where("short_code = ?", code).
		Updates(map[string]interface{}{"original_url_enc": urlEnc, "nonce": nonce}).Error
}

func (r *LinkRepository) UpdateByCode(ctx context.Context, code string, values map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&model.Link{}).Where("short_code = ?", code).Updates(values).Error
}

func (r *LinkRepository) Delete(ctx context.Context, code string) error {
	return r.db.WithContext(ctx).Where("short_code = ?", code).Delete(&model.Link{}).Error
}

func (r *LinkRepository) DeleteExpired(ctx context.Context) (int64, error) {
	result := r.db.WithContext(ctx).
		Where("expires_at IS NOT NULL AND expires_at < ?", time.Now()).
		Delete(&model.Link{})
	return result.RowsAffected, result.Error
}

func (r *LinkRepository) List(ctx context.Context, offset, limit int) ([]model.Link, int64, error) {
	var links []model.Link
	var total int64

	db := r.db.WithContext(ctx).Model(&model.Link{})
	db.Count(&total)
	err := db.Order("created_at DESC").Offset(offset).Limit(limit).Find(&links).Error
	return links, total, err
}

func (r *LinkRepository) IncrementClick(ctx context.Context, code string) error {
	return r.db.WithContext(ctx).Model(&model.Link{}).Where("short_code = ?", code).UpdateColumn("click_count", gorm.Expr("click_count + 1")).Error
}

// SumClicks returns the total click_count across all links. Cheap enough
// for a dashboard tile — MySQL runs it as an index-only scan on the
// aggregate column.
func (r *LinkRepository) SumClicks(ctx context.Context) (int64, error) {
	var total *int64
	err := r.db.WithContext(ctx).Model(&model.Link{}).
		Select("COALESCE(SUM(click_count), 0)").
		Scan(&total).Error
	if err != nil || total == nil {
		return 0, err
	}
	return *total, nil
}

func (r *LinkRepository) MarkUsed(ctx context.Context, code string) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.Link{}).
		Where("short_code = ? AND is_once = ? AND used_at IS NULL", code, true).
		Updates(map[string]interface{}{"used_at": time.Now(), "status": 0})
	return result.RowsAffected > 0, result.Error
}

// ── Recycled codes ──

func (r *LinkRepository) AddRecycled(ctx context.Context, code string, clickCount int64, cooldownDays int) error {
	rc := &model.RecycledCode{
		ShortCode:  code,
		ClickCount: clickCount,
		DeletedAt:  time.Now(),
		ReleasesAt: time.Now().Add(time.Duration(cooldownDays) * 24 * time.Hour),
	}
	return r.db.WithContext(ctx).Create(rc).Error
}

func (r *LinkRepository) ReleaseExpiredCodes(ctx context.Context) (int64, error) {
	result := r.db.WithContext(ctx).Where("releases_at < ?", time.Now()).Delete(&model.RecycledCode{})
	return result.RowsAffected, result.Error
}

// ── Reports ──

func (r *LinkRepository) CreateReport(ctx context.Context, report *model.Report) error {
	return r.db.WithContext(ctx).Create(report).Error
}

func (r *LinkRepository) ListReports(ctx context.Context, status int) ([]model.Report, error) {
	var reports []model.Report
	q := r.db.WithContext(ctx).Order("created_at DESC")
	if status >= 0 {
		q = q.Where("status = ?", status)
	}
	err := q.Find(&reports).Error
	return reports, err
}

func (r *LinkRepository) UpdateReportStatus(ctx context.Context, id uint64, status int, handledBy string) error {
	return r.db.WithContext(ctx).Model(&model.Report{}).Where("id = ?", id).
		Updates(map[string]interface{}{"status": status, "handled_by": handledBy}).Error
}

func (r *LinkRepository) CountReportsToday(ctx context.Context, ipHash string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Report{}).
		Where("reporter_ip = ? AND created_at > ?", ipHash, time.Now().Truncate(24*time.Hour)).
		Count(&count).Error
	return count, err
}

func (r *LinkRepository) IsReportBanned(ctx context.Context, ipHash string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.ReportBan{}).Where("ip_hash = ?", ipHash).Count(&count).Error
	return count > 0, err
}

func (r *LinkRepository) BanReporter(ctx context.Context, ipHash, reason string) error {
	ban := &model.ReportBan{IPHash: ipHash, Reason: reason}
	return r.db.WithContext(ctx).Create(ban).Error
}
