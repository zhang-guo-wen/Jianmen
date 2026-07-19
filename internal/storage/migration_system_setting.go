package storage

import (
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

const systemSettingMigrationVersion = "202607190005"

type systemSettingBeforeDatabaseClientMessageLimit struct {
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

func (systemSettingBeforeDatabaseClientMessageLimit) TableName() string {
	return "system_settings"
}

func migrateSystemSettings(tx *gorm.DB) error {
	return tx.AutoMigrate(
		&systemSettingBeforeDatabaseClientMessageLimit{},
		&model.SystemSettingRevision{},
	)
}
