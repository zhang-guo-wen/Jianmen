package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/objectstore"
	"jianmen/internal/recording"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

const auditRetentionInterval = time.Hour

type auditRetentionRunner interface {
	RunOnce(ctx context.Context, now time.Time) (service.AuditRetentionResult, error)
}

func newAuditRetentionRuntime(
	cfg *config.Config,
	repository *store.DBStore,
	objects objectstore.Store,
) (*service.AuditRetentionService, error) {
	replayStorage, err := recording.NewReplayStorage(cfg.ReplayDir)
	if err != nil {
		return nil, fmt.Errorf("initialize replay storage: %w", err)
	}
	cleaner, err := service.NewAuditRetentionService(
		repository,
		replayStorage,
		service.NewAuditPolicy(cfg.Recording.RetentionDays, cfg.Recording.RecordInput),
		service.AuditRetentionOptions{
			BatchSize:      cfg.Recording.CleanupBatchSize,
			MaxReplayBytes: cfg.Recording.MaxReplayBytes,
			ObjectStorage:  objects,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("initialize audit retention service: %w", err)
	}
	return cleaner, nil
}

func startAuditRetentionRuntime(
	ctx context.Context,
	runner auditRetentionRunner,
	logger *slog.Logger,
	interval time.Duration,
) {
	if interval <= 0 {
		interval = auditRetentionInterval
	}
	go func() {
		runAuditRetention(ctx, runner, logger)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runAuditRetention(ctx, runner, logger)
			}
		}
	}()
}

func runAuditRetention(ctx context.Context, runner auditRetentionRunner, logger *slog.Logger) {
	result, err := runner.RunOnce(ctx, time.Now().UTC())
	if err != nil {
		logger.Warn("audit retention pass completed with errors",
			"claimed", result.Claimed,
			"deleted", result.Deleted,
			"freed_bytes", result.FreedBytes,
			"error", err,
		)
		return
	}
	if result.Claimed > 0 || result.QuotaExceeded {
		logger.Info("audit retention pass completed",
			"claimed", result.Claimed,
			"deleted", result.Deleted,
			"freed_bytes", result.FreedBytes,
			"usage_bytes", result.UsageBytes,
			"quota_exceeded", result.QuotaExceeded,
		)
	}
}
