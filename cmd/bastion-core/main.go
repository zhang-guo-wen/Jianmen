package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"jianmen/internal/access"
	"jianmen/internal/config"
	"jianmen/internal/server/admin"
	"jianmen/internal/server/dbproxy"
	"jianmen/internal/server/sshserver"
	"jianmen/internal/storage"

	"gorm.io/gorm"
)

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load config", "path", *configPath, "error", err)
		os.Exit(1)
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
		if cfg.Database.AutoMigrate {
			if err := storage.AutoMigrate(db); err != nil {
				logger.Error("failed to migrate metadata database", "driver", cfg.Database.Driver, "error", err)
				os.Exit(1)
			}
		}
		if err := storage.BootstrapMetadata(db, cfg); err != nil {
			logger.Error("failed to bootstrap metadata database", "driver", cfg.Database.Driver, "error", err)
			os.Exit(1)
		}
		metadataDB = db
		logger.Info("metadata database ready", "driver", cfg.Database.Driver, "auto_migrate", cfg.Database.AutoMigrate)
	}

	store, err := access.NewStaticStore(cfg)
	if err != nil {
		logger.Error("failed to initialize access store", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 3)

	sshSrv := sshserver.New(cfg, store, logger)
	go func() {
		errCh <- sshSrv.ListenAndServe(ctx)
	}()

	dbManager := dbproxy.NewManager(cfg.DatabaseProxies, cfg.ReplayDir, logger, metadataDB)

	if cfg.Admin.Enabled {
		adminSrv := admin.New(cfg, store, logger, metadataDB)
		adminSrv.SetDatabaseProxyApplier(dbManager)
		go func() {
			errCh <- adminSrv.ListenAndServe(ctx)
		}()
	}

	go func() {
		errCh <- dbManager.ListenAndServe(ctx)
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
