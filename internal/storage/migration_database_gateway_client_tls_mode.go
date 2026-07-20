package storage

import (
	"fmt"

	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/model"
)

const databaseGatewayClientTLSModeMigrationVersion = "202607200002"

func migrateDatabaseGatewayClientTLSMode(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&model.SystemSetting{}); err != nil {
		return fmt.Errorf("add database gateway client TLS mode system setting: %w", err)
	}
	if err := tx.Model(&model.SystemSetting{}).
		Where("database_gateway_client_tls_mode IS NULL OR TRIM(database_gateway_client_tls_mode) = ''").
		Update("database_gateway_client_tls_mode", config.DatabaseGatewayClientTLSModeOptional).
		Error; err != nil {
		return fmt.Errorf("backfill database gateway client TLS mode system setting: %w", err)
	}
	return nil
}
