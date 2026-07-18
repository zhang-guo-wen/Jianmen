package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"jianmen/internal/service"
	"jianmen/internal/store"
)

func newRuntimeLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

func newDatabaseProvisioningRuntime(
	appStore *store.DBStore,
	logger *slog.Logger,
) (*service.DatabaseProvisioningService, error) {
	return service.NewDatabaseProvisioningService(
		appStore,
		service.MySQLDatabaseProvisioner{},
		service.DatabaseProvisioningOptions{
			CleanupTimeout: 10 * time.Second,
			LeaseDuration:  30 * time.Second,
			Logger:         logger,
		},
	)
}

func startDatabaseProvisioningReconciler(
	ctx context.Context,
	errCh chan<- error,
	provisioning *service.DatabaseProvisioningService,
) {
	go func() {
		errCh <- provisioning.RunReconciler(ctx, time.Minute, 100)
	}()
}
