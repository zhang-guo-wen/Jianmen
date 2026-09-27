package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jianmen/internal/model"
)

func TestHandleCreateTargetRejectsInvalidRDPSecurityBeforeWrite(t *testing.T) {
	server := newTargetTestServer(t)
	request := asTestSuperAdmin(httptest.NewRequest(
		http.MethodPost,
		"/api/targets",
		bytes.NewBufferString(`{
			"id":"invalid-security-account",
			"protocol":"rdp",
			"host":"127.0.0.31",
			"port":3389,
			"username":"Administrator",
			"password":"secret",
			"rdp_security":"credssp-or-anything"
		}`),
	))
	response := httptest.NewRecorder()

	server.handleTargets(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "rdp_security must be one of") {
		t.Fatalf("response does not explain valid RDP security modes: %s", response.Body.String())
	}
	var accountCount int64
	if err := server.db.Model(&model.HostAccount{}).
		Where("id = ?", "invalid-security-account").
		Count(&accountCount).Error; err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if accountCount != 0 {
		t.Fatalf("invalid request wrote %d account rows, want 0", accountCount)
	}
	var hostCount int64
	if err := server.db.Model(&model.Host{}).
		Where("address = ?", "127.0.0.31").
		Count(&hostCount).Error; err != nil {
		t.Fatalf("count hosts: %v", err)
	}
	if hostCount != 0 {
		t.Fatalf("invalid request wrote %d host rows, want 0", hostCount)
	}
}

func TestHandleUpdateTargetRejectsInvalidRDPSecurity(t *testing.T) {
	server := newTargetTestServer(t)
	createRequest := asTestSuperAdmin(httptest.NewRequest(
		http.MethodPost,
		"/api/targets",
		bytes.NewBufferString(`{
			"id":"rdp-security-account",
			"host_id":"rdp-security-host",
			"protocol":"rdp",
			"host":"127.0.0.32",
			"port":3389,
			"username":"Administrator",
			"password":"secret",
			"rdp_security":"  NLA  "
		}`),
	))
	createResponse := httptest.NewRecorder()
	server.handleTargets(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body=%s", createResponse.Code, http.StatusCreated, createResponse.Body.String())
	}

	updateRequest := asTestSuperAdmin(httptest.NewRequest(
		http.MethodPut,
		"/api/targets/rdp-security-account",
		bytes.NewBufferString(`{
			"username":"Administrator",
			"rdp_security":"automatic-but-unsafe"
		}`),
	))
	updateResponse := httptest.NewRecorder()
	server.handleTarget(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusBadRequest {
		t.Fatalf("update status = %d, want %d; body=%s", updateResponse.Code, http.StatusBadRequest, updateResponse.Body.String())
	}
	if !strings.Contains(updateResponse.Body.String(), "rdp_security must be one of") {
		t.Fatalf("update response does not explain valid RDP security modes: %s", updateResponse.Body.String())
	}

	stored, err := server.hostTargets.Target(context.Background(), "rdp-security-account")
	if err != nil {
		t.Fatalf("load stored target: %v", err)
	}
	if stored.RDPSecurity != "nla" {
		t.Fatalf("stored security = %q, want unchanged nla", stored.RDPSecurity)
	}
}

func TestHandleCreateTargetRejectsFileTransferWithoutDriveMapping(t *testing.T) {
	server := newTargetTestServer(t)
	request := asTestSuperAdmin(httptest.NewRequest(
		http.MethodPost,
		"/api/targets",
		bytes.NewBufferString(`{
			"id":"invalid-rdp-file-policy",
			"protocol":"rdp",
			"host":"127.0.0.33",
			"port":3389,
			"username":"Administrator",
			"password":"secret",
			"rdp_file_upload":true,
			"rdp_drive_mapping":false
		}`),
	))
	response := httptest.NewRecorder()

	server.handleTargets(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf(
			"status = %d, want %d; body=%s",
			response.Code,
			http.StatusBadRequest,
			response.Body.String(),
		)
	}
	if !strings.Contains(response.Body.String(), "file transfer requires drive mapping") {
		t.Fatalf("response does not explain file-transfer prerequisite: %s", response.Body.String())
	}
	var count int64
	if err := server.db.Model(&model.HostAccount{}).
		Where("id = ?", "invalid-rdp-file-policy").
		Count(&count).Error; err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if count != 0 {
		t.Fatalf("invalid request wrote %d account rows, want 0", count)
	}
	var hostCount int64
	if err := server.db.Model(&model.Host{}).
		Where("address = ?", "127.0.0.33").
		Count(&hostCount).Error; err != nil {
		t.Fatalf("count hosts: %v", err)
	}
	if hostCount != 0 {
		t.Fatalf("invalid request wrote %d host rows, want 0", hostCount)
	}
}
