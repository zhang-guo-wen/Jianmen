package storage

import (
	"jianmen/internal/model"

	"gorm.io/gorm"
)

const auditBatchesMigrationVersion = "202611190001"

// 仅增加列和索引，不改写任何历史审计行。
func migrateAuditBatches(tx *gorm.DB) error {
	session := &model.AuditSession{}
	if !tx.Migrator().HasTable(session) {
		return nil
	}
	if !tx.Migrator().HasColumn(session, "BatchID") {
		if err := tx.Migrator().AddColumn(session, "BatchID"); err != nil {
			return err
		}
	}
	const name = "idx_audit_sessions_batch_started"
	if !tx.Migrator().HasIndex(session, name) {
		return tx.Migrator().CreateIndex(session, name)
	}
	return nil
}
