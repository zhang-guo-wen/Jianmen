package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/guacd"
)

type managedGuacdRuntime interface {
	Wait() error
	Close() error
}

func startManagedGuacdRuntime(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
) (managedGuacdRuntime, error) {
	processConfig, err := managedGuacdProcessConfig(cfg)
	if err != nil {
		return nil, err
	}
	manager, err := guacd.Start(ctx, processConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("start managed guacd runtime: %w", err)
	}
	return manager, nil
}

func managedGuacdProcessConfig(cfg *config.Config) (guacd.Config, error) {
	managed := cfg.WebRDP.ManagedGuacd
	enabled := cfg.WebRDP.Enabled && managed.Enabled
	if !enabled {
		return guacd.Config{}, nil
	}
	host, port, err := net.SplitHostPort(cfg.WebRDP.GuacdAddress)
	if err != nil {
		return guacd.Config{}, fmt.Errorf(
			"parse managed guacd address %q: %w",
			cfg.WebRDP.GuacdAddress,
			err,
		)
	}
	return guacd.Config{
		Enabled:        enabled,
		Command:        managed.BinaryPath,
		Args:           []string{"-f", "-b", host, "-l", port, "-L", "info"},
		WorkDir:        managed.WorkDir,
		ReadyAddress:   cfg.WebRDP.GuacdAddress,
		StartupTimeout: time.Duration(managed.StartupTimeoutSecs) * time.Second,
	}, nil
}

func monitorManagedGuacd(
	ctx context.Context,
	errCh chan<- error,
	manager managedGuacdRuntime,
) {
	go func() {
		err := manager.Wait()
		if err != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("managed guacd runtime stopped: %w", err)
		}
	}()
}

func waitForRuntime(
	ctx context.Context,
	cancel context.CancelFunc,
	errCh <-chan error,
	manager managedGuacdRuntime,
	logger *slog.Logger,
) error {
	select {
	case <-ctx.Done():
		closeErr := wrapManagedGuacdCloseError(manager.Close())
		if closeErr != nil {
			logger.Error("failed to stop managed guacd", "error", closeErr)
		}
		return closeErr
	case runtimeErr := <-errCh:
		running := ctx.Err() == nil
		cancel()
		closeErr := wrapManagedGuacdCloseError(manager.Close())
		if closeErr != nil {
			logger.Error("failed to stop managed guacd", "error", closeErr)
		}
		if running {
			return errors.Join(runtimeErr, closeErr)
		}
		return closeErr
	}
}

func wrapManagedGuacdCloseError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("stop managed guacd: %w", err)
}
