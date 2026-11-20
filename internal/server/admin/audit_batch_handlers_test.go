package admin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

type batchTestPage struct {
	Total int `json:"total"`
	Items []struct {
		ID       string `json:"id"`
		BatchID  string `json:"batch_id"`
		Source   string `json:"audit_session_id"`
		Kind     string `json:"kind"`
		Sessions int    `json:"session_count"`
		Failures int    `json:"failure_count"`
		Logs     int    `json:"log_count"`
	} `json:"items"`
}

func batchTestRequest(t *testing.T, s *Server, path, user string, want int) (*httptest.ResponseRecorder, batchTestPage) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if user == "u-admin" {
		r = asTestSuperAdmin(r)
	} else {
		r = asTestUser(r, user, user)
	}
	w := httptest.NewRecorder()
	if strings.HasPrefix(path, "/api/audit/batches/") {
		s.handleAuditBatches(w, r)
	} else if strings.HasPrefix(path, "/api/audit/db") {
		s.handleAuditDB(w, r)
	} else {
		s.handleAuditSSH(w, r)
	}
	if w.Code != want {
		t.Fatalf("%s for %s: code=%d want %d body=%s", path, user, w.Code, want, w.Body)
	}
	var p batchTestPage
	if want == http.StatusOK {
		if err := decodeTestData(t, w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
	}
	return w, p
}

