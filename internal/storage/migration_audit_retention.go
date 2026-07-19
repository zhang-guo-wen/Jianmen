package storage

import (
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
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

func migrateAuditRetentionCleanup(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&auditSessionBeforeRetention{}) {
		return nil
	}
	return tx.AutoMigrate(&model.AuditSession{})
}
