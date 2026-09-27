package storage

import (
	"time"

	"gorm.io/gorm"
)

const auditSessionLeaseMigrationVersion = "202607190004"

type auditSessionLeaseSchema struct {
	ID             string `gorm:"primaryKey;size:64"`
	State          string `gorm:"index;index:idx_audit_sessions_lease_owner_state,priority:2;index:idx_audit_sessions_lease_expiry,priority:1;size:32"`
	LeaseOwner     string `gorm:"index:idx_audit_sessions_lease_owner_state,priority:1;size:64"`
	HeartbeatAt    *time.Time
	LeaseExpiresAt *time.Time `gorm:"index:idx_audit_sessions_lease_expiry,priority:2"`
}

func (auditSessionLeaseSchema) TableName() string { return "audit_sessions" }

func migrateAuditSessionLease(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&auditSessionWebRDPSchema{}) {
		return nil
	}
	return tx.AutoMigrate(&auditSessionLeaseSchema{})
}
