package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jianmen/internal/model"
)

func TestPlatformAccountHandlerHidesPasswordAndUsesService(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")
	if err := db.Create(&model.PlatformAccount{ID: "platform-1", Name: "Git", PlatformName: "Git", Username: "alice", OwnerID: "u-admin", Password: model.NewEncryptedField("platform-secret"), Status: "active"}).Error; err != nil {
		t.Fatalf("create platform account: %v", err)
	}

	recorder := httptest.NewRecorder()
	server.handlePlatformAccount(recorder, asTestSuperAdmin(httptest.NewRequest(http.MethodGet, "/api/platform-accounts/platform-1", nil)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "platform-secret") || strings.Contains(recorder.Body.String(), `"password":"`) {
		t.Fatalf("platform account response exposed password: %s", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"has_password":true`) {
		t.Fatalf("platform account response lost password marker: %s", recorder.Body.String())
	}

	update := httptest.NewRequest(http.MethodPut, "/api/platform-accounts/platform-1", bytes.NewBufferString(`{"name":"Git updated"}`))
	updateRecorder := httptest.NewRecorder()
	server.handlePlatformAccount(updateRecorder, asTestSuperAdmin(update))
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	passwordRecorder := httptest.NewRecorder()
	server.handlePlatformAccount(passwordRecorder, asTestSuperAdmin(httptest.NewRequest(http.MethodGet, "/api/platform-accounts/platform-1/password", nil)))
	if passwordRecorder.Code != http.StatusOK || !strings.Contains(passwordRecorder.Body.String(), "platform-secret") {
		t.Fatalf("password status = %d, body=%s", passwordRecorder.Code, passwordRecorder.Body.String())
	}
}
