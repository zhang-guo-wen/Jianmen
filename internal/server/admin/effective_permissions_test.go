package admin

import (
	"reflect"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestEffectiveGlobalActionsExcludesResourcePermissionsAndHonorsDeny(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	role := model.Role{ID: "role", Name: "role", Status: "active"}
	permissions := []model.Permission{
		{ID: "allow-host", Action: "host:view", Effect: model.PermissionEffectAllow},
		{ID: "deny-host", Action: "host:view", Effect: model.PermissionEffectDeny},
		{ID: "allow-db", Action: "dbproxy:view", Effect: model.PermissionEffectAllow},
		{ID: "resource-app", Action: "app:connect", ResourceType: model.ResourceTypeApplication, ResourceID: "app1", Effect: model.PermissionEffectAllow},
	}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := db.Create(&model.UserRole{ID: "ur", UserID: "u1", RoleID: role.ID}).Error; err != nil {
		t.Fatalf("create user role: %v", err)
	}
	for _, permission := range permissions {
		if err := db.Create(&permission).Error; err != nil {
			t.Fatalf("create permission: %v", err)
		}
		if err := db.Create(&model.RolePermission{RoleID: role.ID, PermissionID: permission.ID}).Error; err != nil {
			t.Fatalf("create role permission: %v", err)
		}
	}

	actions, err := (&Server{db: db}).effectiveGlobalActions("u1")
	if err != nil {
		t.Fatalf("effectiveGlobalActions: %v", err)
	}
	if want := []string{"dbproxy:view"}; !reflect.DeepEqual(actions, want) {
		t.Fatalf("actions = %#v, want %#v", actions, want)
	}
}
