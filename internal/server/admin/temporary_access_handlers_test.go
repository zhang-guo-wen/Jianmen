package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

func TestTemporaryAuthorizationCreatesBoundedGrant(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	server.superAdminIDs["u-admin"] = true
	if err := db.Create(&model.User{ID: "recipient", Username: "recipient", Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Host{ID: "host-1", Name: "build-host", Address: "127.0.0.1", Port: 22, Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.HostAccount{ID: "account-1", HostID: "host-1", Name: "deploy", Username: "deploy", Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}

	expiresAt := time.Now().UTC().Add(2 * time.Hour)
	body, _ := json.Marshal(map[string]any{
		"authorized_user_id": "recipient", "resource_type": model.ResourceTypeHostAccount,
		"resource_id": "account-1", "expires_at": expiresAt, "remark": "incident response",
	})
	req := asTestSuperAdmin(httptest.NewRequest(http.MethodPost, "/api/temporary-accounts", bytes.NewReader(body)))
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
	allowed, err := rbac.NewResourceGrantChecker(db).HasGrant("recipient", model.ResourceTypeHostAccount, "account-1")
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
