package storage

import (
	"testing"

	"jianmen/internal/model"
)

func TestTemporaryAccessConnectionPasswordMigration(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var migration SchemaMigration
	if err := db.First(&migration, "version = ?", "202607180002").Error; err != nil {
		t.Fatalf("load temporary access lifecycle migration: %v", err)
	}
	for _, column := range []string{"temporary_account_id", "revoked_at"} {
		if !db.Migrator().HasColumn(&model.ConnectionPassword{}, column) {
			t.Fatalf("connection_passwords is missing %s", column)
		}
	}
}
