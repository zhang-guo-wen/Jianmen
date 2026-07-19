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
	"jianmen/internal/rbac"
	"jianmen/internal/service"
	"jianmen/internal/storage"
	"jianmen/internal/store"
)

func TestWriteRBACServiceErrorDoesNotExposeWrappedConflict(t *testing.T) {
	const sensitive = "UNIQUE constraint failed secret_table.permissions"

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rbac/roles", nil)
	writeRBACServiceError(rec, req, fmt.Errorf("role conflict: %w: %s", service.ErrRoleConflict, sensitive))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), sensitive) {
		t.Fatalf("response leaked sensitive error: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "RBAC resource already exists") {
		t.Fatalf("response missing fixed conflict message: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), sensitive) {
		t.Fatalf("server logger did not receive detailed error: %s", logs.String())
	}
}

func TestWriteRBACServiceErrorDoesNotExposeUnknownInternalError(t *testing.T) {
	const sensitive = "dial tcp secret.internal:5432: password=top-secret"

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/rbac/roles", nil)
	writeRBACServiceError(rec, req, fmt.Errorf("database unavailable: %s", sensitive))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), sensitive) {
		t.Fatalf("response leaked sensitive error: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "RBAC operation failed") {
		t.Fatalf("response missing fixed internal message: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), sensitive) {
		t.Fatalf("server logger did not receive detailed error: %s", logs.String())
	}
}

func TestDecodeRBACJSONDoesNotExposeParserDetails(t *testing.T) {
	const sensitive = "password=top-secret"

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/rbac/roles",
		strings.NewReader(`{"password=top-secret":"value"}`),
	)
	var request struct {
		Name string `json:"name"`
	}
	if decodeRBACJSON(rec, req, &request) {
		t.Fatal("decode unexpectedly accepted an unknown field")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), sensitive) {
		t.Fatalf("response leaked parser details: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid RBAC request") {
		t.Fatalf("response missing fixed validation message: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), sensitive) {
		t.Fatalf("server logger did not receive parser details: %s", logs.String())
	}
}

func TestRBACNoDBReturns503AndStaticAPIsStillWork(t *testing.T) {
	server := newTargetTestServer(t)
	server.db = nil

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

func TestRBACCreateDuplicateRoleReturnsConflict(t *testing.T) {
	server, _ := newRBACServer(t)
	createRBACRole(t, server, `{"name":"operators"}`)
	rec := requestRBAC(t, server.handleRBACRoles, http.MethodPost, "/api/rbac/roles", `{"name":"operators"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate role status = %d, want %d; body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestRBACRejectsBuiltinRoleCreationAndMutation(t *testing.T) {
	server, db := newRBACServer(t)
	rec := requestRBAC(t, server.handleRBACRoles, http.MethodPost, "/api/rbac/roles", `{"name":"builtin-from-api","builtin":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("builtin create status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	builtin := model.Role{ID: "builtin-role", Name: "builtin", Builtin: true, Status: "active"}
	if err := db.Create(&builtin).Error; err != nil {
		t.Fatalf("seed builtin role: %v", err)
	}
	rec = requestRBAC(t, server.handleRBACRole, http.MethodPut, "/api/rbac/roles/"+builtin.ID, `{"name":"renamed"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("builtin update status = %d, want %d; body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	rec = requestRBAC(t, server.handleRBACRole, http.MethodPut, "/api/rbac/roles/"+builtin.ID+"/actions", `{"actions":["host:view"]}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("builtin action replace status = %d, want %d; body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}

	normal := createRBACRole(t, server, `{"name":"normal-role"}`)
	rec = requestRBAC(t, server.handleRBACRole, http.MethodPut, "/api/rbac/roles/"+normal.ID, `{"name":"normal-role","builtin":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("builtin field flip status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var stored model.Role
	if err := db.First(&stored, "id = ?", normal.ID).Error; err != nil {
		t.Fatalf("load normal role: %v", err)
	}
	if stored.Builtin {
		t.Fatal("normal role builtin flag was changed")
	}
}

func TestRBACPermissionValidationAndBindingErrors(t *testing.T) {
	server, db := newRBACServer(t)
	for _, body := range []string{
		`{"action":"*"}`,
		`{"action":"unknown:action"}`,
		`{"action":"host:view","resource_type":"host_account","resource_id":"host-1"}`,
	} {
		rec := requestRBAC(t, server.handleRBACPermissions, http.MethodPost, "/api/rbac/permissions", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid permission status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	}
	permissionBody := `{"action":"session:view"}`
	rec := requestRBAC(t, server.handleRBACPermissions, http.MethodPost, "/api/rbac/permissions", permissionBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first permission status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	rec = requestRBAC(t, server.handleRBACPermissions, http.MethodPost, "/api/rbac/permissions", permissionBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate permission status = %d, want %d; body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}

	if err := db.Create(&model.User{ID: "binding-user", Username: "binding-user", Status: "active"}).Error; err != nil {
		t.Fatalf("create binding user: %v", err)
	}
	rec = requestRBAC(t, server.handleRBACUserRoles, http.MethodPost, "/api/rbac/user-roles", `{"user_id":"binding-user","role_id":"missing"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing role binding status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}

	role := createRBACRole(t, server, `{"name":"binding-role"}`)
	userRoleBody := fmt.Sprintf(`{"user_id":"binding-user","role_id":%q}`, role.ID)
	rec = requestRBAC(t, server.handleRBACUserRoles, http.MethodPost, "/api/rbac/user-roles", userRoleBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first user-role status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	rec = requestRBAC(t, server.handleRBACUserRoles, http.MethodPost, "/api/rbac/user-roles", userRoleBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate user-role status = %d, want %d; body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}

	permission := createRBACPermission(t, server, `{"action":"host:view"}`)
	rolePermissionBody := fmt.Sprintf(`{"role_id":%q,"permission_id":%q}`, role.ID, permission.ID)
	rec = requestRBAC(t, server.handleRBACRolePermissions, http.MethodPost, "/api/rbac/role-permissions", rolePermissionBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first role-permission status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	rec = requestRBAC(t, server.handleRBACRolePermissions, http.MethodPost, "/api/rbac/role-permissions", rolePermissionBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate role-permission status = %d, want %d; body=%s", rec.Code, http.StatusConflict, rec.Body.String())
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
		"effect": "allow"
	}`)
	if err := db.Create(&model.ResourceGrant{
		PrincipalType: "user",
		PrincipalID:   "u1",
		ResourceType:  model.ResourceTypeHostAccount,
		ResourceID:    "target-root",
		Effect:        model.PermissionEffectAllow,
	}).Error; err != nil {
		t.Fatalf("create resource grant: %v", err)
	}

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
	server, db := newRBACServer(t)
	role := model.Role{ID: "builtin-admin", Name: "admin", Builtin: true, Status: "active"}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("seed builtin role: %v", err)
	}

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
	storeInst := store.NewDBStore(db)
	identityService, err := service.NewIdentityService(storeInst)
	if err != nil {
		t.Fatalf("new RBAC identity service: %v", err)
	}
	authorizationService, err := service.NewAuthorizationService(
		identityService,
		rbac.NewChecker(db),
		rbac.NewResourceGrantChecker(db),
	)
	if err != nil {
		t.Fatalf("new RBAC authorization service: %v", err)
	}
	seedTestSuperAdmin(t, db, "u-admin")
	server := &Server{
		db:            db,
		identity:      identityService,
		authorization: authorizationService,
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	applyTestAdminDependencies(t, server, storeInst)
	roleManagement, err := newRoleManagementService(storeInst)
	if err != nil {
		t.Fatalf("new RBAC role service: %v", err)
	}
	server.roleManagement = roleManagement
	return server, db
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
	var wrapper struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wrapper); err != nil {
		t.Fatalf("unmarshal response wrapper: %v; body=%s", err, rec.Body.String())
	}
	if err := json.Unmarshal(wrapper.Data, dst); err != nil {
		t.Fatalf("unmarshal response data: %v; body=%s", err, rec.Body.String())
	}
}
