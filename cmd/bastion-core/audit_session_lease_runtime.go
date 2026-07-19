package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"jianmen/internal/service"
)

const auditSessionLeaseHeartbeatInterval = 30 * time.Second

type auditSessionLeaseMaintainer interface {
	Heartbeat(ctx context.Context, now time.Time) error
	RecoverExpired(ctx context.Context, now time.Time) (int64, error)
}

func recoverExpiredAuditSessionsAtStartup(
	ctx context.Context,
	maintainer auditSessionLeaseMaintainer,
	logger *slog.Logger,
) error {
	recovered, err := maintainer.RecoverExpired(ctx, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("startup audit session lease recovery: %w", err)
	}
	if recovered > 0 {
		logger.Warn("recovered expired audit sessions", "count", recovered)
	}
	return nil
}

func startAuditSessionLeaseRuntime(
	ctx context.Context,
	errCh chan<- error,
	maintainer auditSessionLeaseMaintainer,
	logger *slog.Logger,
	interval time.Duration,
) {
	if interval <= 0 {
		interval = auditSessionLeaseHeartbeatInterval
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				now = now.UTC()
				if err := maintainer.Heartbeat(ctx, now); err != nil {
					errCh <- fmt.Errorf("audit session heartbeat failed closed: %w", err)
					return
				}
				recovered, err := maintainer.RecoverExpired(ctx, now)
				if err != nil {
					errCh <- fmt.Errorf("periodic audit session lease recovery: %w", err)
					return
				}
				if recovered > 0 {
					logger.Warn("recovered expired audit sessions", "count", recovered)
				}
			}
		}
	}()
}

var _ auditSessionLeaseMaintainer = (*service.AuditSessionLeaseService)(nil)
