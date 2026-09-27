package admin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

func TestHandleAuditDBRejectsNonDBProtocolForDBAuditOnlyUser(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedConnectionAction(t, db, "db-auditor", rbac.ActionDBAuditView)
	if err := db.Create(&model.AuditSession{
		ID: "ssh-must-not-leak", Protocol: "ssh", State: "ended", StartedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("create SSH audit session: %v", err)
	}

	request := asTestUser(
		httptest.NewRequest(http.MethodGet, "/api/audit/db?protocol=ssh", nil),
		"db-auditor",
		"db-auditor",
	)
	recorder := httptest.NewRecorder()
	server.handleAuditDB(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "ssh-must-not-leak") {
		t.Fatalf("invalid DB protocol returned repository data: %s", recorder.Body.String())
	}
}

func TestHandleAuditArtifactRejectsCrossFamilyAndRDPReads(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")
	replayDir := t.TempDir()
	const marker = "must-not-be-streamed"
	if err := os.WriteFile(filepath.Join(replayDir, "terminal.cast"), []byte(marker), 0o600); err != nil {
		t.Fatalf("write replay: %v", err)
	}
	if err := os.WriteFile(filepath.Join(replayDir, "commands.jsonl"), []byte(`{"command":"must-not-run"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write commands: %v", err)
	}
	sessions := []model.AuditSession{
		{ID: "ssh-session", Protocol: "ssh", State: "ended", StartedAt: time.Now().UTC(), ReplayDir: replayDir},
		{ID: "db-session", Protocol: "mysql", State: "ended", StartedAt: time.Now().UTC(), ReplayDir: replayDir},
		{ID: "rdp-session", Protocol: "rdp", State: "ended", StartedAt: time.Now().UTC(), ReplayDir: replayDir},
	}
	if err := db.Create(&sessions).Error; err != nil {
		t.Fatalf("create audit sessions: %v", err)
	}

	for _, path := range []string{
		"/api/audit/mysql/ssh-session/queries",
		"/api/audit/ssh/ssh-session/queries",
		"/api/audit/ssh/db-session/commands",
		"/api/audit/mysql/db-session/replay",
		"/api/audit/ssh/rdp-session/commands",
		"/api/audit/ssh/rdp-session/replay",
		"/api/audit/rdp/rdp-session",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.handleAuditArtifact(recorder, asTestSuperAdmin(httptest.NewRequest(http.MethodGet, path, nil)))
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status=%d, want 404; body=%s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), "audit session unavailable") {
				t.Fatalf("response does not use uniform unavailable result: %s", recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), marker) || strings.Contains(recorder.Body.String(), "must-not-run") {
				t.Fatalf("cross-family artifact read leaked file data: %s", recorder.Body.String())
			}
		})
	}

	if err := db.Create(&model.User{ID: "no-audit", Username: "no-audit", Status: "active"}).Error; err != nil {
		t.Fatalf("create unauthorized user: %v", err)
	}
	for name, request := range map[string]*http.Request{
		"unauthorized": asTestUser(httptest.NewRequest(http.MethodGet, "/api/audit/ssh/ssh-session/replay", nil), "no-audit", "no-audit"),
		"not found":    asTestSuperAdmin(httptest.NewRequest(http.MethodGet, "/api/audit/ssh/missing-session/replay", nil)),
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.handleAuditArtifact(recorder, request)
			if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), "audit session unavailable") {
				t.Fatalf("status=%d body=%s, want uniform unavailable", recorder.Code, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), marker) {
				t.Fatalf("unavailable artifact leaked file data: %s", recorder.Body.String())
			}
		})
	}
}
