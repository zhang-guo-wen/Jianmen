package storage

import (
	"testing"
	"time"
)

func TestDatabaseTLSDefaultMigrationPreservesExistingPolicies(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&SchemaMigration{}); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE database_instances (
		id text primary key,
		name text not null,
		protocol text not null,
		address text not null,
		port integer not null,
		tls_mode varchar(16) not null default 'verify-full',
		status text,
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create previous database instance schema: %v", err)
	}
	if err := db.Exec(`INSERT INTO database_instances
		(id, name, protocol, address, port, tls_mode, status)
		VALUES ('existing', 'existing', 'mysql', 'db.internal', 3306, 'verify-full', 'active')`).Error; err != nil {
		t.Fatalf("seed existing database instance: %v", err)
	}
	for _, migration := range migrations {
		if migration.Version == databaseTLSDefaultMigrationVersion {
			continue
		}
		if err := db.Create(&SchemaMigration{
			Version: migration.Version, Name: migration.Name, AppliedAt: time.Now().UTC(),
		}).Error; err != nil {
			t.Fatalf("record previous migration %s: %v", migration.Version, err)
		}
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate database TLS default: %v", err)
	}
	var existingMode string
	if err := db.Raw(
		"SELECT tls_mode FROM database_instances WHERE id = ?", "existing",
	).Scan(&existingMode).Error; err != nil {
		t.Fatalf("load existing TLS mode: %v", err)
	}
	if existingMode != "verify-full" {
		t.Fatalf("existing TLS mode = %q, want verify-full", existingMode)
	}
	if err := db.Exec(`INSERT INTO database_instances
		(id, name, protocol, address, port, status)
		VALUES ('new-default', 'new-default', 'mysql', 'db.internal', 3306, 'active')`).Error; err != nil {
		t.Fatalf("insert database instance with default TLS mode: %v", err)
	}
	var defaultMode string
	if err := db.Raw(
		"SELECT tls_mode FROM database_instances WHERE id = ?", "new-default",
	).Scan(&defaultMode).Error; err != nil {
		t.Fatalf("load default TLS mode: %v", err)
	}
	if defaultMode != "disable" {
		t.Fatalf("database TLS default = %q, want disable", defaultMode)
	}
	if err := db.Exec(`INSERT INTO database_instances
		(id, name, protocol, address, port, tls_mode, status)
		VALUES ('null-mode', 'null-mode', 'mysql', 'db.internal', 3306, NULL, 'active')`).Error; err == nil {
		t.Fatal("database accepted an explicit NULL TLS mode")
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migration: %v", err)
	}
}
