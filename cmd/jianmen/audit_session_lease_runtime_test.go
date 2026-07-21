package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type auditSessionLeaseMaintainerStub struct {
	mu           sync.Mutex
	heartbeats   int
	recoveries   int
	recovered    int64
	heartbeatErr error
	recoverErr   error
}

func (s *auditSessionLeaseMaintainerStub) Heartbeat(
	context.Context,
	time.Time,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeats++
	return s.heartbeatErr
}

func (s *auditSessionLeaseMaintainerStub) RecoverExpired(
	context.Context,
	time.Time,
) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recoveries++
	return s.recovered, s.recoverErr
}

func TestAuditSessionLeaseStartupRecoveryFailsClosed(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	maintainer := &auditSessionLeaseMaintainerStub{
		recoverErr: errors.New("database unavailable"),
	}
	err := recoverExpiredAuditSessionsAtStartup(
		context.Background(),
		maintainer,
		logger,
	)
	if err == nil || !strings.Contains(err.Error(), "startup audit session lease recovery") {
		t.Fatalf("startup recovery error = %v", err)
	}
}

func TestAuditSessionLeaseRuntimeStopsOnHeartbeatFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	maintainer := &auditSessionLeaseMaintainerStub{
		heartbeatErr: errors.New("database unavailable"),
	}
	errCh := make(chan error, 1)
	startAuditSessionLeaseRuntime(
		ctx,
		errCh,
		maintainer,
		logger,
		5*time.Millisecond,
	)

	select {
	case err := <-errCh:
		if !strings.Contains(err.Error(), "heartbeat failed closed") {
			t.Fatalf("runtime error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("audit lease runtime did not fail closed")
	}
	maintainer.mu.Lock()
	defer maintainer.mu.Unlock()
	if maintainer.heartbeats != 1 || maintainer.recoveries != 0 {
		t.Fatalf(
			"maintenance calls = heartbeat:%d recovery:%d",
			maintainer.heartbeats,
			maintainer.recoveries,
		)
	}
}

func TestAuditSessionLeaseRuntimeHeartbeatsBeforeRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	maintainer := &auditSessionLeaseMaintainerStub{}
	errCh := make(chan error, 1)
	startAuditSessionLeaseRuntime(
		ctx,
		errCh,
		maintainer,
		logger,
		5*time.Millisecond,
	)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		maintainer.mu.Lock()
		heartbeats := maintainer.heartbeats
		recoveries := maintainer.recoveries
		maintainer.mu.Unlock()
		if heartbeats > 0 && recoveries > 0 {
			cancel()
			return
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	t.Fatal("audit lease runtime did not complete heartbeat and recovery")
}
