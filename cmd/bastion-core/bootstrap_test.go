package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestInitializeMetadataBootstrapsSQLite(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &config.Config{}
	cfg.Database.Enabled = true
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(dataDir, "metadata.db")
	cfg.Database.AutoMigrate = true
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	db, gotDataDir, cleanup, err := initializeMetadata(cfg, logger)
	if err != nil {
		t.Fatalf("initialize metadata: %v", err)
	}
	t.Cleanup(cleanup)
	if gotDataDir != dataDir {
		t.Fatalf("data dir = %q, want %q", gotDataDir, dataDir)
	}
	if !db.Migrator().HasTable(&model.User{}) {
		t.Fatal("users table was not migrated")
	}
}

func TestInitializeMetadataAutoMigrateFlagStillRemovesLegacyAITokenSecrets(t *testing.T) {
	dataDir := t.TempDir()
	dsn := filepath.Join(dataDir, "metadata.db")
	legacyDB, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: dsn})
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	if err := legacyDB.Exec(`CREATE TABLE ai_access_tokens (
		id text primary key,
		user_id text,
		name text,
		access_token_hash text,
		refresh_token_hash text,
		access_token text,
		refresh_token text,
		access_expires_at datetime,
		refresh_expires_at datetime,
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create legacy AI token table: %v", err)
	}
	sqlDB, err := legacyDB.DB()
	if err != nil {
		t.Fatalf("legacy sql database: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	cfg := &config.Config{}
	cfg.Database.Enabled = true
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = dsn
	cfg.Database.AutoMigrate = true
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	db, _, cleanup, err := initializeMetadata(cfg, logger)
	if err != nil {
		t.Fatalf("initialize metadata: %v", err)
	}
	t.Cleanup(cleanup)
	for _, column := range []string{"access_token", "refresh_token"} {
		if db.Migrator().HasColumn("ai_access_tokens", column) {
			t.Fatalf("legacy reversible AI secret column %q still exists", column)
		}
	}
}

func TestInitializeMetadataRequiresEnabledDatabase(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, _, err := initializeMetadata(&config.Config{}, logger); err == nil {
		t.Fatal("expected disabled database error")
	}
}

func TestInitializeMetadataDoesNotReadOrRenameLegacySuperAdministratorFile(t *testing.T) {
	dataDir := t.TempDir()
	legacyPath := filepath.Join(dataDir, ".super_admin_ids")
	importedPath := filepath.Join(dataDir, ".super_admin_ids.imported")
	if err := os.WriteFile(legacyPath, []byte("legacy-admin\n"), 0o600); err != nil {
		t.Fatalf("write legacy super administrator file: %v", err)
	}
	cfg := &config.Config{
		Users: []config.User{
			{ID: "database-admin", Username: "database-admin", SuperAdmin: true},
			{ID: "legacy-admin", Username: "legacy-admin"},
		},
	}
	cfg.Database.Enabled = true
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(dataDir, "metadata.db")
	cfg.Database.AutoMigrate = true
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	db, _, cleanup, err := initializeMetadata(cfg, logger)
	if err != nil {
		t.Fatalf("initialize metadata: %v", err)
	}
	t.Cleanup(cleanup)

	var user model.User
	if err := db.First(&user, "id = ?", "legacy-admin").Error; err != nil {
		t.Fatalf("find configured user: %v", err)
	}
	if user.IsSuperAdmin {
		t.Fatal("legacy file promoted a database user to super administrator")
	}
	raw, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("legacy source was removed or renamed: %v", err)
	}
	if string(raw) != "legacy-admin\n" {
		t.Fatalf("legacy source was modified: %q", raw)
	}
	if _, err := os.Stat(importedPath); !os.IsNotExist(err) {
		t.Fatalf("legacy imported marker was created: %v", err)
	}
}
