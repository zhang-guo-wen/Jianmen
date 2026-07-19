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
	"unicode/utf8"

	"jianmen/internal/model"
	"jianmen/internal/server/dbproxy"
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

func TestHandleAuditDBQueriesUsesBoundedServerPagination(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")

	now := time.Now().UTC()
	session := model.AuditSession{
		ID: "audit-query-page", UserID: "u1", Username: "alice",
		Protocol: "mysql", TargetName: "db-a", StartedAt: now, State: "ended",
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create audit session: %v", err)
	}
	queries := []model.AuditDBQuery{
		{ID: "query-1", AuditSessionID: session.ID, Timestamp: now, SQLText: "SELECT 1"},
		{ID: "query-2", AuditSessionID: session.ID, Timestamp: now.Add(time.Second), SQLText: "SELECT 2"},
		{ID: "query-3", AuditSessionID: session.ID, Timestamp: now.Add(2 * time.Second), SQLText: "SELECT 3"},
	}
	if err := db.Create(&queries).Error; err != nil {
		t.Fatalf("create database queries: %v", err)
	}

	req := asTestSuperAdmin(httptest.NewRequest(
		http.MethodGet,
		"/api/audit/mysql/"+session.ID+"/queries?page=2&page_size=1",
		nil,
	))
	rec := httptest.NewRecorder()
	server.handleAuditArtifact(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page struct {
		Items    []dbproxy.DBQueryEvent `json:"items"`
		Total    int64                  `json:"total"`
		Page     int                    `json:"page"`
		PageSize int                    `json:"page_size"`
	}
	if err := decodeTestData(t, rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode query page: %v", err)
	}
	if page.Total != 3 || page.Page != 2 || page.PageSize != 1 || len(page.Items) != 2 {
		t.Fatalf("unexpected query page: %#v", page)
	}
	started, finished := page.Items[0], page.Items[1]
	if started.Type != "query_started" || started.Seq != 1 || started.SQL != "SELECT 2" {
		t.Fatalf("unexpected query started event: %#v", started)
	}
	if finished.Type != "query_finished" || finished.Seq != 1 || finished.SQL != "" {
		t.Fatalf("query finished event duplicated SQL: %#v", finished)
	}
}

func TestHandleAuditDBQueriesReturnsUTF8SafeTruncatedPreviewMetadata(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")

	now := time.Now().UTC()
	session := model.AuditSession{
		ID: "audit-query-preview", UserID: "u1", Username: "alice",
		Protocol: "postgres", TargetName: "db-a", StartedAt: now, State: "ended",
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create audit session: %v", err)
	}
	sqlText := "SELECT '" + strings.Repeat("界", auditDBQuerySQLPreviewByteLimit/3+100) + "'"
	query := model.AuditDBQuery{
		ID: "query-large", AuditSessionID: session.ID, Timestamp: now,
		SQLText: sqlText, OriginalSQLBytes: 10 * 1024 * 1024,
		SQLTruncated: true,
	}
	if err := db.Create(&query).Error; err != nil {
		t.Fatalf("create database query: %v", err)
	}

	req := asTestSuperAdmin(httptest.NewRequest(
		http.MethodGet,
		"/api/audit/postgres/"+session.ID+"/queries?page_size=1000",
		nil,
	))
	rec := httptest.NewRecorder()
	server.handleAuditArtifact(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page struct {
		Items    []dbproxy.DBQueryEvent `json:"items"`
		Total    int64                  `json:"total"`
		PageSize int                    `json:"page_size"`
	}
	if err := decodeTestData(t, rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode query preview page: %v", err)
	}
	if page.Total != 1 || page.PageSize != auditDBQueryMaxPageSize || len(page.Items) != 2 {
		t.Fatalf("unexpected query preview page: %#v", page)
	}
	started, finished := page.Items[0], page.Items[1]
	if len(started.SQL) > auditDBQuerySQLPreviewByteLimit {
		t.Fatalf("preview bytes = %d, want <= %d", len(started.SQL), auditDBQuerySQLPreviewByteLimit)
	}
	if !utf8.ValidString(started.SQL) {
		t.Fatalf("preview is not valid UTF-8")
	}
	if !strings.HasSuffix(started.SQL, auditDBQuerySQLTruncatedMarker) {
		t.Fatalf("preview lacks explicit truncation marker: %q", started.SQL[len(started.SQL)-64:])
	}
	if finished.SQL != "" {
		t.Fatalf("finished event duplicated %d SQL bytes", len(finished.SQL))
	}
	if got, _ := started.Detail["sql_truncated"].(bool); !got {
		t.Fatalf("sql_truncated = %#v, want true", started.Detail["sql_truncated"])
	}
	if got, _ := started.Detail["sql_audit_truncated"].(bool); !got {
		t.Fatalf("sql_audit_truncated = %#v, want true", started.Detail["sql_audit_truncated"])
	}
	if got, _ := started.Detail["sql_preview_truncated"].(bool); !got {
		t.Fatalf("sql_preview_truncated = %#v, want true", started.Detail["sql_preview_truncated"])
	}
	if got := int64(started.Detail["sql_original_bytes"].(float64)); got != query.OriginalSQLBytes {
		t.Fatalf("sql_original_bytes = %d, want %d", got, query.OriginalSQLBytes)
	}
	if got := int64(started.Detail["sql_stored_bytes"].(float64)); got != int64(len(sqlText)) {
		t.Fatalf("sql_stored_bytes = %d, want %d", got, len(sqlText))
	}
	if got := int(started.Detail["sql_preview_bytes"].(float64)); got != len(started.SQL) {
		t.Fatalf("sql_preview_bytes = %d, want %d", got, len(started.SQL))
	}
	if _, exposed := started.Detail["sql_sha256"]; exposed {
		t.Fatalf("audit response exposed a reversible SQL fingerprint: %#v", started.Detail)
	}
	if rec.Body.Len() > auditDBQuerySQLPreviewByteLimit+8*1024 {
		t.Fatalf("bounded single-query response bytes = %d, want <= %d", rec.Body.Len(), auditDBQuerySQLPreviewByteLimit+8*1024)
	}
}

func TestHandleAuditDBQueriesSearchesBeforePagination(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")

	now := time.Now().UTC()
	session := model.AuditSession{
		ID: "audit-query-search", UserID: "u1", Username: "alice",
		Protocol: "redis", TargetName: "cache-a", StartedAt: now, State: "ended",
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create audit session: %v", err)
	}
	queries := []model.AuditDBQuery{
		{ID: "query-search-1", AuditSessionID: session.ID, Timestamp: now, SQLText: "GET customer:1"},
		{ID: "query-search-2", AuditSessionID: session.ID, Timestamp: now.Add(time.Second), SQLText: "GET session:1"},
		{ID: "query-search-3", AuditSessionID: session.ID, Timestamp: now.Add(2 * time.Second), SQLText: "DEL CUSTOMER:2"},
	}
	if err := db.Create(&queries).Error; err != nil {
		t.Fatalf("create database queries: %v", err)
	}

	req := asTestSuperAdmin(httptest.NewRequest(
		http.MethodGet,
		"/api/audit/redis/"+session.ID+"/queries?q=customer&page=1&page_size=1",
		nil,
	))
	rec := httptest.NewRecorder()
	server.handleAuditArtifact(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var page struct {
		Items []dbproxy.DBQueryEvent `json:"items"`
		Total int64                  `json:"total"`
	}
	if err := decodeTestData(t, rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode query search page: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 2 || page.Items[0].SQL != "GET customer:1" {
		t.Fatalf("unexpected query search page: %#v", page)
	}
}

func TestAuditDBQueryUTF8PrefixRepairsInvalidInputWithinByteLimit(t *testing.T) {
	got, changed := auditDBQueryUTF8Prefix("ab\xff\xfe界tail", 8)
	if !changed {
		t.Fatal("invalid and truncated input was not reported as changed")
	}
	if !utf8.ValidString(got) {
		t.Fatalf("prefix is not valid UTF-8: %q", got)
	}
	if len(got) > 8 {
		t.Fatalf("prefix bytes = %d, want <= 8", len(got))
	}
}

func TestAuditDBQuerySQLPreviewMarksInvalidUTF8AsTruncated(t *testing.T) {
	preview, detail := auditDBQuerySQLPreview(store.AuditDBQueryPreview{
		SQLText:        "SELECT \xff",
		SQLStoredBytes: int64(len("SELECT \xff")),
	})
	if !utf8.ValidString(preview) {
		t.Fatalf("preview is not valid UTF-8: %q", preview)
	}
	if !strings.HasSuffix(preview, auditDBQuerySQLTruncatedMarker) {
		t.Fatalf("preview lacks truncation marker: %q", preview)
	}
	if got, _ := detail["sql_preview_truncated"].(bool); !got {
		t.Fatalf("sql_preview_truncated = %#v, want true", detail["sql_preview_truncated"])
	}
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
