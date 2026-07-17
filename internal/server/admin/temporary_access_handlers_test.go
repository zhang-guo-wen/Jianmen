package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

func TestTemporaryAuthorizationCreatesBoundedGrant(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	server.superAdminIDs["u-admin"] = true
	server.cfg.ListenAddr = "0.0.0.0:47102"
	if err := db.Create(&model.User{ID: "u-admin", Username: "admin", Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Host{ID: "host-1", Name: "build-host", Address: "127.0.0.1", Port: 22, Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.HostAccount{ID: "account-1", HostID: "host-1", Name: "deploy", Username: "deploy", Status: "active", ResourceID: "A001"}).Error; err != nil {
		t.Fatal(err)
	}

	expiresAt := time.Now().UTC().Add(2 * time.Hour)
	body, _ := json.Marshal(map[string]any{
		"resource_type": model.ResourceTypeHostAccount,
		"resource_id":   "account-1", "expires_at": expiresAt, "remark": "incident response",
	})
	req := asTestSuperAdmin(httptest.NewRequest(http.MethodPost, "/api/temporary-accounts", bytes.NewReader(body)))
	req.Header.Set("Origin", "https://bastion.example.test")
	rec := httptest.NewRecorder()
	server.handleTemporaryAccounts(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var result temporaryAccountView
	if err := decodeTestData(t, rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.SessionID == "" || result.Type != model.TemporaryAccountTypeUser {
		t.Fatalf("unexpected temporary account: %#v", result)
	}
	if result.AuthorizedUserID != "u-admin" || result.Connection == nil {
		t.Fatalf("temporary authorization did not default to current user or issue credentials: %#v", result)
	}
	if result.Connection.Address != "bastion.example.test:47102" || result.Connection.Username != "HA001"+result.SessionID || result.Connection.Password == "" {
		t.Fatalf("unexpected connection info: %#v", result.Connection)
	}
	if err := server.store.AuthenticateConnectionPassword(req.Context(), "u-admin", model.ResourceTypeHostAccount, "account-1", result.Connection.Password); err != nil {
		t.Fatalf("issued temporary password does not authenticate: %v", err)
	}
	allowed, err := rbac.NewResourceGrantChecker(db).HasGrant("u-admin", model.ResourceTypeHostAccount, "account-1")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Fatal("temporary grant should allow the recipient to connect")
	}
}

func TestTemporaryAuthorizationRejectsMoreThanSevenDays(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	server.superAdminIDs["u-admin"] = true
	_ = db.Create(&model.User{ID: "recipient", Username: "recipient", Status: "active"}).Error
	_ = db.Create(&model.Host{ID: "host-1", Name: "build-host", Address: "127.0.0.1", Port: 22, Status: "active"}).Error
	_ = db.Create(&model.HostAccount{ID: "account-1", HostID: "host-1", Name: "deploy", Username: "deploy", Status: "active"}).Error

	body, _ := json.Marshal(map[string]any{
		"authorized_user_id": "recipient", "resource_type": model.ResourceTypeHostAccount,
		"resource_id": "account-1", "expires_at": time.Now().UTC().Add(8 * 24 * time.Hour),
	})
	req := asTestSuperAdmin(httptest.NewRequest(http.MethodPost, "/api/temporary-accounts", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	server.handleTemporaryAccounts(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "\u4e34\u65f6\u6388\u6743\u6709\u6548\u671f\u4e0d\u80fd\u8d85\u8fc7 7 \u5929") {
		t.Fatalf("response should explain the seven-day limit: %s", rec.Body.String())
	}
}

func TestTemporaryAuthorizationExtensionRejectsMoreThanSevenDays(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	server.superAdminIDs["u-admin"] = true
	now := time.Now().UTC()
	if err := db.Create(&model.TemporaryAccount{
		ID: "temporary-1", SessionID: "session-1", Type: model.TemporaryAccountTypeUser,
		Username: "tmp_session-1", Status: "active", StartsAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"expires_at": now.Add(8 * 24 * time.Hour),
	})
	req := asTestSuperAdmin(httptest.NewRequest(http.MethodPost, "/api/temporary-accounts/temporary-1/extend", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	server.extendTemporaryAccount(rec, req, "temporary-1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "\u4e34\u65f6\u6388\u6743\u6709\u6548\u671f\u4e0d\u80fd\u8d85\u8fc7 7 \u5929") {
		t.Fatalf("response should explain the seven-day limit: %s", rec.Body.String())
	}
}

func TestCreateUserDefaultsToOneYearExpiry(t *testing.T) {
	server, _ := newAdminDBTestServer(t)
	server.superAdminIDs["u-admin"] = true
	before := time.Now().UTC().AddDate(1, 0, 0)
	body := bytes.NewBufferString(`{"username":"new-user","password":"password-123"}`)
	req := asTestSuperAdmin(httptest.NewRequest(http.MethodPost, "/api/users", body))
	rec := httptest.NewRecorder()
	server.handleUsers(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var result struct {
		User model.User `json:"user"`
	}
	if err := decodeTestData(t, rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.User.ExpiresAt == nil {
		t.Fatal("new regular user should have an expiry")
	}
	if result.User.ExpiresAt.Before(before.Add(-time.Minute)) || result.User.ExpiresAt.After(before.Add(time.Minute)) {
		t.Fatalf("expiry = %v, want about one year from now", result.User.ExpiresAt)
	}
}
