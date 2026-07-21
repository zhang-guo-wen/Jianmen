package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"jianmen/internal/config"
)

func TestDisabledManagedGuacdRuntimeIsNoop(t *testing.T) {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	manager, err := startManagedGuacdRuntime(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("startManagedGuacdRuntime() error = %v", err)
	}
	if err := manager.Wait(); err != nil {
		t.Fatalf("disabled manager Wait() error = %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("disabled manager Close() error = %v", err)
	}
}

func TestManagedGuacdProcessConfigConstructsSupervisedArguments(t *testing.T) {
	tests := []struct {
		name     string
		address  string
		wantBind string
		wantPort string
	}{
		{
			name:     "IPv4 loopback",
			address:  "127.0.0.1:4822",
			wantBind: "127.0.0.1",
			wantPort: "4822",
		},
		{
			name:     "IPv6 loopback",
			address:  "[::1]:4833",
			wantBind: "::1",
			wantPort: "4833",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				WebRDP: config.WebRDPConfig{
					Enabled:      true,
					GuacdAddress: tt.address,
					ManagedGuacd: config.ManagedGuacdConfig{
						Enabled:            true,
						BinaryPath:         "/opt/guacamole/sbin/guacd",
						WorkDir:            "/opt/guacamole",
						StartupTimeoutSecs: 23,
					},
				},
			}

			processConfig, err := managedGuacdProcessConfig(cfg)
			if err != nil {
				t.Fatalf("managedGuacdProcessConfig() error = %v", err)
			}
			if !processConfig.Enabled {
				t.Fatal("process config must be enabled")
			}
			if processConfig.Command != "/opt/guacamole/sbin/guacd" {
				t.Fatalf("Command = %q", processConfig.Command)
			}
			wantArgs := []string{
				"-f", "-b", tt.wantBind, "-l", tt.wantPort, "-L", "info",
			}
			if !slices.Equal(processConfig.Args, wantArgs) {
				t.Fatalf("Args = %q, want %q", processConfig.Args, wantArgs)
			}
			if processConfig.WorkDir != "/opt/guacamole" {
				t.Fatalf("WorkDir = %q", processConfig.WorkDir)
			}
			if processConfig.ReadyAddress != tt.address {
				t.Fatalf("ReadyAddress = %q, want %q", processConfig.ReadyAddress, tt.address)
			}
			if processConfig.StartupTimeout != 23*time.Second {
				t.Fatalf("StartupTimeout = %v", processConfig.StartupTimeout)
			}
		})
	}
}

func TestMonitorManagedGuacdPropagatesUnexpectedExit(t *testing.T) {
	waitErr := errors.New("guacd crashed")
	manager := &fakeManagedGuacdRuntime{waitErr: waitErr}
	errCh := make(chan error, 1)

	monitorManagedGuacd(context.Background(), errCh, manager)

	select {
	case err := <-errCh:
		if !errors.Is(err, waitErr) {
			t.Fatalf("monitor error = %v, want wrapping %v", err, waitErr)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for managed guacd error")
	}
}

func TestWaitForRuntimeClosesManagedGuacdOnContextCancellation(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	stop()
	manager := &fakeManagedGuacdRuntime{}

	err := waitForRuntime(
		ctx,
		func() {},
		make(chan error),
		manager,
		discardRuntimeLogger(),
	)
	if err != nil {
		t.Fatalf("waitForRuntime() error = %v", err)
	}
	if manager.closeCalls != 1 {
		t.Fatalf("Close() calls = %d, want 1", manager.closeCalls)
	}
}

func TestWaitForRuntimeReturnsCloseErrorOnContextCancellation(t *testing.T) {
	closeErr := errors.New("cannot stop guacd")
	ctx, stop := context.WithCancel(context.Background())
	stop()
	manager := &fakeManagedGuacdRuntime{closeErr: closeErr}

	err := waitForRuntime(
		ctx,
		func() {},
		make(chan error),
		manager,
		discardRuntimeLogger(),
	)
	if !errors.Is(err, closeErr) {
		t.Fatalf("waitForRuntime() error = %v, want wrapping %v", err, closeErr)
	}
}

func TestWaitForRuntimeJoinsRuntimeAndCloseErrors(t *testing.T) {
	runtimeErr := errors.New("runtime failed")
	closeErr := errors.New("cannot stop guacd")
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	errCh <- runtimeErr
	manager := &fakeManagedGuacdRuntime{closeErr: closeErr}

	err := waitForRuntime(ctx, cancel, errCh, manager, discardRuntimeLogger())
	if !errors.Is(err, runtimeErr) {
		t.Errorf("waitForRuntime() error = %v, want wrapping %v", err, runtimeErr)
	}
	if !errors.Is(err, closeErr) {
		t.Errorf("waitForRuntime() error = %v, want wrapping %v", err, closeErr)
	}
	if ctx.Err() == nil {
		t.Fatal("runtime context was not canceled")
	}
}

type fakeManagedGuacdRuntime struct {
	waitErr    error
	closeErr   error
	closeCalls int
}

func (f *fakeManagedGuacdRuntime) Wait() error {
	return f.waitErr
}

func (f *fakeManagedGuacdRuntime) Close() error {
	f.closeCalls++
	return f.closeErr
}

func discardRuntimeLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
