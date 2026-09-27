package storage

import (
	"testing"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/model"
)

type legacySystemSettingWithoutClientTLSMode struct {
	ID                            string `gorm:"primaryKey;size:32"`
	DatabaseGatewayMode           string `gorm:"size:16;not null;default:unified"`
	WebRDPEnabled                 bool   `gorm:"not null"`
	WebRDPConnectTimeoutSeconds   int    `gorm:"not null"`
	WebRDPAllowUnrecorded         bool   `gorm:"not null"`
	RecordingEnabled              bool   `gorm:"not null"`
	RecordingRecordInput          bool   `gorm:"not null"`
	RecordingRecordCommands       bool   `gorm:"not null"`
	RecordingRetentionDays        int    `gorm:"not null"`
	RecordingMaxReplayBytes       int64  `gorm:"not null"`
	RecordingCleanupBatchSize     int    `gorm:"not null"`
	DatabaseMaxClientMessageBytes int    `gorm:"not null;default:10485760"`
	Revision                      int64  `gorm:"not null"`
	AppliedRevision               int64  `gorm:"not null;default:0"`
	AppliedAt                     *time.Time
	UpdatedByID                   string `gorm:"size:64"`
	UpdatedByUsername             string `gorm:"size:128"`
	CreatedAt                     time.Time
	UpdatedAt                     time.Time
}

func (legacySystemSettingWithoutClientTLSMode) TableName() string {
	return "system_settings"
}

func TestDatabaseGatewayClientTLSModeMigrationDefaultsExistingRowsToOptional(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&legacySystemSettingWithoutClientTLSMode{}); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	now := time.Now().UTC()
	if err := db.Create(&legacySystemSettingWithoutClientTLSMode{
		ID:                            model.SystemSettingSingletonID,
		DatabaseGatewayMode:           config.DatabaseGatewayModeUnified,
		WebRDPConnectTimeoutSeconds:   15,
		RecordingEnabled:              true,
		RecordingRecordCommands:       true,
		RecordingRetentionDays:        30,
		RecordingMaxReplayBytes:       1024,
		RecordingCleanupBatchSize:     100,
		DatabaseMaxClientMessageBytes: 10 << 20,
		Revision:                      1,
		AppliedRevision:               1,
		CreatedAt:                     now,
		UpdatedAt:                     now,
	}).Error; err != nil {
		t.Fatalf("seed legacy settings: %v", err)
	}

	if err := migrateDatabaseGatewayClientTLSMode(db); err != nil {
		t.Fatalf("migrate database gateway client TLS mode: %v", err)
	}
	var setting model.SystemSetting
	if err := db.First(&setting, "id = ?", model.SystemSettingSingletonID).Error; err != nil {
		t.Fatalf("load migrated settings: %v", err)
	}
	if setting.DatabaseGatewayClientTLSMode != config.DatabaseGatewayClientTLSModeOptional {
		t.Fatalf(
			"database gateway client TLS mode = %q, want %q",
			setting.DatabaseGatewayClientTLSMode,
			config.DatabaseGatewayClientTLSModeOptional,
		)
	}
	if !db.Migrator().HasColumn(&model.SystemSetting{}, "database_gateway_client_tls_mode") {
		t.Fatal("database_gateway_client_tls_mode column is missing")
	}

	setting.ID = "invalid-client-tls-mode"
	setting.DatabaseGatewayClientTLSMode = "disabled"
	if err := db.Create(&setting).Error; err == nil {
		t.Fatal("invalid database gateway client TLS mode unexpectedly satisfied database constraint")
	}
	if err := migrateDatabaseGatewayClientTLSMode(db); err != nil {
		t.Fatalf("second database gateway client TLS mode migration: %v", err)
	}
}
