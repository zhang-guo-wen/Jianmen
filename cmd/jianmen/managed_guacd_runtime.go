package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/guacd"
	"jianmen/internal/guacdruntime"
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
	command := managed.BinaryPath
	args := []string{"-f", "-b", host, "-l", port, "-L", "info"}
	workDir := managed.WorkDir
	var environment map[string]string
	if strings.EqualFold(strings.TrimSpace(command), guacdruntime.EmbeddedBinaryPath) {
		runtimeDir := filepath.Join(filepath.Dir(cfg.WebRDP.SpoolDir), "runtime", "guacd")
		prepared, prepareErr := guacdruntime.Prepare(runtimeDir)
		if prepareErr != nil {
			return guacd.Config{}, fmt.Errorf("prepare embedded guacd runtime: %w", prepareErr)
		}
		command = prepared.Command
		args = append(append([]string{}, prepared.ArgsPrefix...), args...)
		workDir = prepared.WorkDir
		environment = prepared.Env
	}
	return guacd.Config{
		Enabled:        enabled,
		Command:        command,
		Args:           args,
		Env:            environment,
		WorkDir:        workDir,
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
