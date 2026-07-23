package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/online"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestHandleOnlineSessions_IncludesUserSessionID(t *testing.T) {
	srv, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")

	// 创建 UserSession
	us := model.UserSession{
		ID: "us-online-1", UserID: "u-admin", SessionSeq: 10,
		SessionID: "0000A", Type: "permanent", Status: "active",
	}
	require.NoError(t, db.Create(&us).Error)

	// 创建 AuditSession
	as := model.AuditSession{
		ID: "audit-online-1", UserSessionID: "us-online-1",
		UserID: "u-admin", Username: "operator1", Protocol: "ssh",
		State: "started", Outcome: "active",
	}
	require.NoError(t, db.Create(&as).Error)

	// 注册在线会话
	srv.onlineSessions = online.NewRegistry()
	srv.onlineSessions.Register(online.Session{
		ID: "online-1", AuditSessionID: "audit-online-1",
		ResourceType: "host", Instance: "web01", Protocol: "ssh",
		Operator: "operator1", Account: "root", StartedAt: time.Now(),
	}, func() {})

	req := httptest.NewRequest(http.MethodGet, "/api/online-sessions", nil)
	req = asTestSuperAdmin(req)
	rec := httptest.NewRecorder()
	srv.handleOnlineSessions(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Items []online.Session `json:"items"`
		Total int              `json:"total"`
	}
	require.NoError(t, decodeTestData(t, rec.Body.Bytes(), &resp))
	require.Greater(t, len(resp.Items), 0)

	var found bool
	for _, sess := range resp.Items {
		if sess.ID == "online-1" {
			found = true
			assert.Equal(t, "us-online-1", sess.UserSessionID)
			assert.Equal(t, "0000A", sess.SessionID)
			break
		}
	}
	assert.True(t, found, "expected to find session online-1 in response")
}
