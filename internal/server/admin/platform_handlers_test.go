package admin

import (
	"bytes"
	"errors"
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

func TestPlatformAccountPasswordRevealIsAudited(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")
	if err := db.Create(&model.PlatformAccount{ID: "platform-1", Name: "Git", PlatformName: "Git", Username: "alice", OwnerID: "u-admin", Password: model.NewEncryptedField("platform-secret"), Status: "active"}).Error; err != nil {
		t.Fatalf("create platform account: %v", err)
	}

	recorder := httptest.NewRecorder()
	server.handlePlatformAccount(recorder, asTestSuperAdmin(httptest.NewRequest(http.MethodGet, "/api/platform-accounts/platform-1/password", nil)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("password status = %d, body=%s", recorder.Code, recorder.Body.String())
	}

	var events []model.AuditEvent
	if err := db.Where("operation = ?", operationRevealPlatformPassword).Find(&events).Error; err != nil {
		t.Fatalf("load audit events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("reveal audit rows = %d, want 1", len(events))
	}
	event := events[0]
	if event.Result != "success" || event.Action != "read" || event.StatusCode != http.StatusOK {
		t.Fatalf("reveal audit event = %#v", event)
	}
	if event.TargetLabel != "Git · alice" {
		t.Fatalf("reveal target label = %q", event.TargetLabel)
	}
	if strings.Contains(event.Detail, "platform-secret") {
		t.Fatalf("reveal audit leaked password: %s", event.Detail)
	}
}

func TestPlatformAccountPasswordRevealAuditsFailures(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")

	recorder := httptest.NewRecorder()
	server.handlePlatformAccount(recorder, asTestSuperAdmin(httptest.NewRequest(http.MethodGet, "/api/platform-accounts/missing/password", nil)))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing account status = %d", recorder.Code)
	}

	var event model.AuditEvent
	if err := db.Where("operation = ?", operationRevealPlatformPassword).First(&event).Error; err != nil {
		t.Fatalf("load reveal audit event: %v", err)
	}
	if event.Result != "failure" || event.StatusCode != http.StatusNotFound {
		t.Fatalf("failed reveal audit event = %#v", event)
	}
	if event.TargetLabel != "missing" {
		t.Fatalf("failed reveal label = %q", event.TargetLabel)
	}
}

// 审计写不进去时不能把密码返回给客户端。
func TestPlatformAccountPasswordRevealFailsClosedWithoutAudit(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")
	if err := db.Create(&model.PlatformAccount{ID: "platform-1", Name: "Git", PlatformName: "Git", Username: "alice", OwnerID: "u-admin", Password: model.NewEncryptedField("platform-secret"), Status: "active"}).Error; err != nil {
		t.Fatalf("create platform account: %v", err)
	}
	server.audit = &operationAuditTestRepository{createErr: errors.New("injected audit failure")}

	recorder := httptest.NewRecorder()
	server.handlePlatformAccount(recorder, asTestSuperAdmin(httptest.NewRequest(http.MethodGet, "/api/platform-accounts/platform-1/password", nil)))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "platform-secret") {
		t.Fatalf("secret leaked without audit: %s", recorder.Body.String())
	}
}
