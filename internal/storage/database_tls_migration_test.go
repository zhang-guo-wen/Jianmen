package storage

import (
	"testing"
	"time"
)

func TestDatabaseTLSMigrationUpgradesExistingInstances(t *testing.T) {
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
		group_name text,
		remark text,
		status text,
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create previous database instance schema: %v", err)
	}
	if err := db.Exec(`INSERT INTO database_instances
		(id, name, protocol, address, port, status)
		VALUES ('database-1', 'orders', 'mysql', 'db.internal', 3306, 'active')`).Error; err != nil {
		t.Fatalf("seed database instance: %v", err)
	}
	for _, migration := range migrations {
		if migration.Version == "202607180006" {
			continue
		}
		if err := db.Create(&SchemaMigration{
			Version:   migration.Version,
			Name:      migration.Name,
			AppliedAt: time.Now().UTC(),
		}).Error; err != nil {
			t.Fatalf("record previous migration %s: %v", migration.Version, err)
		}
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate existing database: %v", err)
	}
	var tlsMode string
	if err := db.Raw("SELECT tls_mode FROM database_instances WHERE id = ?", "database-1").Scan(&tlsMode).Error; err != nil {
		t.Fatalf("load migrated TLS mode: %v", err)
	}
	if tlsMode != "disable" {
		t.Fatalf("migrated TLS mode = %q, want disable", tlsMode)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}
