package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestRBACNoDBReturns503AndStaticAPIsStillWork(t *testing.T) {
	server := newTargetTestServer(t)

	rbacRec := requestRBAC(t, server.handleRBACRoles, http.MethodGet, "/api/rbac/roles", "")
	if rbacRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("rbac status = %d, want %d; body=%s", rbacRec.Code, http.StatusServiceUnavailable, rbacRec.Body.String())
	}
	if !strings.Contains(rbacRec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("rbac Content-Type = %q, want application/json", rbacRec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rbacRec.Body.String(), rbacMetadataUnavailable) {
		t.Fatalf("rbac body missing unavailable error: %s", rbacRec.Body.String())
	}

	targetsRec := requestRBAC(t, server.handleTargets, http.MethodGet, "/api/targets", "")
	if targetsRec.Code != http.StatusOK {
		t.Fatalf("targets status = %d, want %d; body=%s", targetsRec.Code, http.StatusOK, targetsRec.Body.String())
	}
}

func TestRBACCreateRole(t *testing.T) {
	server, _ := newRBACServer(t)

	rec := requestRBAC(t, server.handleRBACRoles, http.MethodPost, "/api/rbac/roles", `{
		"name": " operators ",
		"description": " SSH operators "
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var role model.Role
	decodeRBACResponse(t, rec, &role)
	if role.ID == "" {
		t.Fatal("role ID was not generated")
	}
	if role.Name != "operators" || role.Description != "SSH operators" || role.Status != "active" {
		t.Fatalf("unexpected role: %#v", role)
	}
}

func TestRBACCreatePermission(t *testing.T) {
	server, _ := newRBACServer(t)

	rec := requestRBAC(t, server.handleRBACPermissions, http.MethodPost, "/api/rbac/permissions", `{
		"name": " connect host account ",
		"action": " session:connect ",
		"resource_type": " host_account ",
		"resource_id": " target-root ",
		"effect": " ALLOW "
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var permission model.Permission
	decodeRBACResponse(t, rec, &permission)
	if permission.ID == "" {
		t.Fatal("permission ID was not generated")
	}
	if permission.Name != "connect host account" ||
		permission.Action != "session:connect" ||
		permission.ResourceType != "host_account" ||
		permission.ResourceID != "target-root" ||
		permission.Effect != model.PermissionEffectAllow {
		t.Fatalf("unexpected permission: %#v", permission)
	}
}

func TestRBACBindingsAndEffectiveAllow(t *testing.T) {
	server, db := newRBACServer(t)
	if err := db.Create(&model.User{ID: "u1", Username: "u1"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	role := createRBACRole(t, server, `{"name":"ops"}`)
	permission := createRBACPermission(t, server, `{
		"name": "connect host",
		"action": "session:connect",
		"resource_type": "host_account",
		"resource_id": "target-root",
		"effect": "allow"
	}`)

	rolePermissionRec := requestRBAC(
		t,
		server.handleRBACRolePermissions,
		http.MethodPost,
		"/api/rbac/role-permissions",
		fmt.Sprintf(`{"role_id":%q,"permission_id":%q}`, role.ID, permission.ID),
	)
	if rolePermissionRec.Code != http.StatusCreated {
		t.Fatalf("role-permission status = %d, want %d; body=%s", rolePermissionRec.Code, http.StatusCreated, rolePermissionRec.Body.String())
	}
	var rolePermission model.RolePermission
	decodeRBACResponse(t, rolePermissionRec, &rolePermission)
	if rolePermission.ID == "" || rolePermission.RoleID != role.ID || rolePermission.PermissionID != permission.ID {
		t.Fatalf("unexpected role permission: %#v", rolePermission)
	}

	userRoleRec := requestRBAC(
		t,
		server.handleRBACUserRoles,
		http.MethodPost,
		"/api/rbac/user-roles",
		fmt.Sprintf(`{"user_id":"u1","role_id":%q}`, role.ID),
	)
	if userRoleRec.Code != http.StatusCreated {
		t.Fatalf("user-role status = %d, want %d; body=%s", userRoleRec.Code, http.StatusCreated, userRoleRec.Body.String())
	}
	var userRole model.UserRole
	decodeRBACResponse(t, userRoleRec, &userRole)
	if userRole.ID == "" || userRole.UserID != "u1" || userRole.RoleID != role.ID {
		t.Fatalf("unexpected user role: %#v", userRole)
	}

	effectiveRec := requestRBAC(
		t,
		server.handleRBACEffective,
		http.MethodGet,
		"/api/rbac/effective?user_id=u1&action=session%3Aconnect&resource_type=host_account&resource_id=target-root",
		"",
	)
	if effectiveRec.Code != http.StatusOK {
		t.Fatalf("effective status = %d, want %d; body=%s", effectiveRec.Code, http.StatusOK, effectiveRec.Body.String())
	}
	var effective rbacEffectiveResponse
	decodeRBACResponse(t, effectiveRec, &effective)
	if !effective.Allowed {
		t.Fatalf("effective allowed = false, want true; body=%s", effectiveRec.Body.String())
	}
}

func TestRBACDeleteBuiltinRoleRejected(t *testing.T) {
	server, _ := newRBACServer(t)
	role := createRBACRole(t, server, `{"name":"admin","builtin":true}`)

	rec := requestRBAC(t, server.handleRBACRole, http.MethodDelete, "/api/rbac/roles/"+role.ID, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func newRBACServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()

	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return &Server{
		db:     db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, db
}

func createRBACRole(t *testing.T, server *Server, body string) model.Role {
	t.Helper()

	rec := requestRBAC(t, server.handleRBACRoles, http.MethodPost, "/api/rbac/roles", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create role status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var role model.Role
	decodeRBACResponse(t, rec, &role)
	return role
}

func createRBACPermission(t *testing.T, server *Server, body string) model.Permission {
	t.Helper()

	rec := requestRBAC(t, server.handleRBACPermissions, http.MethodPost, "/api/rbac/permissions", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create permission status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var permission model.Permission
	decodeRBACResponse(t, rec, &permission)
	return permission
}

func requestRBAC(t *testing.T, handler http.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, target, reader)
	req = asTestSuperAdmin(req)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func decodeRBACResponse(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, rec.Body.String())
	}
}
