package repository

import (
	"context"
	"shortlink/internal/model"

	"gorm.io/gorm"
)

type AdminRepository struct {
	db *gorm.DB
}

func NewAdminRepository(db *gorm.DB) *AdminRepository {
	return &AdminRepository{db: db}
}

func (r *AdminRepository) Get(ctx context.Context) (*model.Admin, error) {
	var admin model.Admin
	if err := r.db.WithContext(ctx).First(&admin).Error; err != nil {
		return nil, err
	}
	return &admin, nil
}

func (r *AdminRepository) Create(ctx context.Context, admin *model.Admin) error {
	return r.db.WithContext(ctx).Create(admin).Error
}

func (r *AdminRepository) FirstOrCreate(ctx context.Context, admin *model.Admin) error {
	return r.db.WithContext(ctx).Where("username = ?", admin.Username).FirstOrCreate(admin).Error
}

func (r *AdminRepository) UpdatePasswordHash(ctx context.Context, adminID int, passwordHash string) error {
	return r.db.WithContext(ctx).Model(&model.Admin{}).Where("id = ?", adminID).Update("password_hash", passwordHash).Error
}

func (r *AdminRepository) UpdateTOTPSecret(ctx context.Context, secret string) error {
	admin, err := r.Get(ctx)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&model.Admin{}).Where("id = ?", admin.ID).Update("totp_secret", secret).Error
}

func (r *AdminRepository) UpdateTOTPVerified(ctx context.Context, verified bool) error {
	admin, err := r.Get(ctx)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&model.Admin{}).Where("id = ?", admin.ID).Update("totp_verified", verified).Error
}

func (r *AdminRepository) GetSettings(ctx context.Context) (*model.AdminSetting, error) {
	var s model.AdminSetting
	if err := r.db.WithContext(ctx).First(&s).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &model.AdminSetting{ShortlinkLength: 6}, nil
		}
		return nil, err
	}
	return &s, nil
}

func (r *AdminRepository) SaveSettings(ctx context.Context, s *model.AdminSetting) error {
	return r.db.WithContext(ctx).Save(s).Error
}
