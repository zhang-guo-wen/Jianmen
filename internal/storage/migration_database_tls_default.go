package storage

import (
	"fmt"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

const databaseTLSDefaultMigrationVersion = "202607190008"

func migrateDatabaseTLSDefault(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&model.DatabaseInstance{}) {
		return nil
	}
	if !tx.Migrator().HasColumn(&model.DatabaseInstance{}, "TLSMode") {
		return fmt.Errorf("change database TLS default: tls_mode column is missing")
	}
	if err := tx.Migrator().AlterColumn(&model.DatabaseInstance{}, "TLSMode"); err != nil {
		return fmt.Errorf("change database TLS default: %w", err)
	}
	return nil
}
