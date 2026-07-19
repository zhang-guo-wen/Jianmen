package storage

import (
	"errors"
	"testing"
	"time"

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

func TestBootstrapMetadataDoesNotOverwritePersistedSuperAdministratorState(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	cfg := &config.Config{
		Users: []config.User{{ID: "u-admin", Username: "admin", SuperAdmin: true}},
	}
	if err := BootstrapMetadata(db, cfg); err != nil {
		t.Fatalf("initial bootstrap: %v", err)
	}

	cfg.Users[0].SuperAdmin = false
	if err := BootstrapMetadata(db, cfg); err != nil {
		t.Fatalf("repeat bootstrap: %v", err)
	}
	var persisted model.User
	if err := db.First(&persisted, "id = ?", "u-admin").Error; err != nil {
		t.Fatalf("find persisted administrator: %v", err)
	}
	if !persisted.IsSuperAdmin {
		t.Fatal("repeat bootstrap overwrote database super administrator state")
	}
}

func TestBootstrapMetadataRejectsUsersWithoutActiveSuperAdministratorAndRollsBack(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	err = BootstrapMetadata(db, &config.Config{
		Users: []config.User{{ID: "u-normal", Username: "normal"}},
	})
	if !errors.Is(err, ErrNoActiveSuperAdmin) {
		t.Fatalf("BootstrapMetadata error = %v, want ErrNoActiveSuperAdmin", err)
	}
	var userCount int64
	if err := db.Model(&model.User{}).Count(&userCount).Error; err != nil {
		t.Fatalf("count rolled back users: %v", err)
	}
	if userCount != 0 {
		t.Fatalf("user count after rejected bootstrap = %d, want 0", userCount)
	}
}

func TestBootstrapMetadataUsesExplicitConfigSuperAdministratorForDatabaseRecovery(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := db.Create(&model.User{
		ID: "u-recovery", Username: "recovery", Status: "active",
	}).Error; err != nil {
		t.Fatalf("create ordinary database user: %v", err)
	}

	if err := BootstrapMetadata(db, &config.Config{
		Users: []config.User{{ID: "u-recovery", Username: "recovery", SuperAdmin: true}},
	}); err != nil {
		t.Fatalf("recover super administrator: %v", err)
	}
	var recovered model.User
	if err := db.First(&recovered, "id = ?", "u-recovery").Error; err != nil {
		t.Fatalf("find recovered administrator: %v", err)
	}
	if !recovered.IsSuperAdmin {
		t.Fatal("explicit recovery seed was not persisted to the database")
	}
}

func TestBootstrapMetadataDoesNotLetConfigOverrideExistingDatabaseAuthority(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := db.Create(&model.User{
		ID: "u-authority", Username: "authority", Status: "active", IsSuperAdmin: true,
	}).Error; err != nil {
		t.Fatalf("create database administrator: %v", err)
	}

	if err := BootstrapMetadata(db, &config.Config{
		Users: []config.User{{ID: "u-config", Username: "config", SuperAdmin: true}},
	}); err != nil {
		t.Fatalf("bootstrap with existing database authority: %v", err)
	}
	var configured model.User
	if err := db.First(&configured, "id = ?", "u-config").Error; err != nil {
		t.Fatalf("find configured user: %v", err)
	}
	if configured.IsSuperAdmin {
		t.Fatal("config promoted a user while an active database super administrator existed")
	}
}

func TestBootstrapMetadataRejectsExpiredSuperAdministrator(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	expiredAt := time.Now().UTC().Add(-time.Minute)
	if err := db.Create(&model.User{
		ID: "u-expired", Username: "expired", Status: "active", IsSuperAdmin: true, ExpiresAt: &expiredAt,
	}).Error; err != nil {
		t.Fatalf("create expired administrator: %v", err)
	}

	err = BootstrapMetadata(db, &config.Config{})
	if !errors.Is(err, ErrNoActiveSuperAdmin) {
		t.Fatalf("BootstrapMetadata error = %v, want ErrNoActiveSuperAdmin", err)
	}
}
