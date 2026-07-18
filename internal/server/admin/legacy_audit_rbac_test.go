package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"jianmen/internal/model"
)

func TestLegacyAuditEndpointsRequireViewPermissions(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	if err := db.Create(&model.User{ID: "u-no-audit", Username: "no-audit", Status: "active"}).Error; err != nil {
		t.Fatalf("create test user: %v", err)
	}
	server.cfg.ReplayDir = t.TempDir()
	for _, path := range []string{
		filepath.Join(server.cfg.ReplayDir, "ssh", "ssh-1", "terminal.cast"),
		filepath.Join(server.cfg.ReplayDir, "ssh", "ssh-1", "commands.jsonl"),
		filepath.Join(server.cfg.ReplayDir, "ssh", "ssh-1", "files.jsonl"),
		filepath.Join(server.cfg.ReplayDir, "db", "db-1", "queries.jsonl"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create replay directory: %v", err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("create replay file: %v", err)
		}
	}

	tests := []struct {
		name string
		path string
		hand http.HandlerFunc
	}{
		{name: "database connections", path: "/api/db/connections", hand: server.handleDBConnections},
		{name: "database queries", path: "/api/db/connections/db-1/queries", hand: server.handleDBConnectionArtifact},
		{name: "session replay", path: "/api/sessions/ssh-1/replay", hand: server.handleSessionArtifact},
		{name: "session commands", path: "/api/sessions/ssh-1/commands", hand: server.handleSessionArtifact},
		{name: "session files", path: "/api/sessions/ssh-1/files", hand: server.handleSessionArtifact},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			ctx := req.Context()
			ctx = context.WithValue(ctx, ctxKeyUserID, "u-no-audit")
			ctx = context.WithValue(ctx, ctxKeyUsername, "no-audit")
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()
			tt.hand(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
			}
		})
	}
}
