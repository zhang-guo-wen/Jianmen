package admin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/rbac"

	"gorm.io/gorm"
)

func TestRBACCatalogReturnsCompleteCatalog(t *testing.T) {
	server, _ := newRBACServer(t)

	rec := requestRBAC(t, server.handleRBACCatalog, http.MethodGet, "/api/rbac/catalog", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response rbacCatalogResponse
	decodeRBACResponse(t, rec, &response)
	if !reflect.DeepEqual(response.Pages, rbac.PermissionPages()) {
		t.Fatalf("catalog response does not match permission pages")
	}
}

func TestRBACCatalogRequiresManagePermission(t *testing.T) {
	server, db := newRBACServer(t)
	if err := db.Create(&model.User{ID: "regular", Username: "regular", Status: "active"}).Error; err != nil {
		t.Fatalf("create regular user: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/rbac/catalog", nil)
	req = withTestUser(req, "regular", "regular")
	rec := httptest.NewRecorder()

	server.handleRBACCatalog(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestRBACReplaceRoleActionsPreservesResourceAndDenyPermissions(t *testing.T) {
	server, db := newRBACServer(t)
	role := createRBACRole(t, server, `{"name":"operators"}`)
	otherRole := createRBACRole(t, server, `{"name":"auditors"}`)

	oldAction := createBoundPermission(t, db, role.ID, model.Permission{
		Action: rbac.ActionSessionConnect, Effect: model.PermissionEffectAllow,
	})
	resourceAllow := createBoundPermission(t, db, role.ID, model.Permission{
		Action: rbac.ActionSessionConnect, ResourceType: model.ResourceTypeHostAccount,
		ResourceID: "account-1", Effect: model.PermissionEffectAllow,
	})
	deny := createBoundPermission(t, db, role.ID, model.Permission{
		Action: rbac.ActionAppView, Effect: model.PermissionEffectDeny,
	})
	otherAction := oldAction
	if err := db.Create(&model.RolePermission{RoleID: otherRole.ID, PermissionID: otherAction.ID}).Error; err != nil {
		t.Fatalf("bind shared action permission: %v", err)
	}

	target := fmt.Sprintf("/api/rbac/roles/%s/actions", role.ID)
	rec := requestRBAC(t, server.handleRBACRole, http.MethodPut, target,
		`{"actions":["application:view","host:view","host:view"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response rbacRoleActionsResponse
	decodeRBACResponse(t, rec, &response)
	want := []string{rbac.ActionAppView, rbac.ActionHostView}
	if !reflect.DeepEqual(response.Actions, want) {
		t.Fatalf("actions = %#v, want %#v", response.Actions, want)
	}

	assertRolePermissionExists(t, db, role.ID, oldAction.ID, false)
	assertRolePermissionExists(t, db, role.ID, resourceAllow.ID, true)
	assertRolePermissionExists(t, db, role.ID, deny.ID, true)
	assertRolePermissionExists(t, db, otherRole.ID, otherAction.ID, true)

	getRec := requestRBAC(t, server.handleRBACRole, http.MethodGet, target, "")
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d; body=%s", getRec.Code, http.StatusOK, getRec.Body.String())
	}
	decodeRBACResponse(t, getRec, &response)
	if !reflect.DeepEqual(response.Actions, want) {
		t.Fatalf("GET actions = %#v, want %#v", response.Actions, want)
	}
}

func TestRBACReplaceRoleActionsRejectsUnknownWithoutChangingBindings(t *testing.T) {
	server, db := newRBACServer(t)
	role := createRBACRole(t, server, `{"name":"operators"}`)
	existing := createBoundPermission(t, db, role.ID, model.Permission{
		Action: rbac.ActionHostView, Effect: model.PermissionEffectAllow,
	})

	target := fmt.Sprintf("/api/rbac/roles/%s/actions", role.ID)
	rec := requestRBAC(t, server.handleRBACRole, http.MethodPut, target,
		`{"actions":["unknown:action"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	assertRolePermissionExists(t, db, role.ID, existing.ID, true)
}

func createBoundPermission(t *testing.T, db *gorm.DB, roleID string, permission model.Permission) model.Permission {
	t.Helper()
	if err := db.Create(&permission).Error; err != nil {
		t.Fatalf("create permission: %v", err)
	}
	binding := model.RolePermission{RoleID: roleID, PermissionID: permission.ID}
	if err := db.Create(&binding).Error; err != nil {
		t.Fatalf("create role permission: %v", err)
	}
	return permission
}

func assertRolePermissionExists(t *testing.T, db *gorm.DB, roleID, permissionID string, want bool) {
	t.Helper()
	var count int64
	if err := db.Model(&model.RolePermission{}).
		Where("role_id = ? AND permission_id = ?", roleID, permissionID).
		Count(&count).Error; err != nil {
		t.Fatalf("count role permission: %v", err)
	}
	if got := count == 1; got != want {
		t.Fatalf("role permission (%s, %s) exists = %v, want %v", roleID, permissionID, got, want)
	}
}
