package model

import "time"

const SystemSettingSingletonID = "system"

// SystemSetting stores the desired restart-applied system policy.
type SystemSetting struct {
	ID                          string `gorm:"primaryKey;size:32"`
	DatabaseGatewayMode         string `gorm:"size:16;not null;default:unified;check:chk_system_settings_database_gateway_mode,database_gateway_mode IN ('unified','independent')"`
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
