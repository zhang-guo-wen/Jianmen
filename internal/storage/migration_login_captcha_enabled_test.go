package storage

import (
	"testing"
	"time"

	"jianmen/internal/model"
)

// legacySystemSettingWithoutCaptcha 模拟验证码开关合入前的存量表结构。
type legacySystemSettingWithoutCaptcha struct {
	ID                          string `gorm:"primaryKey;size:32"`
	DatabaseGatewayMode         string `gorm:"size:16;not null;default:unified"`
	DatabaseGatewayClientTLSMode string `gorm:"size:16;not null;default:optional"`
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

func (legacySystemSettingWithoutCaptcha) TableName() string {
	return "system_settings"
}

func TestLoginCaptchaEnabledMigrationAddsColumnToExistingDeployment(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&legacySystemSettingWithoutCaptcha{},
	); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	now := time.Now().UTC()
	legacy := &legacySystemSettingWithoutCaptcha{
		ID:                          model.SystemSettingSingletonID,
		DatabaseGatewayMode:         "unified",
		DatabaseGatewayClientTLSMode: "optional",
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
	}
	if err := db.Create(legacy).Error; err != nil {
		t.Fatalf("seed legacy settings: %v", err)
	}

	if err := migrateLoginCaptchaEnabled(db); err != nil {
		t.Fatalf("migrate login captcha enabled: %v", err)
	}

	if !db.Migrator().HasColumn(&model.SystemSetting{}, "login_captcha_enabled") {
		t.Fatal("login_captcha_enabled column is missing after migration")
	}

	var setting model.SystemSetting
	if err := db.First(&setting, "id = ?", model.SystemSettingSingletonID).Error; err != nil {
		t.Fatalf("load migrated settings: %v", err)
	}
	if setting.LoginCaptchaEnabled {
		t.Fatalf("login captcha enabled = %v, want false", setting.LoginCaptchaEnabled)
	}
	if setting.RecordingRetentionDays != 30 || setting.DatabaseGatewayMode != "unified" {
		t.Fatalf("migration changed existing values: %+v", setting)
	}

	// 幂等：重复执行不报错且不丢数据
	if err := migrateLoginCaptchaEnabled(db); err != nil {
		t.Fatalf("second login captcha migration: %v", err)
	}
	if err := db.First(&setting, "id = ?", model.SystemSettingSingletonID).Error; err != nil {
		t.Fatalf("settings lost after second migration: %v", err)
	}
}
