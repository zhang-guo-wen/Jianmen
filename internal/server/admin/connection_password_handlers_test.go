package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

func TestHandleConnectionPasswordsRequiresResourceAuthorization(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedConnectionAction(t, db, "connection-user", rbac.ActionSessionConnect)
	host := model.Host{ID: "host-password", Name: "host", Address: "127.0.0.1", Port: 22, Status: "active"}
	account := model.HostAccount{ID: "account-password", HostID: host.ID, ResourceID: "cp01", Username: "root", Status: "active"}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/connection-passwords", strings.NewReader(`{"target_id":"account-password"}`))
	request = withTestUser(request, "connection-user", "connection-user")
	response := httptest.NewRecorder()
	server.handleConnectionPasswords(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("without grant status = %d, want %d; body=%s", response.Code, http.StatusForbidden, response.Body.String())
	}

	grant := model.ResourceGrant{PrincipalType: "user", PrincipalID: "connection-user", ResourceType: model.ResourceTypeHostAccount, ResourceID: account.ID, Effect: model.PermissionEffectAllow}
	if err := db.Create(&grant).Error; err != nil {
		t.Fatalf("create grant: %v", err)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/connection-passwords", strings.NewReader(`{"target_id":"account-password"}`))
	request = withTestUser(request, "connection-user", "connection-user")
	response = httptest.NewRecorder()
	server.handleConnectionPasswords(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("with grant status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	var envelope struct {
		Data struct {
			Password         string `json:"password"`
			ExpiresInSeconds int    `json:"expires_in_seconds"`
			Reusable         bool   `json:"reusable"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Password == "" || envelope.Data.ExpiresInSeconds != 1800 || !envelope.Data.Reusable {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
	var saved model.ConnectionPassword
	if err := db.First(&saved, "user_id = ? AND resource_id = ?", "connection-user", account.ID).Error; err != nil {
		t.Fatalf("load saved credential: %v", err)
	}
	if saved.SecretHash == envelope.Data.Password || strings.Contains(response.Body.String(), saved.SecretHash) {
		t.Fatal("response or database exposed the wrong password representation")
	}
}
