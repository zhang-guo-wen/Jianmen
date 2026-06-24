package storage

import (
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

func TestOpenAndAutoMigrateSQLite(t *testing.T) {
	db, err := Open(Config{
		Driver: DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	for _, m := range model.AllModels() {
		if !db.Migrator().HasTable(m) {
			t.Fatalf("expected table for %T", m)
		}
	}
}

func TestOpenRejectsMissingNetworkDSN(t *testing.T) {
	if _, err := Open(Config{Driver: DriverMySQL}); err == nil {
		t.Fatal("expected mysql without dsn to fail")
	}
	if _, err := Open(Config{Driver: DriverPostgres}); err == nil {
		t.Fatal("expected postgres without dsn to fail")
	}
}

func TestBootstrapMetadataSeedsUsersRolesAndAdminGrant(t *testing.T) {
	db, err := Open(Config{
		Driver: DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	cfg := &config.Config{
		Users: []config.User{
			{ID: "u-admin", Username: "admin"},
			{Username: "operator"},
		},
	}
	if err := BootstrapMetadata(db, cfg); err != nil {
		t.Fatalf("bootstrap metadata: %v", err)
	}

	var users []model.User
	if err := db.Order("username").Find(&users).Error; err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("users = %d, want 2: %#v", len(users), users)
	}

	var roles []model.Role
	if err := db.Find(&roles).Error; err != nil {
		t.Fatalf("list roles: %v", err)
	}
	if len(roles) < 4 {
		t.Fatalf("roles = %d, want at least 4", len(roles))
	}

	allowed, err := rbac.NewChecker(db).HasPermission("u-admin", "session:connect", "host_account", "target-local")
	if err != nil {
		t.Fatalf("check permission: %v", err)
	}
	if !allowed {
		t.Fatal("bootstrapped admin should have wildcard permission")
	}

	operatorAllowed, err := rbac.NewChecker(db).HasPermission("operator", "session:connect", "host_account", "target-local")
	if err != nil {
		t.Fatalf("check operator permission: %v", err)
	}
	if operatorAllowed {
		t.Fatal("non-first config user should not be granted builtin admin automatically")
	}

	if err := db.Create(&model.UserRole{
		ID:     "ur-operator-ssh",
		UserID: "operator",
		RoleID: builtinSSHOperatorRoleID,
	}).Error; err != nil {
		t.Fatalf("grant operator ssh role: %v", err)
	}
	operatorSSHAllowed, err := rbac.NewChecker(db).HasPermission("operator", "session:connect", model.ResourceTypeHostAccount, "target-local")
	if err != nil {
		t.Fatalf("check operator ssh permission: %v", err)
	}
	if !operatorSSHAllowed {
		t.Fatal("builtin ssh operator should grant session connect over all host accounts")
	}

	dbAllowed, err := rbac.NewChecker(db).HasPermission("operator", rbac.ActionDBConnect, model.ResourceTypeDatabaseAccount, "dbacct-mysql-local-app")
	if err != nil {
		t.Fatalf("check operator database permission before grant: %v", err)
	}
	if dbAllowed {
		t.Fatal("builtin db operator should not be granted automatically")
	}

	if err := db.Create(&model.UserRole{
		ID:     "ur-operator-db",
		UserID: "operator",
		RoleID: builtinDBOperatorRoleID,
	}).Error; err != nil {
		t.Fatalf("grant operator db role: %v", err)
	}
	operatorDBAllowed, err := rbac.NewChecker(db).HasPermission("operator", rbac.ActionDBConnect, model.ResourceTypeDatabaseAccount, "dbacct-mysql-local-app")
	if err != nil {
		t.Fatalf("check operator database permission after grant: %v", err)
	}
	if !operatorDBAllowed {
		t.Fatal("builtin db operator should grant db:connect over all database accounts")
	}
}
