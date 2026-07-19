package dbproxy

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRetentionDatabaseReplayUsesAuditSessionID(t *testing.T) {
	root := t.TempDir()
	gateway := &Gateway{
		replayDir: root,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	recorder, err := gateway.newRecorder(&gatewayConn{
		protocol:     "mysql",
		accountName:  "orders-reader",
		accountUser:  "reader",
		instanceName: "orders",
		upstreamAddr: "127.0.0.1:3306",
	}, "audit-session-1", func(error) {})
	if err != nil {
		t.Fatalf("newRecorder: %v", err)
	}
	t.Cleanup(func() { _ = recorder.Close() })

	if recorder.id != "audit-session-1" {
		t.Fatalf("recorder ID = %q", recorder.id)
	}
	wantMeta := filepath.Join(root, "db", "audit-session-1", "meta.json")
	if recorder.metaPath != wantMeta {
		t.Fatalf("recorder meta path = %q, want %q", recorder.metaPath, wantMeta)
	}
	fallback, err := gateway.newRecorder(&gatewayConn{protocol: "mysql"}, "", func(error) {})
	if err != nil {
		t.Fatalf("newRecorder without audit session: %v", err)
	}
	defer fallback.Close()
	if fallback.id == "" {
		t.Fatal("fallback recorder ID is empty")
	}
}

func TestRetentionDatabaseRecorderFailsClosedOnReplayErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Run("initialization", func(t *testing.T) {
		rootFile := filepath.Join(t.TempDir(), "replay-file")
		if err := os.WriteFile(rootFile, []byte("blocked"), 0o600); err != nil {
			t.Fatalf("write replay root file: %v", err)
		}
		gateway := &Gateway{replayDir: rootFile, logger: logger}
		if _, err := gateway.newRecorder(
			&gatewayConn{protocol: "mysql"},
			"audit-session",
			func(error) {},
		); err == nil {
			t.Fatal("newRecorder accepted a non-directory replay root")
		}
	})

	t.Run("runtime write", func(t *testing.T) {
		gateway := &Gateway{replayDir: t.TempDir(), logger: logger}
		fatal := make(chan error, 1)
		recorder, err := gateway.newRecorder(
			&gatewayConn{protocol: "mysql"},
			"audit-session",
			func(err error) { fatal <- err },
		)
		if err != nil {
			t.Fatalf("newRecorder: %v", err)
		}
		if err := recorder.file.Close(); err != nil {
			t.Fatalf("close query audit file: %v", err)
		}
		_, decision := recorder.StartQuery("select 1", nil)
		if decision.Allowed || decision.ErrorCode != observerErrorAuditFailure {
			t.Fatalf("decision = %#v", decision)
		}
		select {
		case err := <-fatal:
			if !strings.Contains(err.Error(), "query start") {
				t.Fatalf("fatal error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("database recorder did not signal fatal failure")
		}
	})
}
