package storage

import (
	"fmt"

	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/model"
)

const databaseGatewayModeMigrationVersion = "202607190007"

func migrateDatabaseGatewayMode(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&model.SystemSetting{}); err != nil {
		return fmt.Errorf("add database gateway mode system setting: %w", err)
	}
	if err := tx.Model(&model.SystemSetting{}).
		Where("database_gateway_mode IS NULL OR TRIM(database_gateway_mode) = ''").
		Update("database_gateway_mode", config.DatabaseGatewayModeUnified).
		Error; err != nil {
		return fmt.Errorf("backfill database gateway mode system setting: %w", err)
	}
	return nil
}
