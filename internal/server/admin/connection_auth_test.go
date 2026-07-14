package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

func TestAuthorizeConnectionRequiresActionAndResourceGrant(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedConnectionAction(t, db, "user-connect", rbac.ActionSessionConnect)

	allowed, err := server.authorizeConnection("user-connect", rbac.ActionSessionConnect, model.ResourceTypeHostAccount, "account-1")
	if err != nil {
		t.Fatalf("authorize with action only: %v", err)
	}
	if allowed {
		t.Fatal("action permission without a resource grant was allowed")
	}

	grant := model.ResourceGrant{
		PrincipalType: "user",
		PrincipalID:   "user-connect",
		ResourceType:  model.ResourceTypeHostAccount,
		ResourceID:    "account-1",
		Effect:        model.PermissionEffectAllow,
	}
	if err := db.Create(&grant).Error; err != nil {
		t.Fatalf("create resource grant: %v", err)
	}
	allowed, err = server.authorizeConnection("user-connect", rbac.ActionSessionConnect, model.ResourceTypeHostAccount, "account-1")
	if err != nil {
		t.Fatalf("authorize with grant: %v", err)
	}
	if !allowed {
		t.Fatal("action permission plus resource grant was denied")
	}
}

func TestAuthorizeConnectionSuperAdminBypassesChecks(t *testing.T) {
	server, _ := newAdminDBTestServer(t)
	server.superAdminIDs["super"] = true

	allowed, err := server.authorizeConnection("super", rbac.ActionDBConnect, model.ResourceTypeDatabaseAccount, "missing")
	if err != nil {
		t.Fatalf("authorize super administrator: %v", err)
	}
	if !allowed {
		t.Fatal("super administrator did not bypass connection authorization")
	}
}

func TestHandleSavedDatabaseAccountTestRequiresConnectionAuthorization(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedConnectionAction(t, db, "db-user", rbac.ActionDBConnect)
	inst := model.DatabaseInstance{ID: "db-instance", Name: "db-instance", Protocol: "mysql", Address: "127.0.0.1", Port: 1, Status: "active"}
	acct := model.DatabaseAccount{ID: "db-account", InstanceID: inst.ID, UniqueName: "db-account", Username: "app", Status: "active"}
	if err := db.Create(&inst).Error; err != nil {
		t.Fatalf("create database instance: %v", err)
	}
	if err := db.Create(&acct).Error; err != nil {
		t.Fatalf("create database account: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/db/accounts/test/"+acct.ID, nil)
	req = withTestUser(req, "db-user", "db-user")
	rec := httptest.NewRecorder()
	server.handleTestDBConnection(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("without grant status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	grant := model.ResourceGrant{PrincipalType: "user", PrincipalID: "db-user", ResourceType: model.ResourceTypeDatabaseAccount, ResourceID: acct.ID, Effect: model.PermissionEffectAllow}
	if err := db.Create(&grant).Error; err != nil {
		t.Fatalf("create database account grant: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/db/accounts/test/"+acct.ID, nil)
	req = withTestUser(req, "db-user", "db-user")
	rec = httptest.NewRecorder()
	server.handleTestDBConnection(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("with grant status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHandleUnsavedDatabaseAccountTestRequiresCreatePermission(t *testing.T) {
	server, _ := newAdminDBTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/db/accounts/test", strings.NewReader(`{"instance_id":"db-instance","username":"app","password":"secret"}`))
	req = withTestUser(req, "regular", "regular")
	rec := httptest.NewRecorder()

	server.handleTestDBConnection(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAuthenticateWebTerminalReturnsActiveUser(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	users := []model.User{
		{ID: "active-web", Username: "alice", TokenHash: hashToken("active-token"), Status: "active"},
		{ID: "disabled-web", Username: "bob", TokenHash: hashToken("disabled-token"), Status: "disabled"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, webTerminalPath+"?token=active-token", nil)
	user, ok := server.authenticateWebTerminal(req)
	if !ok || user.ID != "active-web" || user.Username != "alice" {
		t.Fatalf("authenticated user = %#v, ok=%v", user, ok)
	}
	req = httptest.NewRequest(http.MethodGet, webTerminalPath+"?token=disabled-token", nil)
	if _, ok := server.authenticateWebTerminal(req); ok {
		t.Fatal("disabled user token was accepted")
	}
}

func TestHandleWebTerminalRequiresHostAccountGrant(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedConnectionAction(t, db, "web-user", rbac.ActionSessionConnect)
	if err := db.Model(&model.User{}).Where("id = ?", "web-user").Update("token_hash", hashToken("web-token")).Error; err != nil {
		t.Fatalf("set web token: %v", err)
	}
	host := model.Host{ID: "web-host", Name: "web-host", Address: "127.0.0.1", Port: 22, Status: "active"}
	account := model.HostAccount{ID: "web-account", HostID: host.ID, Username: "root", Status: "active", ResourceSeq: 1, ResourceID: "H001"}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create host account: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, webTerminalPath+"?target_id="+account.ID+"&token=web-token", nil)
	rec := httptest.NewRecorder()
	server.handleWebTerminal(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestWebTerminalRecorderUsesAuthenticatedIdentity(t *testing.T) {
	server, _ := newAdminDBTestServer(t)
	server.cfg = &config.Config{ReplayDir: t.TempDir(), Recording: config.RecordingConfig{Enabled: true}}
	user := model.User{ID: "real-user-id", Username: "real-user"}
	target := store.TargetConfig{ID: "target-1", HostID: "host-1", Name: "root@127.0.0.1", Host: "127.0.0.1", Port: 22, Username: "root"}
	req := httptest.NewRequest(http.MethodGet, webTerminalPath, nil)
	recorder := server.newWebTerminalRecorder(req, user, target)
	if recorder == nil {
		t.Fatal("web terminal recorder was not created")
	}
	defer recorder.Close()

	raw, err := os.ReadFile(filepath.Join(recorder.Dir(), "meta.json"))
	if err != nil {
		t.Fatalf("read recorder metadata: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("decode recorder metadata: %v", err)
	}
	if meta["user_id"] != user.ID || meta["user"] != user.Username {
		t.Fatalf("recorder identity = user_id:%v user:%v", meta["user_id"], meta["user"])
	}
}

func seedConnectionAction(t *testing.T, db *gorm.DB, userID, action string) {
	t.Helper()
	user := model.User{ID: userID, Username: userID, Status: "active"}
	role := model.Role{ID: "role-" + userID + "-" + action, Name: "role-" + userID + "-" + action, Status: "active"}
	permission := model.Permission{ID: "perm-" + userID + "-" + action, Action: action, Effect: model.PermissionEffectAllow}
	for _, value := range []any{&user, &role, &permission} {
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("seed connection authorization: %v", err)
		}
	}
	if err := db.Create(&model.UserRole{ID: "ur-" + userID + "-" + action, UserID: user.ID, RoleID: role.ID}).Error; err != nil {
		t.Fatalf("create user role: %v", err)
	}
	if err := db.Create(&model.RolePermission{ID: "rp-" + userID + "-" + action, RoleID: role.ID, PermissionID: permission.ID}).Error; err != nil {
		t.Fatalf("create role permission: %v", err)
	}
}

func withTestUser(req *http.Request, userID, username string) *http.Request {
	ctx := context.WithValue(req.Context(), ctxKeyUserID, userID)
	ctx = context.WithValue(ctx, ctxKeyUsername, username)
	return req.WithContext(ctx)
}
