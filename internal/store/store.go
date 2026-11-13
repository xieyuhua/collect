package store

import (
	"fmt"
	"path/filepath"
	"time"

	"collect/internal/config"
	"collect/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Open() (*gorm.DB, error) {
	dsn := filepath.Join(config.DataPath(), "app.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(time.Hour)
	_ = db.Exec("PRAGMA journal_mode=WAL;").Error
	_ = db.Exec("PRAGMA busy_timeout=8000;").Error
	if err := db.AutoMigrate(
		&model.Task{}, &model.RunLog{}, &model.StepLog{}, &model.DatasetRow{},
		&model.DataSource{}, &model.Notification{}, &model.DedupeKey{}, &model.Setting{},
		&model.Job{}, &model.JobRun{}, &model.JobStepLog{},
	); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	DB = db
	seed(db)
	return db, nil
}

func seed(db *gorm.DB) {
	defaults := map[string]string{
		"wecom_enabled":     "false",
		"wecom_webhook":     "",
		"notify_on_failure": "true",
		"notify_on_success": "false",
		"max_body_kb":       "256",
	}
	for k, v := range defaults {
		var s model.Setting
		if err := db.Where("key = ?", k).First(&s).Error; err == gorm.ErrRecordNotFound {
			db.Create(&model.Setting{Key: k, Value: v, UpdatedAt: time.Now()})
		}
	}
}

func GetSetting(key string) string {
	var s model.Setting
	if err := DB.Where("key = ?", key).First(&s).Error; err != nil {
		return ""
	}
	return s.Value
}

func SetSetting(key, value string) error {
	s := model.Setting{Key: key, Value: value, UpdatedAt: time.Now()}
	return DB.Save(&s).Error
}
