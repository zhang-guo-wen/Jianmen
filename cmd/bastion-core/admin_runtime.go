package main

import (
	"context"
	"log/slog"

	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/objectstore"
	"jianmen/internal/online"
	"jianmen/internal/rbac"
	"jianmen/internal/server/admin"
	"jianmen/internal/server/appproxy"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

func startAdminRuntime(
	ctx context.Context,
	errCh chan<- error,
	cfg *config.Config,
	objects objectstore.Store,
	appStore *store.DBStore,
	metadataDB *gorm.DB,
	identity *service.IdentityService,
	browserSessions *service.BrowserSessionService,
	authorization *service.AuthorizationService,
	databaseProvisioning *service.DatabaseProvisioningService,
	logger *slog.Logger,
	dataDir string,
	onlineSessions *online.Registry,
) error {
	appProxy := appproxy.New(
		cfg.ApplicationGateway, cfg.Admin, metadataDB, identity,
		browserSessions, authorization, logger,
	)
	go func() {
		errCh <- appProxy.ListenAndServe(ctx)
	}()
	resourceGrants, err := service.NewResourceGrantService(
		appStore, rbac.NewResourceGrantChecker(metadataDB),
	)
	if err != nil {
		return err
	}
	resourceGroups, err := service.NewResourceGroupService(appStore)
	if err != nil {
		return err
	}
	webRuntime, err := newWebRDPRuntime(
		ctx, cfg, objects, appStore, identity, browserSessions, authorization,
		onlineSessions, logger,
	)
	if err != nil {
		return err
	}
	adminServer, err := admin.New(
		cfg, appStore, metadataDB, identity, browserSessions, authorization,
		resourceGrants, resourceGroups, databaseProvisioning, logger, dataDir,
		appProxy, onlineSessions, webRuntime.webRDP, webRuntime.accessRequests,
	)
	if err != nil {
		return err
	}
	go func() {
		errCh <- adminServer.ListenAndServe(ctx)
	}()
	return nil
}
