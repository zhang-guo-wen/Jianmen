package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
	"jianmen/internal/storage"
	"jianmen/internal/store"

	"gorm.io/gorm"
)

// This is deliberately the first red test for the role boundary: services
// must reject missing repositories instead of allowing handlers to fall back
// to direct database access.
func TestNewRoleServiceRequiresRepository(t *testing.T) {
	if _, err := service.NewRoleService(nil); err == nil {
		t.Fatal("NewRoleService(nil) succeeded")
	}
}

func TestRoleServiceRejectsBuiltinCreationAndProtectsBuiltinMutations(t *testing.T) {
	roles, db := newRoleServiceTest(t)
	builtin := true
	if _, err := roles.Create(context.Background(), service.RoleInput{Name: "administrators", Builtin: &builtin}); !errors.Is(err, service.ErrInvalidRole) {
		t.Fatalf("create builtin role error = %v, want ErrInvalidRole", err)
	}

	role := model.Role{ID: "builtin-role", Name: "administrators", Builtin: true, Status: "active"}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("seed builtin role: %v", err)
	}
	if _, err := roles.Update(context.Background(), role.ID, service.RoleInput{Name: "renamed"}); !errors.Is(err, service.ErrBuiltinRole) {
		t.Fatalf("update builtin role error = %v, want ErrBuiltinRole", err)
	}
	if err := roles.Delete(context.Background(), role.ID); !errors.Is(err, service.ErrBuiltinRole) {
		t.Fatalf("delete builtin role error = %v, want ErrBuiltinRole", err)
	}
	if _, err := roles.ReplaceActions(context.Background(), role.ID, []string{rbac.ActionHostView}); !errors.Is(err, service.ErrBuiltinRole) {
		t.Fatalf("replace builtin role actions error = %v, want ErrBuiltinRole", err)
	}

	normal, err := roles.Create(context.Background(), service.RoleInput{Name: "operators"})
	if err != nil {
		t.Fatalf("create normal role: %v", err)
	}
	if _, err := roles.Update(context.Background(), normal.ID, service.RoleInput{Name: "operators", Builtin: &builtin}); !errors.Is(err, service.ErrInvalidRole) {
		t.Fatalf("flip builtin field error = %v, want ErrInvalidRole", err)
	}
}

func TestRoleServiceValidatesPermissionCatalogAndResourceScope(t *testing.T) {
	roles, _ := newRoleServiceTest(t)
	valid := []service.PermissionInput{
		{Action: rbac.ActionHostView},
		{Action: rbac.ActionSessionConnect, ResourceType: model.ResourceTypeHostAccount, ResourceID: "host-account-1"},
		{Action: rbac.ActionSessionConnect, ResourceType: model.ResourceTypeGroup, ResourceID: "group-1"},
		{ResourceType: model.ResourceTypeHostAccount, ResourceID: "host-account-1"},
	}
	for index, input := range valid {
		if _, err := roles.CreatePermission(context.Background(), input); err != nil {
			t.Fatalf("valid permission %d rejected: %v", index, err)
		}
	}

	invalid := []service.PermissionInput{
		{Action: "*"},
		{Action: "unknown:action"},
		{Action: rbac.ActionHostView, ResourceType: model.ResourceTypeHostAccount, ResourceID: "host-account-1"},
		{Action: rbac.ActionDBConnect, ResourceType: model.ResourceTypeHostAccount, ResourceID: "host-account-1"},
		{Action: rbac.ActionSessionConnect, ResourceType: model.ResourceTypeDatabaseAccount, ResourceID: "db-account-1"},
		{ResourceType: model.ResourceTypeHostAccount},
	}
	for index, input := range invalid {
		if _, err := roles.CreatePermission(context.Background(), input); !errors.Is(err, service.ErrInvalidPermission) {
			t.Fatalf("invalid permission %d error = %v, want ErrInvalidPermission", index, err)
		}
	}
}

