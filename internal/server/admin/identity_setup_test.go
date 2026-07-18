package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jianmen/internal/model"
)

func TestInitSetupPersistsExactlyOneSuperAdministrator(t *testing.T) {
	server, db := newAdminDBTestServer(t)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/init/setup",
		strings.NewReader(`{"username":"admin","password":"secure-password","email":"admin@example.com"}`),
	)
	response := httptest.NewRecorder()
	server.handleInitSetup(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("setup status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}

	var users []model.User
	if err := db.Find(&users).Error; err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("user count = %d, want 1", len(users))
	}
	if !users[0].IsSuperAdmin {
		t.Fatal("setup user is not persisted as super administrator")
	}

	secondRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/init/setup",
		strings.NewReader(`{"username":"other","password":"secure-password"}`),
	)
	secondResponse := httptest.NewRecorder()
	server.handleInitSetup(secondResponse, secondRequest)
	if secondResponse.Code != http.StatusForbidden {
		t.Fatalf("second setup status = %d, want %d; body=%s", secondResponse.Code, http.StatusForbidden, secondResponse.Body.String())
	}

	var superAdminCount int64
	if err := db.Model(&model.User{}).Where("is_super_admin = ?", true).Count(&superAdminCount).Error; err != nil {
		t.Fatalf("count super administrators: %v", err)
	}
	if superAdminCount != 1 {
		t.Fatalf("super administrator count = %d, want 1", superAdminCount)
	}
}
