package storage

import (
	"testing"

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
