package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"jianmen/internal/config"
	"jianmen/internal/online"
	"jianmen/internal/rbac"
	"jianmen/internal/server/admin"
	"jianmen/internal/server/appproxy"
	"jianmen/internal/server/dbproxy"
	"jianmen/internal/server/sshserver"
	"jianmen/internal/service"
	"jianmen/internal/store"
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
	metadataDB, dataDir, cleanupMetadata, err := initializeMetadata(cfg, logger)
	if err != nil {
		logger.Error("failed to initialize metadata", "error", err)
		os.Exit(1)
	}
	defer cleanupMetadata()

	appStore := store.NewDBStore(metadataDB)
	logger.Info("using database-backed store")
	identityService, err := service.NewIdentityService(appStore)
	if err != nil {
		logger.Error("failed to initialize identity service", "error", err)
		os.Exit(1)
	}
	authorizationService, err := service.NewAuthorizationService(
		identityService,
		rbac.NewChecker(metadataDB),
		rbac.NewResourceGrantChecker(metadataDB),
	)
	if err != nil {
		logger.Error("failed to initialize authorization service", "error", err)
		os.Exit(1)
	}
	browserSessionService, err := service.NewBrowserSessionService(appStore)
	if err != nil {
		logger.Error("failed to initialize browser session service", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 4)
	onlineSessions := online.NewRegistry()

	sshSrv, err := sshserver.New(cfg, appStore, authorizationService, logger, onlineSessions)
	if err != nil {
		logger.Error("failed to initialize SSH server", "error", err)
		os.Exit(1)
	}
	go func() {
		errCh <- sshSrv.ListenAndServe(ctx)
	}()

	dbGateway := dbproxy.NewGateway(cfg.DatabaseGateway, appStore, cfg.ReplayDir, logger, metadataDB, authorizationService, onlineSessions, appStore)

	if cfg.Admin.Enabled {
		appProxy := appproxy.New(
			cfg.ApplicationGateway,
			cfg.Admin,
			metadataDB,
			identityService,
			browserSessionService,
			authorizationService,
			logger,
		)
		go func() {
			errCh <- appProxy.ListenAndServe(ctx)
		}()
		resourceGrants, err := service.NewResourceGrantService(appStore, rbac.NewResourceGrantChecker(metadataDB))
		if err != nil {
			logger.Error("failed to initialize resource grant service", "error", err)
			os.Exit(1)
		}
		resourceGroups, err := service.NewResourceGroupService(appStore)
		if err != nil {
			logger.Error("failed to initialize resource group service", "error", err)
			os.Exit(1)
		}
		adminSrv, err := admin.New(
			cfg,
			appStore,
			metadataDB,
			identityService,
			browserSessionService,
			authorizationService,
			resourceGrants,
			resourceGroups,
			logger,
			dataDir,
			appProxy,
			onlineSessions,
		)
		if err != nil {
			logger.Error("failed to initialize admin server", "error", err)
			os.Exit(1)
		}
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
