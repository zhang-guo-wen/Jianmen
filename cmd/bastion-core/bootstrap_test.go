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

func TestInitializeMetadataRequiresEnabledDatabase(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, _, err := initializeMetadata(&config.Config{}, logger); err == nil {
		t.Fatal("expected disabled database error")
	}
}

func TestInitializeMetadataImportsLegacySuperAdministratorOnce(t *testing.T) {
	dataDir := t.TempDir()
	legacyPath := filepath.Join(dataDir, storage.LegacySuperAdminIDsFile)
	if err := os.WriteFile(legacyPath, []byte("legacy-admin\n"), 0o600); err != nil {
		t.Fatalf("write legacy super administrator file: %v", err)
	}
	cfg := &config.Config{
		Users: []config.User{{ID: "legacy-admin", Username: "legacy-admin"}},
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
		t.Fatalf("find legacy administrator: %v", err)
	}
	if !user.IsSuperAdmin {
		t.Fatal("legacy administrator was not imported")
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy source still exists: %v", err)
	}
}
