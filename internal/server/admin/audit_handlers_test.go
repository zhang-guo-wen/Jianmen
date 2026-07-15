package admin

import (
	"net/http"
	"net/http/httptest"
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
		{ID: "audit-alpha-2", UserID: "u2", Username: "bob", Protocol: "ssh", TargetName: "alpha-db", StartedAt: now.Add(-time.Minute), State: "ended"},
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
	if page.Items[0].TargetName != "alpha-db" {
		t.Fatalf("unexpected audit item: %#v", page.Items[0])
	}
}
