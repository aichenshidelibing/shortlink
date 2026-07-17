package repository

import (
	"fmt"
	"shortlink/internal/config"
	"shortlink/internal/model"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func NewDB(cfg *config.DatabaseConfig) (*gorm.DB, error) {
	var dialector gorm.Dialector

	switch cfg.Driver {
	case "sqlite3", "sqlite":
		dialector = sqlite.Open(cfg.DBName)
	default:
		dialector = mysql.Open(cfg.DSN())
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// Connection pool settings only apply to MySQL
	if cfg.Driver != "sqlite3" && cfg.Driver != "sqlite" {
		sqlDB, err := db.DB()
		if err != nil {
			return nil, err
		}

		sqlDB.SetMaxOpenConns(cfg.MaxOpen)
		sqlDB.SetMaxIdleConns(cfg.MaxIdle)
		sqlDB.SetConnMaxLifetime(5 * time.Minute)
		sqlDB.SetConnMaxIdleTime(2 * time.Minute)
	}

	return db, nil
}

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&model.Admin{},
		&model.AdminSetting{},
		&model.Link{},
		&model.APIKey{},
		&model.Click{},
		&model.BannedIP{},
		&model.WordRule{},
		&model.WhiteList{},
		&model.RecycledCode{},
		&model.Report{},
		&model.ReportBan{},
		&model.ReporterStats{},
		&model.AdminAuditLog{},
		&model.Domain{},
	)
}
