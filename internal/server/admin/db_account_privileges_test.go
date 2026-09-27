package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

func TestHandleDBAccountPrivilegesMethodNotAllowed(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	_, account := seedDatabasePrivilegesFixture(t, db, "db-priv-user")
	seedGlobalAction(t, db, "db-priv-user", rbac.ActionDBConnect)
	seedResourceGrant(t, db, "db-priv-user", model.ResourceTypeDatabaseAccount, account.ID)

	request := asTestUser(
		httptest.NewRequest(http.MethodPost, "/api/db/accounts/"+account.ID+"/privileges", nil),
		"db-priv-user",
		"operator",
	)
	recorder := httptest.NewRecorder()
	server.handleDBAccount(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleDBAccountPrivilegesRejectsUnknownChild(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	_, account := seedDatabasePrivilegesFixture(t, db, "db-priv-user")
	seedGlobalAction(t, db, "db-priv-user", rbac.ActionDBConnect)
	seedResourceGrant(t, db, "db-priv-user", model.ResourceTypeDatabaseAccount, account.ID)

	request := asTestUser(
		httptest.NewRequest(http.MethodGet, "/api/db/accounts/"+account.ID+"/unknown-child", nil),
		"db-priv-user",
		"operator",
	)
	recorder := httptest.NewRecorder()
	server.handleDBAccount(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleDBAccountPrivilegesRequiresConnectPermission(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	_, account := seedDatabasePrivilegesFixture(t, db, "db-priv-user")
	// No ActionDBConnect grant: inspecting privileges must be denied.
	seedGlobalAction(t, db, "db-priv-user", rbac.ActionDBProxyView)

	request := asTestUser(
		httptest.NewRequest(http.MethodGet, "/api/db/accounts/"+account.ID+"/privileges", nil),
		"db-priv-user",
		"operator",
	)
	recorder := httptest.NewRecorder()
	server.handleDBAccount(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "admin-secret") {
		t.Fatalf("forbidden response leaked credential: %s", recorder.Body.String())
	}
}

func TestHandleDBAccountPrivilegesUpstreamFailureDoesNotExposeDetails(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	_, account := seedDatabasePrivilegesFixture(t, db, "db-priv-user")
	seedGlobalAction(t, db, "db-priv-user", rbac.ActionDBConnect)
	seedResourceGrant(t, db, "db-priv-user", model.ResourceTypeDatabaseAccount, account.ID)

	request := asTestUser(
		httptest.NewRequest(http.MethodGet, "/api/db/accounts/"+account.ID+"/privileges", nil),
		"db-priv-user",
		"operator",
	)
	recorder := httptest.NewRecorder()
	server.handleDBAccount(recorder, request)
	// The fixture instance points at a closed port (127.0.0.1:1), so the
	// upstream dial fails immediately and is mapped to a safe 502.
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "数据库账号权限查询失败") {
		t.Fatalf("response body = %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "admin-secret") ||
		strings.Contains(recorder.Body.String(), "connect database account upstream") {
		t.Fatalf("upstream failure leaked details: %s", recorder.Body.String())
	}
}

func seedDatabasePrivilegesFixture(
	t *testing.T,
	db *gorm.DB,
	userID string,
) (model.DatabaseInstance, model.DatabaseAccount) {
	t.Helper()
	if err := db.Create(&model.User{ID: userID, Username: userID, Status: "active"}).Error; err != nil {
		t.Fatalf("create privileges user: %v", err)
	}
	instance := model.DatabaseInstance{
		Name: "orders", Protocol: "mysql", Address: "127.0.0.1", Port: 1, Status: "active",
	}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create database instance: %v", err)
	}
	account := model.DatabaseAccount{
		InstanceID: instance.ID,
		UniqueName: "app-" + userID,
		Username:   "app",
		Password:   model.NewEncryptedField("admin-secret"),
		Status:     "active",
		ResourceID: "D001",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create database account: %v", err)
	}
	return instance, account
}
