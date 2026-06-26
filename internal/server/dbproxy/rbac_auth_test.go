//go:build ignore

package dbproxy

import (
	"bytes"
	"io"
	"log/slog"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
	rbaccheck "jianmen/internal/rbac"
	"jianmen/internal/storage"

	"gorm.io/gorm"
)

type permissionCheckCall struct {
	userID       string
	action       string
	resourceType string
	resourceID   string
}

type fakePermissionChecker struct {
	allowed bool
	err     error
	calls   []permissionCheckCall
}

func (f *fakePermissionChecker) HasPermission(userID, action, resourceType, resourceID string) (bool, error) {
	f.calls = append(f.calls, permissionCheckCall{
		userID:       userID,
		action:       action,
		resourceType: resourceType,
		resourceID:   resourceID,
	})
	return f.allowed, f.err
}

func TestCopyClientToUpstreamChecksRBACDatabaseAccount(t *testing.T) {
	packet := mysqlLoginPacket("app")
	proxy := config.DatabaseProxyConfig{Name: "mysql-local", Protocol: "mysql", ListenAddr: "127.0.0.1:33060"}
	checker := &fakePermissionChecker{allowed: true}
	manager := newRBACAuthTestManager(checker)
	auth := newRBACAuthTestAccountAuth(t, "mysql", []string{"app"})
	var upstream bytes.Buffer

	manager.copyClientToUpstream(proxy, &upstream, bytes.NewReader(packet), io.Discard, noopObserver{}, auth, nil)

	if !bytes.Equal(upstream.Bytes(), packet) {
		t.Fatalf("upstream bytes = %x, want %x", upstream.Bytes(), packet)
	}
	if len(checker.calls) != 1 {
		t.Fatalf("permission checks = %d, want 1: %#v", len(checker.calls), checker.calls)
	}
	wantResourceID := rbaccheck.DatabaseAccountResourceID(proxy.Name, proxy.ListenAddr, "app")
	call := checker.calls[0]
	if call.userID != "app" ||
		call.action != rbaccheck.ActionDBConnect ||
		call.resourceType != model.ResourceTypeDatabaseAccount ||
		call.resourceID != wantResourceID {
		t.Fatalf("unexpected permission check: %#v", call)
	}
}

func TestCopyClientToUpstreamRejectsRBACDeniedDatabaseAccount(t *testing.T) {
	packet := mysqlLoginPacket("app")
	proxy := config.DatabaseProxyConfig{Name: "mysql-local", Protocol: "mysql", ListenAddr: "127.0.0.1:33060"}
	checker := &fakePermissionChecker{allowed: false}
	manager := newRBACAuthTestManager(checker)
	auth := newRBACAuthTestAccountAuth(t, "mysql", []string{"app"})
	var upstream bytes.Buffer

	manager.copyClientToUpstream(proxy, &upstream, bytes.NewReader(packet), io.Discard, noopObserver{}, auth, nil)

	if upstream.Len() != 0 {
		t.Fatalf("upstream received %d bytes after RBAC denial", upstream.Len())
	}
	if len(checker.calls) != 1 {
		t.Fatalf("permission checks = %d, want 1: %#v", len(checker.calls), checker.calls)
	}
}

func TestCopyClientToUpstreamWithMetadataDBAllowsGrantedDatabaseAccount(t *testing.T) {
	db := newRBACAuthTestDB(t)
	proxy := config.DatabaseProxyConfig{Name: "mysql-local", Protocol: "mysql", ListenAddr: "127.0.0.1:33060"}
	resourceID := rbaccheck.DatabaseAccountResourceID(proxy.Name, proxy.ListenAddr, "app")
	seedDBConnectGrant(t, db, "app", resourceID)
	manager := NewManager(nil, "", slog.New(slog.NewTextHandler(io.Discard, nil)), db)
	auth := newRBACAuthTestAccountAuth(t, "mysql", []string{"app"})
	packet := mysqlLoginPacket("app")
	var upstream bytes.Buffer

	manager.copyClientToUpstream(proxy, &upstream, bytes.NewReader(packet), io.Discard, noopObserver{}, auth, nil)

	if !bytes.Equal(upstream.Bytes(), packet) {
		t.Fatalf("upstream bytes = %x, want %x", upstream.Bytes(), packet)
	}
}

