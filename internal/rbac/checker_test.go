package rbac

import (
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/storage"
	"gorm.io/gorm"
)

func TestHasPermissionRequiresActionAndResourceGrant(t *testing.T) {
	db := newTestDB(t)
	seedRBAC(t, db, "u1", []model.Permission{
		{ID: "p-connect", Action: "session:connect", Effect: model.PermissionEffectAllow},
		{ID: "p-host-1", ResourceType: "host", ResourceID: "host-1", Effect: model.PermissionEffectAllow},
		{ID: "p-read-host-1", Action: "sftp:read", ResourceType: "host", ResourceID: "host-1", Effect: model.PermissionEffectAllow},
	})

	checker := NewChecker(db)
	assertPermission(t, checker, "u1", "session:connect", "", "", true)
	assertPermission(t, checker, "u1", "session:connect", "host", "host-1", true)
	assertPermission(t, checker, "u1", "session:connect", "host", "host-2", false)
	assertPermission(t, checker, "u1", "sftp:write", "host", "host-1", false)
	assertPermission(t, checker, "u1", "sftp:read", "host", "host-1", true)
	assertPermission(t, checker, "missing", "session:connect", "", "", false)
}

func TestHasPermissionSupportsResourceGroupGrant(t *testing.T) {
	db := newTestDB(t)
	seedRBAC(t, db, "u1", []model.Permission{
		{ID: "p-connect", Action: "session:connect", Effect: model.PermissionEffectAllow},
		{ID: "p-group", ResourceType: model.ResourceTypeGroup, ResourceID: "g1", Effect: model.PermissionEffectAllow},
	})
	if err := db.Create(&model.ResourceGroup{ID: "g1", Name: "prod", ResourceType: "host"}).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := db.Create(&model.ResourceGroupMember{
		ID:           "gm1",
		GroupID:      "g1",
		ResourceType: "host",
		ResourceID:   "host-2",
	}).Error; err != nil {
		t.Fatalf("create group member: %v", err)
	}

	checker := NewChecker(db)
	assertPermission(t, checker, "u1", "session:connect", "host", "host-2", true)
	assertPermission(t, checker, "u1", "session:connect", "host", "host-3", false)
}

func TestHasPermissionDenyOverridesAllow(t *testing.T) {
	db := newTestDB(t)
	seedRBAC(t, db, "u1", []model.Permission{
		{ID: "p-connect", Action: "session:connect", Effect: model.PermissionEffectAllow},
		{ID: "p-host-1", ResourceType: "host", ResourceID: "host-1", Effect: model.PermissionEffectAllow},
		{ID: "p-deny-host-1", Action: "session:connect", ResourceType: "host", ResourceID: "host-1", Effect: model.PermissionEffectDeny},
	})

	assertPermission(t, NewChecker(db), "u1", "session:connect", "host", "host-1", false)
}

func newTestDB(t *testing.T) *gorm.DB {
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

func seedRBAC(t *testing.T, db *gorm.DB, userID string, permissions []model.Permission) {
	t.Helper()

	if err := db.Create(&model.User{ID: userID, Username: userID}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&model.Role{ID: "r-" + userID, Name: "role-" + userID, Status: "active"}).Error; err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := db.Create(&model.UserRole{ID: "ur-" + userID, UserID: userID, RoleID: "r-" + userID}).Error; err != nil {
		t.Fatalf("create user role: %v", err)
	}
	for i := range permissions {
		if err := db.Create(&permissions[i]).Error; err != nil {
			t.Fatalf("create permission %s: %v", permissions[i].ID, err)
		}
		if err := db.Create(&model.RolePermission{
			ID:           "rp-" + permissions[i].ID,
			RoleID:       "r-" + userID,
			PermissionID: permissions[i].ID,
		}).Error; err != nil {
			t.Fatalf("create role permission %s: %v", permissions[i].ID, err)
		}
	}
}

func assertPermission(t *testing.T, checker *Checker, userID, action, resourceType, resourceID string, want bool) {
	t.Helper()

	got, err := checker.HasPermission(userID, action, resourceType, resourceID)
	if err != nil {
		t.Fatalf("has permission: %v", err)
	}
	if got != want {
		t.Fatalf("HasPermission(%q, %q, %q, %q) = %v, want %v", userID, action, resourceType, resourceID, got, want)
	}
}