func TestAuditBatchListsAfterMatch(t *testing.T) {
	s, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")
	now := time.Now().UTC()
	if err := db.Create(&[]model.AuditSession{
		{ID: "db1", BatchID: "test-batch", Protocol: "mysql", State: "ended", StartedAt: now, Outcome: model.AuditOutcomeFailed},
		{ID: "db2", BatchID: "test-batch", Protocol: "mysql", State: "ended", StartedAt: now.Add(time.Minute), Outcome: model.AuditOutcomeSucceeded},
		{ID: "db3", BatchID: "new-batch", Protocol: "mysql", State: "ended", StartedAt: now.Add(2 * time.Minute)},
		{ID: "legacy", Protocol: "mysql", State: "ended", StartedAt: now},
		{ID: "ssh1", BatchID: "ssh-batch", Protocol: "ssh", State: "ended", StartedAt: now},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AuditDBQuery{AuditSessionID: "db1", Timestamp: now, SQLText: "SELECT target_match"}).Error; err != nil {
		t.Fatal(err)
	}
	_, p := batchTestRequest(t, s, "/api/audit/batches/db?q=TARGET_MATCH", "u-admin", 200)
	if p.Total != 1 || len(p.Items) != 1 || p.Items[0].BatchID != "test-batch" || p.Items[0].Sessions != 2 || p.Items[0].Logs != 1 || p.Items[0].Failures != 1 {
		t.Fatalf("wrong matched aggregate: %+v", p)
	}
	_, p = batchTestRequest(t, s, "/api/audit/batches/db?page=2&page_size=1", "u-admin", 200)
	if p.Total != 2 || len(p.Items) != 1 || p.Items[0].BatchID != "test-batch" {
		t.Fatalf("wrong batch page: %+v", p)
	}
	_, p = batchTestRequest(t, s, "/api/audit/db?batch_id=test-batch", "u-admin", 200)
	if p.Total != 2 {
		t.Fatalf("drill-down includes other batches: %+v", p)
	}
	_, p = batchTestRequest(t, s, "/api/audit/db", "u-admin", 200)
	if p.Total != 4 {
		t.Fatalf("session view omitted old audit: %+v", p)
	}
}

func TestAuditBatchLogsSearchBeforePagination(t *testing.T) {
	s, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")
	now := time.Now().UTC()
	if err := db.Create(&[]model.AuditSession{{ID: "d1", BatchID: "db-batch", Protocol: "postgres", State: "ended", StartedAt: now}, {ID: "s1", BatchID: "ssh-batch", Protocol: "ssh", State: "ended", StartedAt: now}, {ID: "s2", BatchID: "ssh-batch", Protocol: "ssh", ProtocolSubtype: "sftp", State: "ended", StartedAt: now}}).Error; err != nil {
		t.Fatal(err)
	}
	for i, keys := range []struct{ query, cmd, path string }{
		{"SELECT 'Needle%!_needle'", "whoami Needle%!_needle", "/file/Needle%!_needle"},
		{"SELECT 'NeedleX_needle'", "whoami other", "/root/other"},
		{strings.Repeat("x", 64*1024) + "hidden-sql-suffix", "pwd", "/tmp/other"},
	} {
		ts := now.Add(time.Duration(i) * time.Second)
		for _, row := range []any{
			&model.AuditDBQuery{ID: fmt.Sprintf("q%d", i), AuditSessionID: "d1", Timestamp: ts, SQLText: keys.query},
			&model.AuditSSHCommand{ID: fmt.Sprintf("c%d", i), AuditSessionID: "s1", Timestamp: ts, Command: keys.cmd},
			&model.AuditSFTPEvent{ID: fmt.Sprintf("f%d", i), AuditSessionID: "s2", Timestamp: ts, Action: "read", Path: keys.path},
		} {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		route string
		total int
		ids   []string
	}{{"db/db-batch", 3, []string{"q0"}}, {"ssh/ssh-batch", 6, []string{"c0", "f0"}}} {
		base := "/api/audit/batches/" + tc.route + "/logs"
		_, p := batchTestRequest(t, s, base+"?page_size=1", "u-admin", 200)
		if p.Total != tc.total || len(p.Items) != 1 || slices.Contains(tc.ids, p.Items[0].ID) {
			t.Fatalf("fixture missing late-page log: %+v", p)
		}
		_, p = batchTestRequest(t, s, base+"?page_size=2&q="+url.QueryEscape("NEEDLE%!_needle"), "u-admin", 200)
		var ids []string
		for _, it := range p.Items {
			ids = append(ids, it.ID)
			if it.Source == "" {
				t.Fatal("missing source audit session")
			}
		}
		if p.Total != len(tc.ids) || !slices.Equal(ids, tc.ids) {
			t.Fatalf("wrong match: %+v", p)
		}
		_, p = batchTestRequest(t, s, base+"?q=hidden-sql-suffix", "u-admin", 200)
		if p.Total != 0 || len(p.Items) != 0 {
			t.Fatalf("unexpected content leak: %+v", p)
		}
	}
}

func TestAuditBatchLogsScopesAccessBeforeSearchAndCount(t *testing.T) {
	s, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")
	seedConnectionAction(t, db, "db-auditor", rbac.ActionDBAuditView)
	now := time.Now().UTC()
	for _, sess := range []model.AuditSession{{ID: "ended", BatchID: "batch", Protocol: "mysql", State: "ended", StartedAt: now}, {ID: "online", BatchID: "batch", Protocol: "mysql", State: "started", StartedAt: now}, {ID: "odd", BatchID: "batch", Protocol: "mysql", State: "unknown", StartedAt: now}, {ID: "other-protocol", BatchID: "batch", Protocol: "ssh", State: "ended", StartedAt: now}} {
		if err := db.Create(&sess).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.AuditDBQuery{ID: "q-" + sess.ID, AuditSessionID: sess.ID, Timestamp: now, SQLText: "unique-" + sess.ID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	const base = "/api/audit/batches/db/batch/logs"
	_, p := batchTestRequest(t, s, base+"?q=unique-", "db-auditor", 200)
	if p.Total != 1 || len(p.Items) != 1 || p.Items[0].Source != "ended" {
		t.Fatalf("leaked session state or protocol: %+v", p)
	}
	_, p = batchTestRequest(t, s, base+"?q=unique-online", "db-auditor", 200)
	if p.Total != 0 {
		t.Fatalf("leaked online hit count: %+v", p)
	}
	_, p = batchTestRequest(t, s, base+"?q=unique-", "u-admin", 200)
	if p.Total != 2 {
		t.Fatalf("authorized results=%+v", p)
	}
	_, p = batchTestRequest(t, s, "/api/audit/db?batch_id=batch", "db-auditor", 200)
	if p.Total != 1 {
		t.Fatalf("drill-down leaked session count: %+v", p)
	}
}

func TestAuditBatchLogsStablePageOrder(t *testing.T) {
	s, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")
	now := time.Now().UTC()
	if err := db.Create(&[]model.AuditSession{{ID: "a", BatchID: "batch", Protocol: "ssh", State: "ended", StartedAt: now}, {ID: "b", BatchID: "batch", Protocol: "ssh", State: "ended", StartedAt: now}}).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{&model.AuditSSHCommand{ID: "same", AuditSessionID: "a", Timestamp: now}, &model.AuditSSHCommand{ID: "two", AuditSessionID: "b", Timestamp: now}, &model.AuditSFTPEvent{ID: "same", AuditSessionID: "b", Timestamp: now}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for i := 1; i <= 3; i++ {
		_, p := batchTestRequest(t, s, fmt.Sprintf("/api/audit/batches/ssh/batch/logs?page_size=1&page=%d", i), "u-admin", 200)
		if p.Total != 3 || len(p.Items) != 1 {
			t.Fatalf("weird pagination: %+v", p)
		}
		got = append(got, p.Items[0].Kind+":"+p.Items[0].ID)
	}
	if !slices.Equal(got, []string{"command:same", "command:two", "file:same"}) {
		t.Fatalf("unstable ties: %v", got)
	}
}

func TestAuditBatchHTTP(t *testing.T) {
	s, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")
	seedConnectionAction(t, db, "db-auditor", rbac.ActionDBAuditView)
	if err := db.Create(&model.AuditSession{ID: "only-ssh", BatchID: "ssh-batch", Protocol: "ssh", StartedAt: time.Now(), State: "ended"}).Error; err != nil {
		t.Fatal(err)
	}
	batchTestRequest(t, s, "/api/audit/batches/db/ssh-batch/logs", "u-admin", 404)
	batchTestRequest(t, s, "/api/audit/batches/ssh/ssh-batch/logs", "db-auditor", 404)
	batchTestRequest(t, s, "/api/audit/batches/ssh", "db-auditor", 403)
	batchTestRequest(t, s, "/api/audit/batches/rdp", "u-admin", 404)
}

func TestAuditBatchDoesNotChangeSingleArtifactAuthorization(t *testing.T) {
	s, db := newAdminDBTestServer(t)
	seedConnectionAction(t, db, "db-auditor", rbac.ActionDBAuditView)
	if err := db.Create(&model.AuditSession{ID: "audit-source", BatchID: "batch-safe", Protocol: "mysql", State: "ended", StartedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AuditDBQuery{ID: "audit-query", AuditSessionID: "audit-source", Timestamp: time.Now(), SQLText: "SELECT 1", SQLLogBytes: 8}).Error; err != nil {
		t.Fatal(err)
	}
	w, _ := batchTestRequest(t, s, "/api/audit/batches/db/batch-safe/logs", "db-auditor", 200)
	if strings.Contains(w.Body.String(), `"has_full_log":true`) {
		t.Fatal("batch advertised unavailable full-artifact access")
	}
	r := asTestUser(httptest.NewRequest(http.MethodGet, "/api/audit/db/audit-source/queries/audit-query/detail", nil), "db-auditor", "db-auditor")
	out := httptest.NewRecorder()
	s.handleAuditArtifact(out, r)
	if out.Code != 404 {
		t.Fatalf("legacy full detail permission changed: %d", out.Code)
	}
}
