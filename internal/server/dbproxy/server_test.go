package dbproxy

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"jianmen/internal/config"
)

func TestManagerApplyStartsAndStopsProxy(t *testing.T) {
	manager := NewManager(nil, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- manager.ListenAndServe(ctx)
	}()

	listenAddr := freeTCPAddr(t)
	if err := manager.Apply([]config.DatabaseProxyConfig{{
		Enabled:      true,
		Name:         "runtime-tcp",
		Protocol:     "tcp",
		ListenAddr:   listenAddr,
		UpstreamAddr: "127.0.0.1:1",
	}}); err != nil {
		t.Fatalf("Apply enabled proxy returned error: %v", err)
	}
	waitForDialState(t, listenAddr, true)

	if err := manager.Apply(nil); err != nil {
		t.Fatalf("Apply empty proxy list returned error: %v", err)
	}
	waitForDialState(t, listenAddr, false)

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ListenAndServe returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ListenAndServe did not stop after context cancellation")
	}
}

func freeTCPAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	defer listener.Close()
	return listener.Addr().String()
}

func waitForDialState(t *testing.T, addr string, wantOpen bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			if wantOpen {
				return
			}
		} else if !wantOpen {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	if wantOpen {
		t.Fatalf("proxy listener %s did not open", addr)
	}
	t.Fatalf("proxy listener %s did not close", addr)
}
