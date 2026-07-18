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
	seedTestSuperAdmin(t, db, "u-admin")

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

func TestHandleAuditSearchIncludesCommandAndSQLContent(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")

	now := time.Now().UTC()
	sshSession := model.AuditSession{ID: "audit-command-search", UserID: "u1", Username: "alice", Protocol: "ssh", TargetName: "host-a", StartedAt: now, State: "ended"}
	dbSession := model.AuditSession{ID: "audit-sql-search", UserID: "u1", Username: "alice", Protocol: "mysql", TargetName: "db-a", StartedAt: now.Add(-time.Minute), State: "ended"}
	if err := db.Create(&[]model.AuditSession{sshSession, dbSession}).Error; err != nil {
		t.Fatalf("create audit sessions: %v", err)
	}
	if err := db.Create(&model.AuditSSHCommand{AuditSessionID: sshSession.ID, Timestamp: now, Command: "kubectl get pods"}).Error; err != nil {
		t.Fatalf("create SSH command: %v", err)
	}
	if err := db.Create(&model.AuditDBQuery{AuditSessionID: dbSession.ID, Timestamp: now, SQLText: "SELECT * FROM customer_orders"}).Error; err != nil {
		t.Fatalf("create database query: %v", err)
	}

	assertAuditSearchResult := func(path, expectedID string) {
		t.Helper()
		req := asTestSuperAdmin(httptest.NewRequest(http.MethodGet, path, nil))
		rec := httptest.NewRecorder()
		if strings.HasPrefix(path, "/api/audit/ssh") {
			server.handleAuditSSH(rec, req)
		} else {
			server.handleAuditDB(rec, req)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("search status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var page struct {
			Items []store.AuditSessionView `json:"items"`
			Total int64                    `json:"total"`
		}
		if err := decodeTestData(t, rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode search result: %v", err)
		}
		if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != expectedID {
			t.Fatalf("unexpected search result for %s: %#v", path, page)
		}
	}

	assertAuditSearchResult("/api/audit/ssh?q=KUBECTL", sshSession.ID)
	assertAuditSearchResult("/api/audit/db?q=CUSTOMER_ORDERS", dbSession.ID)
}

func TestHandleAuditListsIncludeLogCounts(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")

	now := time.Now().UTC()
	sshSession := model.AuditSession{ID: "audit-count-ssh", UserID: "u1", Username: "alice", Protocol: "ssh", TargetName: "host-a", StartedAt: now, State: "ended"}
	sftpSession := model.AuditSession{ID: "audit-count-sftp", UserID: "u1", Username: "alice", Protocol: "ssh", ProtocolSubtype: "sftp", TargetName: "host-b", StartedAt: now.Add(-time.Minute), State: "ended"}
	dbSession := model.AuditSession{ID: "audit-count-db", UserID: "u1", Username: "alice", Protocol: "mysql", TargetName: "db-a", StartedAt: now.Add(-2 * time.Minute), State: "ended"}
	if err := db.Create(&[]model.AuditSession{sshSession, sftpSession, dbSession}).Error; err != nil {
		t.Fatalf("create audit sessions: %v", err)
	}
	if err := db.Create(&[]model.AuditSSHCommand{
		{AuditSessionID: sshSession.ID, Timestamp: now, Command: "whoami"},
		{AuditSessionID: sshSession.ID, Timestamp: now.Add(time.Second), Command: "hostname"},
		{AuditSessionID: sftpSession.ID, Timestamp: now, Command: "ignored for sftp count"},
	}).Error; err != nil {
		t.Fatalf("create SSH commands: %v", err)
	}
	if err := db.Create(&[]model.AuditSFTPEvent{
		{AuditSessionID: sftpSession.ID, Timestamp: now, Action: "open_read", Path: "/tmp/a", Result: "success"},
		{AuditSessionID: sftpSession.ID, Timestamp: now.Add(time.Second), Action: "read", Path: "/tmp/a", Result: "success"},
		{AuditSessionID: sftpSession.ID, Timestamp: now.Add(2 * time.Second), Action: "close", Path: "/tmp/a", Result: "success"},
	}).Error; err != nil {
		t.Fatalf("create SFTP events: %v", err)
	}
	if err := db.Create(&[]model.AuditDBQuery{
		{AuditSessionID: dbSession.ID, Timestamp: now, SQLText: "SELECT 1"},
		{AuditSessionID: dbSession.ID, Timestamp: now.Add(time.Second), SQLText: "SELECT 2"},
	}).Error; err != nil {
		t.Fatalf("create database queries: %v", err)
	}

	assertCounts := func(path string, handler http.HandlerFunc, expected map[string]int64) {
		t.Helper()
		req := asTestSuperAdmin(httptest.NewRequest(http.MethodGet, path, nil))
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var page struct {
			Items []store.AuditSessionView `json:"items"`
		}
		if err := decodeTestData(t, rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode audit page: %v", err)
		}
		for _, item := range page.Items {
			want, ok := expected[item.ID]
			if !ok {
				continue
			}
			if item.LogCount != want {
				t.Fatalf("session %s log_count = %d, want %d", item.ID, item.LogCount, want)
			}
			delete(expected, item.ID)
		}
		if len(expected) != 0 {
			t.Fatalf("missing audit sessions: %#v", expected)
		}
	}

	assertCounts("/api/audit/ssh?page_size=20", server.handleAuditSSH, map[string]int64{
		sshSession.ID:  2,
		sftpSession.ID: 3,
	})
	assertCounts("/api/audit/db?page_size=20", server.handleAuditDB, map[string]int64{
		dbSession.ID: 2,
	})
}

func TestHandleAuditSSHCommandsLoadsOutputFromRecordingFile(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")

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
