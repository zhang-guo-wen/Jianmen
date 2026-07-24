package storage

import (
	"testing"
	"time"

	"jianmen/internal/model"
)

const resourceGrantLogicalUniquenessMigrationVersion = "202607190001"

func TestResourceGrantLogicalUniquenessMigrationDeduplicatesExistingGrants(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&SchemaMigration{}); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE resource_grants (
		id text primary key,
		principal_type text not null,
		principal_id text not null,
		resource_type text not null,
		resource_id text not null,
		effect text not null default 'allow',
		expires_at datetime,
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create previous resource grant schema: %v", err)
	}
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO resource_grants
		(id, principal_type, principal_id, resource_type, resource_id, effect, expires_at, created_at)
		VALUES
		('grant-1', 'user', 'user-1', 'host', 'host-1', 'allow', ?, ?),
		('grant-2', 'user', 'user-1', 'host', 'host-1', 'allow', NULL, ?),
		('grant-3', 'user', 'user-1', 'host', 'host-1', 'deny', NULL, ?)`,
		now.Add(time.Hour), now, now.Add(time.Minute), now.Add(2*time.Minute)).Error; err != nil {
		t.Fatalf("seed resource grants: %v", err)
	}
	for _, migration := range migrations {
		if migration.Version == resourceGrantLogicalUniquenessMigrationVersion {
			continue
		}
		if err := db.Create(&SchemaMigration{
			Version: migration.Version, Name: migration.Name, AppliedAt: now,
		}).Error; err != nil {
			t.Fatalf("record previous migration %s: %v", migration.Version, err)
		}
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate existing database: %v", err)
	}
	if !db.Migrator().HasIndex(&model.ResourceGrant{}, "uidx_resource_grants_logic") {
		t.Fatal("resource grant logical unique index is missing")
	}
	var allowGrants []model.ResourceGrant
	if err := db.Where(
		"principal_type = ? AND principal_id = ? AND resource_type = ? AND resource_id = ? AND effect = ?",
		"user", "user-1", model.ResourceTypeHost, "host-1", model.PermissionEffectAllow,
	).Find(&allowGrants).Error; err != nil {
		t.Fatalf("load migrated grants: %v", err)
	}
	if len(allowGrants) != 1 || allowGrants[0].ExpiresAt != nil {
		t.Fatalf("migrated allow grants = %#v, want one permanent grant", allowGrants)
	}
	duplicate := resourceGrantLogicalUniquenessSchema{
		ID: "grant-4", PrincipalType: "user", PrincipalID: "user-1",
		ResourceType: model.ResourceTypeHost, ResourceID: "host-1", Effect: model.PermissionEffectAllow,
	}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("logical duplicate resource grant was accepted")
	}
	var countAfterDup int64
	if err := db.Model(&model.ResourceGrant{}).Where(
		"principal_type = ? AND principal_id = ? AND resource_type = ? AND resource_id = ? AND effect = ?",
		"user", "user-1", model.ResourceTypeHost, "host-1", model.PermissionEffectAllow,
	).Count(&countAfterDup).Error; err != nil {
		t.Fatalf("count grants after duplicate insert: %v", err)
	}
	if countAfterDup != 1 {
		t.Fatalf("allow grant count after rejected duplicate = %d, want 1", countAfterDup)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}
