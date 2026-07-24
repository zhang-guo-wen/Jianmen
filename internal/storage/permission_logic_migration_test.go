package storage

import (
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestPermissionLogicalUniquenessMigrationUpgradesExistingSchema(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&SchemaMigration{}); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE permissions (
		id text primary key,
		name text,
		action text,
		resource_type text,
		resource_id text,
		effect text not null default 'allow',
		description text,
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create previous permissions schema: %v", err)
	}
	if err := db.Exec(`INSERT INTO permissions
		(id, name, action, resource_type, resource_id, effect)
		VALUES ('permission-1', 'View hosts', 'host:view', '', '', 'allow')`).Error; err != nil {
		t.Fatalf("seed permission: %v", err)
	}

	for _, migration := range migrations {
		if migration.Version == "202607180007" {
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

	if db.Migrator().HasIndex(&model.Permission{}, "idx_permissions_logic") {
		t.Fatal("permission logical unique index unexpectedly existed before migration")
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate existing database: %v", err)
	}
	if !db.Migrator().HasIndex(&model.Permission{}, "idx_permissions_logic") {
		t.Fatal("permission logical unique index is missing after migration")
	}

	duplicate := permissionLogicalUniquenessSchema{
		ID:           "permission-2",
		Action:       "host:view",
		ResourceType: "",
		ResourceID:   "",
		Effect:       model.PermissionEffectAllow,
	}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("logical duplicate permission was accepted after migration")
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}
