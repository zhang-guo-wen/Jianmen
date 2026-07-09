package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/crypto"
	"jianmen/internal/server/admin"
	"jianmen/internal/server/dbproxy"
	"jianmen/internal/server/sshserver"
	"jianmen/internal/storage"
	"jianmen/internal/store"

	"gorm.io/gorm"
)

func main() {
	configPath := flag.String("config", "config.local.json", "path to config file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load config", "path", *configPath, "error", err)
		os.Exit(1)
	}
	if !cfg.Database.Enabled {
		logger.Error("metadata database is required; set database.enabled to true")
		os.Exit(1)
	}

	// 初始化加密密钥（在任何数据库操作之前）
	dataDir := filepath.Dir(cfg.Database.DSN)
	if dataDir == "." || dataDir == "" {
		dataDir = "data"
	}
	newKeyGenerated, err := crypto.Init(dataDir)
	if err != nil {
		logger.Error("failed to initialize crypto", "error", err)
		os.Exit(1)
	}
	if newKeyGenerated {
		logger.Info("generated new encryption key", "path", filepath.Join(dataDir, "encryption.key"))
		logger.Info("please save this key file, it is required to decrypt stored credentials")
	}

	var metadataDB *gorm.DB
	if cfg.Database.Enabled {
		db, err := storage.Open(storage.Config{
			Driver:          storage.Driver(cfg.Database.Driver),
			DSN:             cfg.Database.DSN,
			MaxOpenConns:    cfg.Database.MaxOpenConns,
			MaxIdleConns:    cfg.Database.MaxIdleConns,
			ConnMaxLifetime: time.Duration(cfg.Database.ConnMaxLifetimeSeconds) * time.Second,
		})
		if err != nil {
			logger.Error("failed to open metadata database", "driver", cfg.Database.Driver, "error", err)
			os.Exit(1)
		}
		sqlDB, err := db.DB()
		if err != nil {
			logger.Error("failed to initialize metadata database", "driver", cfg.Database.Driver, "error", err)
			os.Exit(1)
		}
		defer sqlDB.Close()
		migrationMode := "versioned"
		if cfg.Database.AutoMigrate {
			migrationMode = "automigrate"
			if err := storage.AutoMigrate(db); err != nil {
				logger.Error("failed to migrate metadata database", "driver", cfg.Database.Driver, "error", err)
				os.Exit(1)
			}
		} else if err := storage.Migrate(db); err != nil {
			logger.Error("failed to migrate metadata database", "driver", cfg.Database.Driver, "error", err)
			os.Exit(1)
		}
		if err := storage.BootstrapMetadata(db, cfg); err != nil {
			logger.Error("failed to bootstrap metadata database", "driver", cfg.Database.Driver, "error", err)
			os.Exit(1)
		}
		metadataDB = db
		logger.Info("metadata database ready", "driver", cfg.Database.Driver, "migration_mode", migrationMode)
	}

	appStore := store.NewDBStore(metadataDB)
	logger.Info("using database-backed store")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 3)

	sshSrv := sshserver.New(cfg, appStore, logger, dataDir, metadataDB)
	go func() {
		errCh <- sshSrv.ListenAndServe(ctx)
	}()

	dbGateway := dbproxy.NewGateway(cfg.DatabaseGateway, appStore, cfg.ReplayDir, logger, metadataDB, admin.LoadSuperAdminIDs(cfg, dataDir), appStore)

	if cfg.Admin.Enabled {
		adminSrv := admin.New(cfg, appStore, logger, dataDir, metadataDB)
		go func() {
			errCh <- adminSrv.ListenAndServe(ctx)
		}()
	}

	go func() {
		errCh <- dbGateway.ListenAndServe(ctx)
	}()

	select {
	case <-ctx.Done():
		return
	case err := <-errCh:
		if err != nil && ctx.Err() == nil {
			cancel()
			logger.Error("server stopped", "error", err)
			os.Exit(1)
		}
		cancel()
		return
	}
}
