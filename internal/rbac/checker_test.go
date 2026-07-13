package rbac

import (
	"testing"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestHasPermissionRequiresActionAndResourceGrant(t *testing.T) {
	db := newTestDB(t)
	seedRBAC(t, db, "u1", []model.Permission{
		{ID: "p-connect", Action: "session:connect", Effect: model.PermissionEffectAllow},
		{ID: "p-target-root", ResourceType: "host_account", ResourceID: "target-root", Effect: model.PermissionEffectAllow},
		{ID: "p-read-target-root", Action: "sftp:read", ResourceType: "host_account", ResourceID: "target-root", Effect: model.PermissionEffectAllow},
	})

	checker := NewChecker(db)
	assertPermission(t, checker, "u1", "session:connect", "", "", true)
	assertPermission(t, checker, "u1", "session:connect", "host_account", "target-root", true)
	assertPermission(t, checker, "u1", "session:connect", "host_account", "target-ubuntu", false)
	assertPermission(t, checker, "u1", "sftp:write", "host_account", "target-root", false)
	assertPermission(t, checker, "u1", "sftp:read", "host_account", "target-root", true)
	assertPermission(t, checker, "missing", "session:connect", "", "", false)
}

func TestHasPermissionSupportsResourceGroupGrant(t *testing.T) {
	db := newTestDB(t)
	seedRBAC(t, db, "u1", []model.Permission{
		{ID: "p-connect", Action: "session:connect", Effect: model.PermissionEffectAllow},
		{ID: "p-group", ResourceType: model.ResourceTypeGroup, ResourceID: "g1", Effect: model.PermissionEffectAllow},
	})
	// 创建资源组
	if err := db.Create(&model.ResourceGroup{ID: "g1", Name: "prod"}).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	// 创建主机（分组名匹配资源组名）
	if err := db.Create(&model.Host{ID: "h1", Name: "server1", Address: "10.0.0.1", Port: 22, GroupName: "prod"}).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	// 创建主机账号（属于该主机）
	if err := db.Create(&model.HostAccount{ID: "target-ubuntu", HostID: "h1", Username: "ubuntu", Status: "active"}).Error; err != nil {
		t.Fatalf("create host account: %v", err)
	}

	checker := NewChecker(db)
	assertPermission(t, checker, "u1", "session:connect", "host_account", "target-ubuntu", true)
	assertPermission(t, checker, "u1", "session:connect", "host_account", "target-missing", false)
}

func TestHasPermissionSupportsGroupForHostAndDatabaseInstance(t *testing.T) {
	db := newTestDB(t)
	seedRBAC(t, db, "u1", []model.Permission{
		{ID: "p-connect", Action: "session:connect", Effect: model.PermissionEffectAllow},
		{ID: "p-group", ResourceType: model.ResourceTypeGroup, ResourceID: "g1", Effect: model.PermissionEffectAllow},
	})
	// 创建资源组
	if err := db.Create(&model.ResourceGroup{ID: "g1", Name: "prod"}).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	// 创建主机（分组名匹配）
	if err := db.Create(&model.Host{ID: "h1", Name: "server1", Address: "10.0.0.1", GroupName: "prod"}).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	// 创建数据库实例（分组名匹配）
	if err := db.Create(&model.DatabaseInstance{ID: "db1", Name: "mysql-main", Protocol: "mysql", Address: "10.0.0.2", Port: 3306, GroupName: "prod"}).Error; err != nil {
		t.Fatalf("create db instance: %v", err)
	}
	// 创建数据库账号（属于该实例）
	if err := db.Create(&model.DatabaseAccount{ID: "dbacct1", InstanceID: "db1", UniqueName: "app_user", Username: "app", Status: "active"}).Error; err != nil {
		t.Fatalf("create db account: %v", err)
	}

	checker := NewChecker(db)
	// host 本身在 prod 组内
	assertPermission(t, checker, "u1", "session:connect", model.ResourceTypeHost, "h1", true)
	// database_instance 本身在 prod 组内
	assertPermission(t, checker, "u1", "session:connect", model.ResourceTypeDatabaseInstance, "db1", true)
	// database_account 所属实例在 prod 组内
	assertPermission(t, checker, "u1", "session:connect", model.ResourceTypeDatabaseAccount, "dbacct1", true)
}

func TestHasPermissionDenyOverridesAllow(t *testing.T) {
	db := newTestDB(t)
	seedRBAC(t, db, "u1", []model.Permission{
		{ID: "p-connect", Action: "session:connect", Effect: model.PermissionEffectAllow},
		{ID: "p-target-root", ResourceType: "host_account", ResourceID: "target-root", Effect: model.PermissionEffectAllow},
		{ID: "p-deny-target-root", Action: "session:connect", ResourceType: "host_account", ResourceID: "target-root", Effect: model.PermissionEffectDeny},
	})

	assertPermission(t, NewChecker(db), "u1", "session:connect", "host_account", "target-root", false)
}

func TestHasPermissionSupportsWildcardResourceScope(t *testing.T) {
	db := newTestDB(t)
	seedRBAC(t, db, "u1", []model.Permission{
		{ID: "p-all-actions", Action: "*", Effect: model.PermissionEffectAllow},
		{ID: "p-all-resources", Action: "*", ResourceType: "*", ResourceID: "*", Effect: model.PermissionEffectAllow},
	})

	checker := NewChecker(db)
	assertPermission(t, checker, "u1", "session:connect", "host_account", "target-root", true)
	assertPermission(t, checker, "u1", "db:audit:view", "database_proxy", "mysql-local", true)
	assertPermission(t, checker, "u1", "role:create", "", "", true)
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
