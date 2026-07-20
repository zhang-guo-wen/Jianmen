package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"jianmen/internal/config"
	"jianmen/internal/online"
	"jianmen/internal/rbac"
	"jianmen/internal/server/dbproxy"
	"jianmen/internal/server/sshserver"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

func main() {
	configPath := flag.String("config", "config.local.json", "path to config file")
	flag.Parse()
	logger := newRuntimeLogger()
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
	systemSettings, err := bootstrapSystemSettings(
		context.Background(),
		cfg,
		appStore,
		func(effective *config.Config) error {
			return prepareDatabaseGatewayTLS(effective, dataDir, logger)
		},
	)
	if err != nil {
		logger.Error("failed to initialize system settings", "error", err)
		os.Exit(1)
	}
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
	databaseProvisioning, err := newDatabaseProvisioningRuntime(appStore, logger)
	if err != nil {
		logger.Error("failed to initialize database provisioning service")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 5)
	onlineSessions := online.NewRegistry()
	auditLeases, err := service.NewAuditSessionLeaseService(appStore)
	if err != nil {
		logger.Error("failed to initialize audit session leases", "error", err)
		os.Exit(1)
	}
	if err := recoverExpiredAuditSessionsAtStartup(ctx, auditLeases, logger); err != nil {
		logger.Error("failed to recover audit session leases", "error", err)
		os.Exit(1)
	}
	startAuditSessionLeaseRuntime(
		ctx,
		errCh,
		auditLeases,
		logger,
		auditSessionLeaseHeartbeatInterval,
	)
	rdpObjects, err := newRDPObjectStore(ctx, cfg)
	if err != nil {
		logger.Error("failed to initialize RDP object storage", "error", err)
		os.Exit(1)
	}
	systemSettingsDiagnostics, err := newSystemSettingsDiagnostics(cfg, rdpObjects)
	if err != nil {
		logger.Error("failed to initialize system settings diagnostics", "error", err)
		os.Exit(1)
	}
	auditRetention, err := newAuditRetentionRuntime(cfg, appStore, rdpObjects)
	if err != nil {
		logger.Error("failed to initialize audit retention", "error", err)
		os.Exit(1)
	}
	startAuditRetentionRuntime(ctx, auditRetention, logger, auditRetentionInterval)
	startDatabaseProvisioningReconciler(ctx, errCh, databaseProvisioning)
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
		if err := startAdminRuntime(
			ctx, errCh, cfg, rdpObjects, appStore, metadataDB, identityService,
			browserSessionService, authorizationService, databaseProvisioning,
			systemSettings, systemSettingsDiagnostics, logger, dataDir, onlineSessions,
		); err != nil {
			logger.Error("failed to initialize admin server", "error", err)
			os.Exit(1)
		}
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
