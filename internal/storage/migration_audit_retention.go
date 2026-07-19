package storage

import (
	"time"

	"gorm.io/gorm"
)

// auditSessionBeforeRetention is the physical schema owned by migrations before
// 202607190002. Keeping cleanup columns out of historical migrations makes the
// versioned retention migration the only production path that can add them.
type auditSessionBeforeRetention struct {
	ID              string    `gorm:"primaryKey;size:64"`
	UserSessionID   string    `gorm:"index;size:64"`
	UserID          string    `gorm:"index;size:64"`
	Username        string    `gorm:"index;size:128"`
	Protocol        string    `gorm:"index:idx_audit_sessions_protocol_started,priority:1;size:32"`
	ProtocolSubtype string    `gorm:"size:64"`
	TargetName      string    `gorm:"size:255"`
	TargetAddress   string    `gorm:"size:255"`
	AccountName     string    `gorm:"size:128"`
	AccountUsername string    `gorm:"size:128"`
	ClientIP        string    `gorm:"size:128"`
	StartedAt       time.Time `gorm:"index:idx_audit_sessions_protocol_started,priority:2;index:idx_audit_sessions_user_started,priority:2;index:idx_audit_sessions_session_started,priority:2"`
	EndedAt         *time.Time
	State           string `gorm:"size:32"`
	ReplayDir       string `gorm:"size:512"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (auditSessionBeforeRetention) TableName() string {
	return "audit_sessions"
}

// auditSessionRetentionSchema freezes the columns owned by migration
// 202607190002. Later protocol migrations must not leak their columns into
// this historical step through the current runtime model.
type auditSessionRetentionSchema struct {
	ID              string     `gorm:"primaryKey;size:64"`
	UserSessionID   string     `gorm:"index;size:64"`
	UserID          string     `gorm:"index;size:64"`
	Username        string     `gorm:"index;size:128"`
	Protocol        string     `gorm:"index:idx_audit_sessions_protocol_started,priority:1;size:32"`
	ProtocolSubtype string     `gorm:"size:64"`
	TargetName      string     `gorm:"size:255"`
	TargetAddress   string     `gorm:"size:255"`
	AccountName     string     `gorm:"size:128"`
	AccountUsername string     `gorm:"size:128"`
	ClientIP        string     `gorm:"size:128"`
	StartedAt       time.Time  `gorm:"index:idx_audit_sessions_protocol_started,priority:2;index:idx_audit_sessions_user_started,priority:2;index:idx_audit_sessions_session_started,priority:2"`
	EndedAt         *time.Time `gorm:"index:idx_audit_sessions_cleanup,priority:2"`
	State           string     `gorm:"size:32"`
	ReplayDir       string     `gorm:"size:512"`
	CleanupStatus   string     `gorm:"index:idx_audit_sessions_cleanup,priority:1;size:16;not null;default:ready"`
	CleanupAt       *time.Time `gorm:"index"`
	CleanupError    string     `gorm:"type:text"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (auditSessionRetentionSchema) TableName() string {
	return "audit_sessions"
}

func migrateAuditRetentionCleanup(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&auditSessionBeforeRetention{}) {
		return nil
	}
	return tx.AutoMigrate(&auditSessionRetentionSchema{})
}
