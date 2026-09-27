package storage

import (
	"gorm.io/gorm"
	"jianmen/internal/model"
)

func migrateAuditRedactionSettings(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&model.SystemSetting{}, &model.AuditDBQuery{}); err != nil {
		return err
	}
	return tx.Model(&model.SystemSetting{}).Where("id = ?", model.SystemSettingSingletonID).
		Updates(map[string]any{
			"ssh_redaction_enabled":            false,
			"database_audit_redaction_enabled": false,
			"database_audit_preview_bytes":     65536,
		}).Error
}
