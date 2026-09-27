package storage

import (
	"fmt"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

const userPreferenceClientsMigrationVersion = "202607210001"

func migrateUserPreferenceClients(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&model.UserPreference{}) {
		return tx.AutoMigrate(&model.UserPreference{})
	}
	if err := tx.AutoMigrate(&model.UserPreference{}); err != nil {
		return fmt.Errorf("add user preference client fields: %w", err)
	}
	return nil
}