func TestRoleServiceValidatesBindingReferencesAndExpiry(t *testing.T) {
	roles, db := newRoleServiceTest(t)
	role, err := roles.Create(context.Background(), service.RoleInput{Name: "operators"})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	permission, err := roles.CreatePermission(context.Background(), service.PermissionInput{Action: rbac.ActionHostView})
	if err != nil {
		t.Fatalf("create permission: %v", err)
	}
	if err := db.Create(&model.User{ID: "user-1", Username: "user-1", Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	future := time.Now().Add(time.Hour)
	if _, err := roles.CreateUserRole(context.Background(), service.UserRoleInput{UserID: "user-1", RoleID: role.ID, ExpiresAt: &future}); err != nil {
		t.Fatalf("create valid user role: %v", err)
	}
	past := time.Now().Add(-time.Minute)
	if _, err := roles.CreateUserRole(context.Background(), service.UserRoleInput{UserID: "user-1", RoleID: role.ID, ExpiresAt: &past}); !errors.Is(err, service.ErrInvalidRole) {
		t.Fatalf("expired user role error = %v, want ErrInvalidRole", err)
	}
	if _, err := roles.CreateUserRole(context.Background(), service.UserRoleInput{UserID: "missing-user", RoleID: role.ID}); !errors.Is(err, service.ErrUserNotFound) {
		t.Fatalf("missing user error = %v, want ErrUserNotFound", err)
	}
	if _, err := roles.CreateUserRole(context.Background(), service.UserRoleInput{UserID: "user-1", RoleID: "missing-role"}); !errors.Is(err, service.ErrRoleNotFound) {
		t.Fatalf("missing role error = %v, want ErrRoleNotFound", err)
	}
	if _, err := roles.CreateRolePermission(context.Background(), service.RolePermissionInput{RoleID: "missing-role", PermissionID: permission.ID}); !errors.Is(err, service.ErrRoleNotFound) {
		t.Fatalf("missing role permission role error = %v, want ErrRoleNotFound", err)
	}
	if _, err := roles.CreateRolePermission(context.Background(), service.RolePermissionInput{RoleID: role.ID, PermissionID: "missing-permission"}); !errors.Is(err, service.ErrPermissionNotFound) {
		t.Fatalf("missing permission error = %v, want ErrPermissionNotFound", err)
	}
}

func TestRoleServiceMapsDuplicateRoleAndBindingToConflict(t *testing.T) {
	roles, db := newRoleServiceTest(t)
	role, err := roles.Create(context.Background(), service.RoleInput{Name: "operators"})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	if _, err := roles.Create(context.Background(), service.RoleInput{Name: "operators"}); !errors.Is(err, service.ErrRoleConflict) {
		t.Fatalf("duplicate role error = %v, want ErrRoleConflict", err)
	}
	if err := db.Create(&model.User{ID: "user-1", Username: "user-1", Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := roles.CreateUserRole(context.Background(), service.UserRoleInput{UserID: "user-1", RoleID: role.ID}); err != nil {
		t.Fatalf("create binding: %v", err)
	}
	if _, err := roles.CreateUserRole(context.Background(), service.UserRoleInput{UserID: "user-1", RoleID: role.ID}); !errors.Is(err, service.ErrRoleBindingConflict) {
		t.Fatalf("duplicate binding error = %v, want ErrRoleBindingConflict", err)
	}
}

func TestRoleServiceEffectiveActionsHonorExpiryDenyAndDoNotPaginate(t *testing.T) {
	roles, db := newRoleServiceTest(t)
	if err := db.Create(&model.User{ID: "user-1", Username: "user-1", Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	active := model.Role{ID: "active", Name: "active", Status: "active"}
	expired := model.Role{ID: "expired", Name: "expired", Status: "active"}
	if err := db.Create(&[]model.Role{active, expired}).Error; err != nil {
		t.Fatalf("create roles: %v", err)
	}
	past := time.Now().Add(-time.Minute)
	if err := db.Create(&[]model.UserRole{{UserID: "user-1", RoleID: active.ID}, {UserID: "user-1", RoleID: expired.ID, ExpiresAt: &past}}).Error; err != nil {
		t.Fatalf("create user roles: %v", err)
	}
	for index := 0; index < 60; index++ {
		permission := model.Permission{Action: fmt.Sprintf("custom:%02d", index), Effect: model.PermissionEffectAllow}
		if err := db.Create(&permission).Error; err != nil {
			t.Fatalf("create permission %d: %v", index, err)
		}
		if err := db.Create(&model.RolePermission{RoleID: active.ID, PermissionID: permission.ID}).Error; err != nil {
			t.Fatalf("bind permission %d: %v", index, err)
		}
	}
	deny := model.Permission{Action: "custom:10", Effect: model.PermissionEffectDeny}
	if err := db.Create(&deny).Error; err != nil {
		t.Fatalf("create deny: %v", err)
	}
	if err := db.Create(&model.RolePermission{RoleID: active.ID, PermissionID: deny.ID}).Error; err != nil {
		t.Fatalf("bind deny: %v", err)
	}
	expiredPermission := model.Permission{Action: rbac.ActionRBACManage, Effect: model.PermissionEffectAllow}
	if err := db.Create(&expiredPermission).Error; err != nil {
		t.Fatalf("create expired permission: %v", err)
	}
	if err := db.Create(&model.RolePermission{RoleID: expired.ID, PermissionID: expiredPermission.ID}).Error; err != nil {
		t.Fatalf("bind expired permission: %v", err)
	}
	actions, err := roles.EffectiveGlobalActions(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("effective actions: %v", err)
	}
	if len(actions) != 59 {
		t.Fatalf("effective action count = %d, want 59", len(actions))
	}
	for _, action := range actions {
		if action == "custom:10" || action == rbac.ActionRBACManage {
			t.Fatalf("unexpected effective action %q in %#v", action, actions)
		}
	}
}

func newRoleServiceTest(t *testing.T) (*service.RoleService, *gorm.DB) {
	t.Helper()
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	roles, err := service.NewRoleService(store.NewDBStore(db))
	if err != nil {
		t.Fatalf("new role service: %v", err)
	}
	return roles, db
}
