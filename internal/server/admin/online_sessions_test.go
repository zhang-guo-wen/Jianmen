package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/online"
)

func TestHandleOnlineSessionsFiltersAndDisconnects(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")
	server.onlineSessions = online.NewRegistry()
	disconnected := false
	server.onlineSessions.Register(online.Session{
		ID:           "ssh-live",
		ResourceType: model.ResourceTypeHost,
		ResourceID:   "host-1",
		Instance:     "host-a",
		Protocol:     "ssh",
		Account:      "root",
		Operator:     "alice",
		StartedAt:    time.Now().UTC(),
	}, func() {
		disconnected = true
	})
	server.onlineSessions.Register(online.Session{
		ID:           "db-live",
		ResourceType: model.ResourceTypeDatabaseInstance,
		ResourceID:   "db-1",
		Instance:     "database-a",
		Protocol:     "mysql",
		Account:      "app",
		Operator:     "bob",
		StartedAt:    time.Now().UTC().Add(-time.Minute),
	}, func() {})

	req := asTestSuperAdmin(httptest.NewRequest(http.MethodGet, "/api/online-sessions?resource_type=host&resource_id=host-1&q=ROOT", nil))
	rec := httptest.NewRecorder()
	server.handleOnlineSessions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var page struct {
		Items []online.Session `json:"items"`
		Total int              `json:"total"`
	}
	if err := decodeTestData(t, rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode online sessions: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "ssh-live" {
		t.Fatalf("unexpected online sessions: %#v", page)
	}

	req = asTestSuperAdmin(httptest.NewRequest(http.MethodDelete, "/api/online-sessions/ssh-live", nil))
	rec = httptest.NewRecorder()
	server.handleOnlineSession(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("disconnect status = %d, want %d; body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if !disconnected {
		t.Fatal("disconnect callback was not called")
	}
	if got := server.onlineSessions.List(); len(got) != 1 || got[0].ID != "db-live" {
		t.Fatalf("online sessions after disconnect: %#v", got)
	}
}
