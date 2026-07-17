package main

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
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
