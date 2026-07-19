package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

func TestContainerRuntimeRequiresEndpointGrant(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	userID := "container-connect"
	endpointID := "endpoint-no-grant"
	seedContainerAction(t, db, userID, rbac.ActionContainerConnect, model.PermissionEffectAllow, "", "")
	createContainerEndpoint(t, db, endpointID)

	req := httptest.NewRequest(http.MethodGet, "/api/containers/endpoints/"+endpointID+"/containers", nil)
	req = withTestUser(req, userID, userID)
	rec := httptest.NewRecorder()
	server.handleContainerEndpoint(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("without endpoint grant status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer docker.Close()
	if err := db.Model(&model.ContainerEndpoint{}).Where("id = ?", endpointID).Update("address", docker.URL).Error; err != nil {
		t.Fatalf("update Docker endpoint address: %v", err)
	}
	if err := db.Create(&model.ResourceGrant{
		ID: "grant-container-connect", PrincipalType: "user", PrincipalID: userID,
		ResourceType: model.ResourceTypeContainerEndpoint, ResourceID: endpointID, Effect: model.PermissionEffectAllow,
	}).Error; err != nil {
		t.Fatalf("create endpoint grant: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/containers/endpoints/"+endpointID+"/containers", nil)
	req = withTestUser(req, userID, userID)
	rec = httptest.NewRecorder()
	server.handleContainerEndpoint(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("with endpoint grant status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestContainerRuntimeRejectsExplicitResourceDeny(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	endpointID := "endpoint-denied"
	seedContainerAction(t, db, "container-denied", rbac.ActionContainerConnect, model.PermissionEffectAllow, "", "")
	seedContainerAction(t, db, "container-denied", rbac.ActionContainerConnect, model.PermissionEffectDeny, model.ResourceTypeContainerEndpoint, endpointID)
	createContainerEndpoint(t, db, endpointID)
	if err := db.Create(&model.ResourceGrant{
		ID: "grant-container-denied", PrincipalType: "user", PrincipalID: "container-denied",
		ResourceType: model.ResourceTypeContainerEndpoint, ResourceID: endpointID, Effect: model.PermissionEffectAllow,
	}).Error; err != nil {
		t.Fatalf("create endpoint grant: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/containers/endpoints/"+endpointID+"/containers", nil)
	req = withTestUser(req, "container-denied", "container-denied")
	rec := httptest.NewRecorder()
	server.handleContainerEndpoint(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("resource deny status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestContainerEndpointListOnlyReturnsAuthorizedEndpoints(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	userID := "container-list"
	seedContainerAction(t, db, userID, rbac.ActionContainerView, model.PermissionEffectAllow, "", "")
	createContainerEndpoint(t, db, "endpoint-visible")
	createContainerEndpoint(t, db, "endpoint-hidden")
	if err := db.Create(&model.ResourceGrant{
		ID: "grant-container-visible", PrincipalType: "user", PrincipalID: userID,
		ResourceType: model.ResourceTypeContainerEndpoint, ResourceID: "endpoint-visible", Effect: model.PermissionEffectAllow,
	}).Error; err != nil {
		t.Fatalf("create endpoint grant: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/containers/endpoints", nil)
	req = withTestUser(req, userID, userID)
	rec := httptest.NewRecorder()
	server.handleContainerEndpoints(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var response struct {
		Data struct {
			Items []store.ContainerEndpointView `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(response.Data.Items) != 1 || response.Data.Items[0].ID != "endpoint-visible" {
		t.Fatalf("authorized endpoints = %#v, want only visible endpoint", response.Data.Items)
	}
}

func TestContainerEndpointLogsRequireEndpointGrant(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedContainerAction(t, db, "container-logs", rbac.ActionContainerConnect, model.PermissionEffectAllow, "", "")
	createContainerEndpoint(t, db, "endpoint-logs")

	req := httptest.NewRequest(http.MethodGet, "/api/containers/endpoints/endpoint-logs/containers/abc/logs", nil)
	req = withTestUser(req, "container-logs", "container-logs")
	rec := httptest.NewRecorder()
	server.handleContainerEndpoint(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("logs without endpoint grant status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestContainerEndpointCRUDRequiresMatchingResourceAction(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		action     string
		body       string
		wantStatus int
	}{
		{name: "view", method: http.MethodGet, action: rbac.ActionContainerView, wantStatus: http.StatusForbidden},
		{name: "update", method: http.MethodPut, action: rbac.ActionContainerUpdate, body: `{"name":"updated","runtime":"docker","connection_mode":"docker_api","address":"http://127.0.0.1:2375"}`, wantStatus: http.StatusForbidden},
		{name: "delete", method: http.MethodDelete, action: rbac.ActionContainerDelete, wantStatus: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, db := newAdminDBTestServer(t)
			userID := "container-" + tt.name
			seedContainerAction(t, db, userID, tt.action, model.PermissionEffectAllow, "", "")
			createContainerEndpoint(t, db, "endpoint-"+tt.name)

			req := httptest.NewRequest(tt.method, "/api/containers/endpoints/endpoint-"+tt.name, bytes.NewBufferString(tt.body))
			req = withTestUser(req, userID, userID)
			rec := httptest.NewRecorder()
			server.handleContainerEndpoint(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("%s without endpoint grant status = %d, want %d; body=%s", tt.name, rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestContainerEndpointCRUDAllowsMatchingResourceAction(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		action     string
		body       string
		wantStatus int
	}{
		{name: "view-allowed", method: http.MethodGet, action: rbac.ActionContainerView, wantStatus: http.StatusOK},
		{name: "update-allowed", method: http.MethodPut, action: rbac.ActionContainerUpdate, body: `{"name":"updated","runtime":"docker","connection_mode":"docker_api","address":"http://127.0.0.1:2375"}`, wantStatus: http.StatusOK},
		{name: "delete-allowed", method: http.MethodDelete, action: rbac.ActionContainerDelete, wantStatus: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, db := newAdminDBTestServer(t)
			userID := "container-" + tt.name
			endpointID := "endpoint-" + tt.name
			seedContainerAction(t, db, userID, tt.action, model.PermissionEffectAllow, "", "")
			createContainerEndpoint(t, db, endpointID)
			if err := db.Create(&model.ResourceGrant{
				ID: "grant-" + tt.name, PrincipalType: "user", PrincipalID: userID,
				ResourceType: model.ResourceTypeContainerEndpoint, ResourceID: endpointID, Effect: model.PermissionEffectAllow,
			}).Error; err != nil {
				t.Fatalf("create endpoint grant: %v", err)
			}

			req := httptest.NewRequest(tt.method, "/api/containers/endpoints/"+endpointID, bytes.NewBufferString(tt.body))
			req = withTestUser(req, userID, userID)
			rec := httptest.NewRecorder()
			server.handleContainerEndpoint(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("%s with endpoint grant status = %d, want %d; body=%s", tt.name, rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestContainerEndpointCreateRequiresAuthorizedHostAccount(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	userID := "container-create-account-auth"
	seedContainerAction(t, db, userID, rbac.ActionContainerCreate, model.PermissionEffectAllow, "", "")
	seedContainerHostAccount(t, db, "container-host-1", "container-account-1")

	req := httptest.NewRequest(http.MethodPost, "/api/containers/endpoints", bytes.NewBufferString(`{
		"name":"ssh-endpoint","runtime":"docker","connection_mode":"ssh",
		"host_id":"container-host-1","host_account_id":"container-account-1"
	}`))
	req = withTestUser(req, userID, userID)
	rec := httptest.NewRecorder()
	server.handleContainerEndpoints(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("create with unauthorized host account status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestContainerEndpointUpdateRequiresAuthorizedHostAccount(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	userID := "container-update-account-auth"
	endpointID := "container-update-account-endpoint"
	seedContainerAction(t, db, userID, rbac.ActionContainerUpdate, model.PermissionEffectAllow, "", "")
	seedContainerHostAccount(t, db, "container-host-1", "container-account-1")
	createContainerEndpoint(t, db, endpointID)
	if err := db.Create(&model.ResourceGrant{
		ID: "grant-" + endpointID, PrincipalType: "user", PrincipalID: userID,
		ResourceType: model.ResourceTypeContainerEndpoint, ResourceID: endpointID, Effect: model.PermissionEffectAllow,
	}).Error; err != nil {
		t.Fatalf("create endpoint grant: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/containers/endpoints/"+endpointID, bytes.NewBufferString(`{
		"name":"ssh-endpoint","runtime":"docker","connection_mode":"ssh",
		"host_id":"container-host-1","host_account_id":"container-account-1"
	}`))
	req = withTestUser(req, userID, userID)
	rec := httptest.NewRecorder()
	server.handleContainerEndpoint(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("update with unauthorized host account status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestContainerConnectionTestRequiresAuthorizedHostAccount(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	userID := "container-test-account-auth"
	seedContainerAction(t, db, userID, rbac.ActionContainerCreate, model.PermissionEffectAllow, "", "")
	seedContainerHostAccount(t, db, "container-host-1", "container-account-1")

	req := httptest.NewRequest(http.MethodPost, "/api/containers/test", bytes.NewBufferString(`{
		"name":"ssh-endpoint","runtime":"docker","connection_mode":"ssh",
		"host_id":"container-host-1","host_account_id":"container-account-1"
	}`))
	req = withTestUser(req, userID, userID)
	rec := httptest.NewRecorder()
	server.handleContainerConnectionTest(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("connection test with unauthorized host account status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestContainerEndpointRejectsAccountFromDifferentHost(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	userID := "container-host-binding"
	seedContainerAction(t, db, userID, rbac.ActionContainerCreate, model.PermissionEffectAllow, "", "")
	seedContainerHostAccount(t, db, "container-host-1", "container-account-1")
	if err := db.Create(&model.Host{
		ID: "container-host-2", Name: "container-host-2", Address: "127.0.0.1", Port: 22, Status: "active",
	}).Error; err != nil {
		t.Fatalf("create second container host: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/containers/endpoints", bytes.NewBufferString(`{
		"name":"ssh-endpoint","runtime":"docker","connection_mode":"ssh",
		"host_id":"container-host-2","host_account_id":"container-account-1"
	}`))
	req = withTestUser(req, userID, userID)
	rec := httptest.NewRecorder()
	server.handleContainerEndpoints(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("create with account from different host before account grant status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestContainerEndpointRejectsMissingHostAccount(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	userID := "container-missing-account"
	seedContainerAction(t, db, userID, rbac.ActionContainerCreate, model.PermissionEffectAllow, "", "")
	if err := db.Create(&model.Host{
		ID: "container-host-missing-account", Name: "container-host-missing-account", Address: "127.0.0.1", Port: 22, Status: "active",
	}).Error; err != nil {
		t.Fatalf("create container host: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/containers/endpoints", bytes.NewBufferString(`{
		"name":"ssh-endpoint","runtime":"docker","connection_mode":"ssh",
		"host_id":"container-host-missing-account","host_account_id":"does-not-exist"
	}`))
	req = withTestUser(req, userID, userID)
	rec := httptest.NewRecorder()
	server.handleContainerEndpoints(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("create with missing host account before account grant status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func seedContainerAction(t *testing.T, db *gorm.DB, userID, action, effect, resourceType, resourceID string) {
	t.Helper()
	var role model.Role
	if err := db.Where("id = ?", "role-"+userID).First(&role).Error; err != nil {
		role = model.Role{ID: "role-" + userID, Name: "role-" + userID, Status: "active"}
		if err := db.Create(&role).Error; err != nil {
			t.Fatalf("create container role: %v", err)
		}
	}
	if err := db.Where("id = ?", userID).First(&model.User{}).Error; err != nil {
		if err := db.Create(&model.User{ID: userID, Username: userID, Status: "active"}).Error; err != nil {
			t.Fatalf("create container user: %v", err)
		}
	}
	permission := model.Permission{ID: "perm-" + userID + "-" + effect + "-" + resourceID, Action: action, ResourceType: resourceType, ResourceID: resourceID, Effect: effect}
	if err := db.Create(&permission).Error; err != nil {
		t.Fatalf("create container permission: %v", err)
	}
	var membership model.UserRole
	if err := db.Where("user_id = ? AND role_id = ?", userID, role.ID).First(&membership).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		if err := db.Create(&model.UserRole{ID: "ur-" + userID, UserID: userID, RoleID: role.ID}).Error; err != nil {
			t.Fatalf("create container user role: %v", err)
		}
	} else if err != nil {
		t.Fatalf("find container user role: %v", err)
	}
	if err := db.Create(&model.RolePermission{ID: "rp-" + permission.ID, RoleID: role.ID, PermissionID: permission.ID}).Error; err != nil {
		t.Fatalf("create container role permission: %v", err)
	}
}

func createContainerEndpoint(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	if err := db.Create(&model.ContainerEndpoint{
		ID: id, Name: id, Runtime: model.ContainerRuntimeDocker,
		ConnectionMode: model.ContainerConnectionDockerAPI, Address: "http://127.0.0.1:2375", Status: "active",
	}).Error; err != nil {
		t.Fatalf("create container endpoint: %v", err)
	}
}

func seedContainerHostAccount(t *testing.T, db *gorm.DB, hostID, accountID string) {
	t.Helper()
	if err := db.Create(&model.Host{
		ID: hostID, Name: hostID, Address: "127.0.0.1", Port: 22, Status: "active",
	}).Error; err != nil {
		t.Fatalf("create container host: %v", err)
	}
	if err := db.Create(&model.HostAccount{
		ID: accountID, HostID: hostID, Name: accountID, Username: "root", Status: "active",
	}).Error; err != nil {
		t.Fatalf("create container host account: %v", err)
	}
}
