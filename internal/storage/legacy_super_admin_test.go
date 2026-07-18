package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"jianmen/internal/model"
)

func TestImportLegacySuperAdministratorIDsPersistsAndRenamesSource(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	users := []model.User{
		{ID: "u-admin", Username: "admin", Status: "active"},
		{ID: "u-normal", Username: "normal", Status: "active"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	dataDir := t.TempDir()
	source := filepath.Join(dataDir, LegacySuperAdminIDsFile)
	if err := os.WriteFile(source, []byte("\n u-admin \nmissing\n"), 0o600); err != nil {
		t.Fatalf("write legacy ids: %v", err)
	}

	if err := ImportLegacySuperAdminIDs(context.Background(), db, dataDir); err != nil {
		t.Fatalf("import legacy super administrators: %v", err)
	}

	var admin model.User
	if err := db.First(&admin, "id = ?", "u-admin").Error; err != nil {
		t.Fatalf("find imported administrator: %v", err)
	}
	if !admin.IsSuperAdmin {
		t.Fatal("legacy administrator was not persisted")
	}
	var normal model.User
	if err := db.First(&normal, "id = ?", "u-normal").Error; err != nil {
		t.Fatalf("find normal user: %v", err)
	}
	if normal.IsSuperAdmin {
		t.Fatal("unlisted user was promoted")
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("legacy source still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, LegacySuperAdminIDsImportedFile)); err != nil {
		t.Fatalf("imported marker file is missing: %v", err)
	}
}

func TestImportLegacySuperAdministratorIDsIsNoopWithoutSource(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := ImportLegacySuperAdminIDs(context.Background(), db, t.TempDir()); err != nil {
		t.Fatalf("import without source: %v", err)
	}
}
