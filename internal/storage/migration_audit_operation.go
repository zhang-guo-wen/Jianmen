package storage

import (
	"gorm.io/gorm"

	"jianmen/internal/model"
)

const auditOperationMigrationVersion = "202611230001"

// migrateAuditOperationFields 为操作日志补充业务动作、调用来源和可读对象名。
// 只新增列与索引，绝不回填或改写历史行：历史行保持空值，读取方回退到原有展示。
func migrateAuditOperationFields(tx *gorm.DB) error {
	event := &model.AuditEvent{}
	if !tx.Migrator().HasTable(event) {
		return nil
	}
	columns := []string{"Operation", "ActorChannel", "TargetLabel"}
	for _, column := range columns {
		if tx.Migrator().HasColumn(event, column) {
			continue
		}
		if err := tx.Migrator().AddColumn(event, column); err != nil {
			return err
		}
	}
	const indexName = "idx_audit_events_operation"
	if !tx.Migrator().HasIndex(event, indexName) {
		if err := tx.Migrator().CreateIndex(event, indexName); err != nil {
			return err
		}
	}
	const channelIndexName = "idx_audit_events_channel"
	if !tx.Migrator().HasIndex(event, channelIndexName) {
		if err := tx.Migrator().CreateIndex(event, channelIndexName); err != nil {
			return err
		}
	}
	return nil
}
