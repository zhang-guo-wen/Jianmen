package storage

import (
	"gorm.io/gorm"

	"jianmen/internal/model"
)

const systemSettingMigrationVersion = "202607190005"

func migrateSystemSettings(tx *gorm.DB) error {
	return tx.AutoMigrate(
		&model.SystemSetting{},
		&model.SystemSettingRevision{},
	)
}
