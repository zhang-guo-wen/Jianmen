package main

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"jianmen/internal/service"
)

type auditRetentionRunnerStub struct {
	mu    sync.Mutex
	calls int
	done  chan struct{}
}

func (s *auditRetentionRunnerStub) RunOnce(context.Context, time.Time) (service.AuditRetentionResult, error) {
	s.mu.Lock()
	s.calls++
	if s.calls == 1 {
		close(s.done)
	}
	s.mu.Unlock()
	return service.AuditRetentionResult{}, nil
}

func TestAuditRetentionRuntimeRunsImmediatelyAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &auditRetentionRunnerStub{done: make(chan struct{})}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	startAuditRetentionRuntime(ctx, runner, logger, time.Hour)

	select {
	case <-runner.done:
	case <-time.After(time.Second):
		t.Fatal("audit retention did not run at startup")
	}
	cancel()
	time.Sleep(20 * time.Millisecond)

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.calls != 1 {
		t.Fatalf("audit retention calls = %d, want 1", runner.calls)
	}
}
