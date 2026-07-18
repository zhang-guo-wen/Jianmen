package storage

import (
	"testing"

	"jianmen/internal/model"
)

func TestMigrateCreatesAtomicSystemInitializationGuard(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !db.Migrator().HasTable(&model.SystemInitialization{}) {
		t.Fatal("system initialization guard table is missing")
	}

	var migration SchemaMigration
	if err := db.First(&migration, "version = ?", "202607180003").Error; err != nil {
		t.Fatalf("find system initialization migration: %v", err)
	}
}
