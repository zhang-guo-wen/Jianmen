package storage

import (
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
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

func TestBootstrapMetadataSeedsUsersOnly(t *testing.T) {
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

	// 去掉预置角色后，bootstrap 不应创建任何角色
	var roles []model.Role
	if err := db.Find(&roles).Error; err != nil {
		t.Fatalf("list roles: %v", err)
	}
	if len(roles) != 0 {
		t.Fatalf("roles = %d, want 0 — no builtin roles should exist", len(roles))
	}

	// 去掉预置权限后，bootstrap 不应创建任何权限
	var permissions []model.Permission
	if err := db.Find(&permissions).Error; err != nil {
		t.Fatalf("list permissions: %v", err)
	}
	if len(permissions) != 0 {
		t.Fatalf("permissions = %d, want 0 — no builtin permissions should exist", len(permissions))
	}
}
