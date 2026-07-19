package storage

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

const (
	auditDBQueryLargePayloadMigrationVersion = "202607190006"
	defaultDatabaseClientMessageBytes        = 10 * 1024 * 1024
)

// auditDBQueryBeforeLargePayload freezes the schema installed by historical
// migrations.
type auditDBQueryBeforeLargePayload struct {
	ID             string    `gorm:"primaryKey;size:64"`
	AuditSessionID string    `gorm:"index;size:64;not null"`
	Timestamp      time.Time `gorm:"index"`
	SQLText        string    `gorm:"type:text"`
	QueryKind      string    `gorm:"size:32"`
	DurationMs     int64
}

func (auditDBQueryBeforeLargePayload) TableName() string {
	return "audit_db_queries"
}

// auditDBQueryLargePayloadSchema freezes the audit query schema owned by
// migration 202607190006. Future runtime model changes must be introduced by a
// later migration rather than silently changing this historical migration.
type auditDBQueryLargePayloadSchema struct {
	ID               string    `gorm:"primaryKey;size:64;index:idx_audit_db_queries_session_timestamp_id,priority:3"`
	AuditSessionID   string    `gorm:"index;size:64;not null;index:idx_audit_db_queries_session_timestamp_id,priority:1"`
	Timestamp        time.Time `gorm:"index;index:idx_audit_db_queries_session_timestamp_id,priority:2"`
	SQLText          string    `gorm:"size:4294967295"`
	OriginalSQLBytes int64     `gorm:"column:original_sql_bytes;not null;default:0"`
	SQLTruncated     bool      `gorm:"column:sql_truncated;not null;default:false"`
	QueryKind        string    `gorm:"size:32"`
	DurationMs       int64
}

func (auditDBQueryLargePayloadSchema) TableName() string {
	return "audit_db_queries"
}

// systemSettingDatabaseClientMessageSchema freezes the system setting schema
// extended by migration 202607190006.
type systemSettingDatabaseClientMessageSchema struct {
	ID                            string `gorm:"primaryKey;size:32"`
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

func (systemSettingDatabaseClientMessageSchema) TableName() string {
	return "system_settings"
}

func migrateAuditDBQueryLargePayload(tx *gorm.DB) error {
	if err := tx.AutoMigrate(
		&auditDBQueryLargePayloadSchema{},
		&systemSettingDatabaseClientMessageSchema{},
	); err != nil {
		return fmt.Errorf("enable large database proxy client messages: %w", err)
	}
	if err := tx.Model(&systemSettingDatabaseClientMessageSchema{}).
		Where("database_max_client_message_bytes = ?", 0).
		Update(
			"database_max_client_message_bytes",
			defaultDatabaseClientMessageBytes,
		).Error; err != nil {
		return fmt.Errorf("backfill database client message size: %w", err)
	}
	return nil
}