func TestCopyClientToUpstreamAllowedUsersRejectsBeforeRBAC(t *testing.T) {
	packet := mysqlLoginPacket("app")
	proxy := config.DatabaseProxyConfig{Name: "mysql-local", Protocol: "mysql", ListenAddr: "127.0.0.1:33060"}
	checker := &fakePermissionChecker{allowed: true}
	manager := newRBACAuthTestManager(checker)
	auth := newRBACAuthTestAccountAuth(t, "mysql", []string{"report"})
	var upstream bytes.Buffer

	manager.copyClientToUpstream(proxy, &upstream, bytes.NewReader(packet), io.Discard, noopObserver{}, auth, nil)

	if upstream.Len() != 0 {
		t.Fatalf("upstream received %d bytes after allowed_users denial", upstream.Len())
	}
	if len(checker.calls) != 0 {
		t.Fatalf("permission checks = %d, want 0: %#v", len(checker.calls), checker.calls)
	}
}

func TestCopyClientToUpstreamSkipsRBACWhenAllowedUsersNotConfigured(t *testing.T) {
	packet := mysqlLoginPacket("app")
	proxy := config.DatabaseProxyConfig{Name: "mysql-local", Protocol: "mysql", ListenAddr: "127.0.0.1:33060"}
	checker := &fakePermissionChecker{allowed: false}
	manager := newRBACAuthTestManager(checker)
	auth := newRBACAuthTestAccountAuth(t, "mysql", nil)
	var upstream bytes.Buffer

	manager.copyClientToUpstream(proxy, &upstream, bytes.NewReader(packet), io.Discard, noopObserver{}, auth, nil)

	if !bytes.Equal(upstream.Bytes(), packet) {
		t.Fatalf("upstream bytes = %x, want %x", upstream.Bytes(), packet)
	}
	if len(checker.calls) != 0 {
		t.Fatalf("permission checks = %d, want 0: %#v", len(checker.calls), checker.calls)
	}
}

func newRBACAuthTestManager(checker permissionChecker) *Manager {
	return &Manager{
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		permissionChecker: checker,
	}
}

func newRBACAuthTestAccountAuth(t *testing.T, protocol string, allowedUsers []string) *accountAuthState {
	t.Helper()
	auth, err := newAccountAuth(protocol, allowedUsers)
	if err != nil {
		t.Fatalf("newAccountAuth returned error: %v", err)
	}
	return auth
}

func newRBACAuthTestDB(t *testing.T) *gorm.DB {
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
	return db
}

func seedDBConnectGrant(t *testing.T, db *gorm.DB, userID, resourceID string) {
	t.Helper()
	if err := db.Create(&model.User{ID: userID, Username: userID, Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&model.Role{ID: "r-" + userID, Name: "role-" + userID, Status: "active"}).Error; err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := db.Create(&model.UserRole{ID: "ur-" + userID, UserID: userID, RoleID: "r-" + userID}).Error; err != nil {
		t.Fatalf("create user role: %v", err)
	}
	permission := model.Permission{
		ID:           "p-db-connect-" + userID,
		Action:       rbaccheck.ActionDBConnect,
		ResourceType: model.ResourceTypeDatabaseAccount,
		ResourceID:   resourceID,
		Effect:       model.PermissionEffectAllow,
	}
	if err := db.Create(&permission).Error; err != nil {
		t.Fatalf("create permission: %v", err)
	}
	if err := db.Create(&model.RolePermission{
		ID:           "rp-" + permission.ID,
		RoleID:       "r-" + userID,
		PermissionID: permission.ID,
	}).Error; err != nil {
		t.Fatalf("create role permission: %v", err)
	}
}
