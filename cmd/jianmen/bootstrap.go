package main

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/crypto"
	"jianmen/internal/storage"
)

func initializeMetadata(cfg *config.Config, logger *slog.Logger) (*gorm.DB, string, func(), error) {
	if cfg == nil {
		return nil, "", nil, fmt.Errorf("config is required")
	}
	if logger == nil {
		return nil, "", nil, fmt.Errorf("logger is required")
	}
	if !cfg.Database.Enabled {
		return nil, "", nil, fmt.Errorf("metadata database is required; set database.enabled to true")
	}
	dataDir := filepath.Dir(cfg.Database.DSN)
	if dataDir == "." || dataDir == "" {
		dataDir = "data"
	}
	newKeyGenerated, err := crypto.Init(dataDir)
	if err != nil {
		return nil, "", nil, fmt.Errorf("initialize crypto: %w", err)
	}
	if newKeyGenerated {
		logger.Info("generated new encryption key", "path", filepath.Join(dataDir, "encryption.key"))
		logger.Info("please save this key file, it is required to decrypt stored credentials")
	}

	db, err := storage.Open(storage.Config{
		Driver:          storage.Driver(cfg.Database.Driver),
		DSN:             cfg.Database.DSN,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: time.Duration(cfg.Database.ConnMaxLifetimeSeconds) * time.Second,
	})
	if err != nil {
		return nil, "", nil, fmt.Errorf("open metadata database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, "", nil, fmt.Errorf("initialize metadata database: %w", err)
	}
	cleanup := func() {
		if err := sqlDB.Close(); err != nil {
			logger.Warn("failed to close metadata database", "error", err)
		}
	}

	// SQLite 迁移前自动备份，每次覆盖上一次的 .bak 文件。
	if storage.IsSQLiteDriver(cfg.Database.Driver) {
		if err := storage.BackupSQLite(cfg.Database.DSN); err != nil {
			logger.Warn("数据库迁移前备份失败", "error", err)
		} else {
			logger.Info("数据库迁移前已备份", "path", storage.BackupPath(cfg.Database.DSN))
		}
	}

	err = storage.Migrate(db)
	if err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("migrate metadata database: %w", err)
	}
	// 幂等结构迁移：host_accounts.id 主键迁移为 (id, active_marker) 复合
	// 唯一索引、补齐含 active_marker 的复合唯一索引。版本化迁移链不覆盖
	// 这两类迁移（测试走 AutoMigrate，生产在此显式执行）。
	if err := storage.MigrateHostAccountIDActiveIndex(db); err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("migrate host account id active index: %w", err)
	}
	if err := storage.MigrateAuditUniqueIndexes(db); err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("migrate audit unique indexes: %w", err)
	}
	if err := storage.BootstrapMetadata(db, cfg); err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("bootstrap metadata database: %w", err)
	}
	logger.Info("metadata database ready", "driver", cfg.Database.Driver, "migration_mode", "versioned")
	return db, dataDir, cleanup, nil
}
