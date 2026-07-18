package storage

import (
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
)

func TestBootstrapMetadataPersistsOnlyExplicitConfigSuperAdministrator(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	cfg := &config.Config{
		Users: []config.User{
			{ID: "u-normal", Username: "normal"},
			{ID: "u-admin", Username: "admin", SuperAdmin: true},
		},
	}

	if err := BootstrapMetadata(db, cfg); err != nil {
		t.Fatalf("bootstrap metadata: %v", err)
	}

	var normal model.User
	if err := db.First(&normal, "id = ?", "u-normal").Error; err != nil {
		t.Fatalf("find normal user: %v", err)
	}
	if normal.IsSuperAdmin {
		t.Fatal("normal config user was implicitly promoted to super administrator")
	}

	var admin model.User
	if err := db.First(&admin, "id = ?", "u-admin").Error; err != nil {
		t.Fatalf("find super administrator: %v", err)
	}
	if !admin.IsSuperAdmin {
		t.Fatal("explicit config super administrator flag was not persisted")
	}
}

func TestMigrateAddsUserSuperAdministratorColumn(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !db.Migrator().HasColumn(&model.User{}, "is_super_admin") {
		t.Fatal("users.is_super_admin column is missing")
	}
	var applied SchemaMigration
	if err := db.First(&applied, "version = ?", "202607180001").Error; err != nil {
		t.Fatalf("find super administrator migration: %v", err)
	}
}
