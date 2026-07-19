package storage

import (
	"testing"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/model"
)

type legacySystemSettingWithoutGatewayMode struct {
	ID                          string `gorm:"primaryKey;size:32"`
	WebRDPEnabled               bool   `gorm:"not null"`
	WebRDPConnectTimeoutSeconds int    `gorm:"not null"`
	WebRDPAllowUnrecorded       bool   `gorm:"not null"`
	RecordingEnabled            bool   `gorm:"not null"`
	RecordingRecordInput        bool   `gorm:"not null"`
	RecordingRecordCommands     bool   `gorm:"not null"`
	RecordingRetentionDays      int    `gorm:"not null"`
	RecordingMaxReplayBytes     int64  `gorm:"not null"`
	RecordingCleanupBatchSize   int    `gorm:"not null"`
	Revision                    int64  `gorm:"not null"`
	AppliedRevision             int64  `gorm:"not null;default:0"`
	AppliedAt                   *time.Time
	UpdatedByID                 string `gorm:"size:64"`
	UpdatedByUsername           string `gorm:"size:128"`
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

func (legacySystemSettingWithoutGatewayMode) TableName() string {
	return "system_settings"
}

func TestDatabaseGatewayModeMigrationBackfillsAndConstrainsExistingRows(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&legacySystemSettingWithoutGatewayMode{},
	); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	now := time.Now().UTC()
	if err := db.Create(&legacySystemSettingWithoutGatewayMode{
		ID:                          model.SystemSettingSingletonID,
		WebRDPConnectTimeoutSeconds: 15,
		RecordingEnabled:            true,
		RecordingRecordCommands:     true,
		RecordingRetentionDays:      30,
		RecordingMaxReplayBytes:     1024,
		RecordingCleanupBatchSize:   100,
		Revision:                    1,
		AppliedRevision:             1,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	}).Error; err != nil {
		t.Fatalf("seed legacy settings: %v", err)
	}
	if err := migrateDatabaseGatewayMode(db); err != nil {
		t.Fatalf("migrate database gateway mode: %v", err)
	}
	var setting model.SystemSetting
	if err := db.First(&setting, "id = ?", model.SystemSettingSingletonID).Error; err != nil {
		t.Fatalf("load migrated settings: %v", err)
	}
	if setting.DatabaseGatewayMode != config.DatabaseGatewayModeUnified {
		t.Fatalf(
			"database gateway mode = %q, want %q",
			setting.DatabaseGatewayMode,
			config.DatabaseGatewayModeUnified,
		)
	}
	if !db.Migrator().HasColumn(&model.SystemSetting{}, "database_gateway_mode") {
		t.Fatal("database_gateway_mode column is missing")
	}

	setting.ID = "invalid-mode"
	setting.DatabaseGatewayMode = "auto"
	if err := db.Create(&setting).Error; err == nil {
		t.Fatal("invalid database gateway mode unexpectedly satisfied database constraint")
	}
	if err := migrateDatabaseGatewayMode(db); err != nil {
		t.Fatalf("second database gateway mode migration: %v", err)
	}
}
