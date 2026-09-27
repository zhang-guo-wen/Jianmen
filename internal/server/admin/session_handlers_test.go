package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type userSessionHandlerRepository struct{}

func (userSessionHandlerRepository) FindActiveHostAccount(context.Context, string) (model.HostAccount, bool, error) {
	return model.HostAccount{ID: "target-1", HostID: "host-1", ResourceID: "H001"}, true, nil
}
func (userSessionHandlerRepository) FindActiveHost(context.Context, string) (model.Host, bool, error) {
	return model.Host{ID: "host-1", Status: "active"}, true, nil
}
func (userSessionHandlerRepository) FindActiveDatabaseAccount(context.Context, string) (model.DatabaseAccount, bool, error) {
	return model.DatabaseAccount{}, false, nil
}
func (userSessionHandlerRepository) GetOrCreateActivePermanentUserSession(context.Context, string) (model.UserSession, error) {
	return model.UserSession{ID: "session-1", SessionID: "00001", SessionSeq: 1, Type: "permanent", Status: "active"}, nil
}

func (userSessionHandlerRepository) FindUserSessionBySessionID(context.Context, string) (model.UserSession, error) {
	return model.UserSession{}, nil
}

type userSessionHandlerAuthorizer struct{ allowed bool }

func (a userSessionHandlerAuthorizer) AuthorizeConnection(context.Context, string, []string, string, string) (bool, error) {
	return a.allowed, nil
}

func TestHandleUserSessionsUsesCreationService(t *testing.T) {
	creation, err := service.NewUserSessionCreationService(userSessionHandlerRepository{}, userSessionHandlerAuthorizer{allowed: true})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	server := &Server{userSessionCreation: creation}
	request := httptest.NewRequest(http.MethodPost, "/api/user-sessions", bytes.NewBufferString(`{"target_id":"target-1"}`))
	request = request.WithContext(context.WithValue(request.Context(), ctxKeyUserID, "user-1"))
	recorder := httptest.NewRecorder()
	server.handleUserSessions(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Data struct {
			ResourceID      string `json:"resource_id"`
			ResourceType    string `json:"resource_type"`
			CompactUsername string `json:"compact_username"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.ResourceID != "H001" || body.Data.ResourceType != model.ResourceTypeHostAccount || body.Data.CompactUsername != "HH00100001" {
		t.Fatalf("response data = %#v", body.Data)
	}
}

func TestHandleUserSessionsRejectsInvalidRequestBeforeService(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/api/user-sessions", bytes.NewBufferString(`{`))
	request = request.WithContext(context.WithValue(request.Context(), ctxKeyUserID, "user-1"))
	recorder := httptest.NewRecorder()
	server.handleUserSessions(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestHandleUserSessionsFailsClosedWhenConnectionIsDenied(t *testing.T) {
	creation, err := service.NewUserSessionCreationService(userSessionHandlerRepository{}, userSessionHandlerAuthorizer{allowed: false})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	server := &Server{userSessionCreation: creation}
	request := httptest.NewRequest(http.MethodPost, "/api/user-sessions", bytes.NewBufferString(`{"target_id":"target-1"}`))
	request = request.WithContext(context.WithValue(request.Context(), ctxKeyUserID, "user-1"))
	recorder := httptest.NewRecorder()
	server.handleUserSessions(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestHandleUserSessionBySessionID_Success(t *testing.T) {
	srv, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")

	user := model.User{ID: "u1", Username: "testuser", Status: "active"}
	require.NoError(t, db.Create(&user).Error)
	sess := model.UserSession{
		ID: "us1", UserID: "u1", SessionSeq: 1, SessionID: "00001",
		Type: "permanent", Status: "active",
	}
	require.NoError(t, db.Create(&sess).Error)

	req := httptest.NewRequest(http.MethodGet, "/api/user-sessions/by-session-id/00001", nil)
	req = asTestSuperAdmin(req)
	rec := httptest.NewRecorder()
	srv.handleUserSessionBySessionID(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var detail model.UserSessionAuthDetail
	require.NoError(t, decodeTestData(t, rec.Body.Bytes(), &detail))
	assert.Equal(t, "00001", detail.SessionID)
	assert.Equal(t, "normal", detail.AuthorizationType)
}

func TestHandleUserSessionBySessionID_NotFound(t *testing.T) {
	srv, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")

	req := httptest.NewRequest(http.MethodGet, "/api/user-sessions/by-session-id/99999", nil)
	req = asTestSuperAdmin(req)
	rec := httptest.NewRecorder()
	srv.handleUserSessionBySessionID(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleUserSessionBySessionID_InvalidFormat(t *testing.T) {
	srv, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")

	req := httptest.NewRequest(http.MethodGet, "/api/user-sessions/by-session-id/too/long/path", nil)
	req = asTestSuperAdmin(req)
	rec := httptest.NewRecorder()
	srv.handleUserSessionBySessionID(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleUserSessionBySessionID_SessionIDTooLong(t *testing.T) {
	srv, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")

	req := httptest.NewRequest(http.MethodGet, "/api/user-sessions/by-session-id/123456", nil)
	req = asTestSuperAdmin(req)
	rec := httptest.NewRecorder()
	srv.handleUserSessionBySessionID(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleUserSessionBySessionID_Forbidden(t *testing.T) {
	srv, db := newAdminDBTestServer(t)
	user := model.User{ID: "regular-user", Username: "regular", Status: "active"}
	require.NoError(t, db.Create(&user).Error)

	req := httptest.NewRequest(http.MethodGet, "/api/user-sessions/by-session-id/00001", nil)
	req = asTestUser(req, "regular-user", "regular")
	rec := httptest.NewRecorder()
	srv.handleUserSessionBySessionID(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}
