package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/store"
)

func TestHandleAuditSSHUsesStandardPaginationAndSearchParams(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	server.superAdminIDs["u-admin"] = true

	now := time.Now().UTC()
	sessions := []model.AuditSession{
		{ID: "audit-alpha-1", UserID: "u1", Username: "alice", Protocol: "ssh", TargetName: "alpha-host", StartedAt: now, State: "ended"},
		{ID: "audit-alpha-2", UserID: "u2", Username: "bob", Protocol: "ssh", TargetName: "alpha-db", TargetAddress: "10.0.0.2:22", AccountName: "operations", AccountUsername: "root", StartedAt: now.Add(-time.Minute), State: "ended"},
		{ID: "audit-beta", UserID: "u3", Username: "carol", Protocol: "ssh", TargetName: "beta-host", StartedAt: now.Add(-2 * time.Minute), State: "ended"},
	}
	if err := db.Create(&sessions).Error; err != nil {
		t.Fatalf("create audit sessions: %v", err)
	}

	req := asTestSuperAdmin(httptest.NewRequest(http.MethodGet, "/api/audit/ssh?page=2&page_size=1&q=alpha", nil))
	rec := httptest.NewRecorder()
	server.handleAuditSSH(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page struct {
		Items    []store.AuditSessionView `json:"items"`
		Total    int64                    `json:"total"`
		Page     int                      `json:"page"`
		PageSize int                      `json:"page_size"`
	}
	if err := decodeTestData(t, rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode audit page: %v", err)
	}
	if page.Total != 2 || page.Page != 2 || page.PageSize != 1 || len(page.Items) != 1 {
		t.Fatalf("unexpected audit page: %#v", page)
	}
	if page.Items[0].TargetName != "alpha-db" || page.Items[0].TargetAddress != "10.0.0.2:22" {
		t.Fatalf("unexpected audit target: %#v", page.Items[0])
	}
	if page.Items[0].AccountUsername != "root" || page.Items[0].AccountName != "operations" {
		t.Fatalf("unexpected audit account: %#v", page.Items[0])
	}
}

func TestHandleAuditSSHCommandsLoadsOutputFromRecordingFile(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	server.superAdminIDs["u-admin"] = true

	replayDir := t.TempDir()
	startedAt := time.Now().UTC().Add(-time.Minute)
	session := model.AuditSession{
		ID:         "audit-command-output",
		UserID:     "u1",
		Username:   "alice",
		Protocol:   "ssh",
		TargetName: "alpha-host",
		StartedAt:  startedAt,
		State:      "ended",
		ReplayDir:  replayDir,
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create audit session: %v", err)
	}

	recorded := map[string]any{
		"seq":        1,
		"offset_ms":  120,
		"command":    "whoami",
		"preview":    "alice\n",
		"confidence": "partial",
		"started_at": startedAt.UnixMilli(),
		"ended_at":   startedAt.Add(time.Second).UnixMilli(),
	}
	raw, err := json.Marshal(recorded)
	if err != nil {
		t.Fatalf("marshal recorded command: %v", err)
	}
	if err := os.WriteFile(filepath.Join(replayDir, "commands.jsonl"), append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write command recording: %v", err)
	}

	req := asTestSuperAdmin(httptest.NewRequest(http.MethodGet, "/api/audit/ssh/"+session.ID+"/commands", nil))
	rec := httptest.NewRecorder()
	server.handleAuditArtifact(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var page struct {
		Items []struct {
			Command string `json:"command"`
			Output  string `json:"output"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := decodeTestData(t, rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode command output page: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Command != "whoami" || page.Items[0].Output != "alice\n" {
		t.Fatalf("unexpected command output page: %#v", page)
	}
	if strings.Contains(rec.Body.String(), `"preview"`) {
		t.Fatalf("response still exposes preview field: %s", rec.Body.String())
	}
}
