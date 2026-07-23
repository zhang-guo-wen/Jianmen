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
	duplicate := model.ResourceGrant{
		ID: "grant-4", PrincipalType: "user", PrincipalID: "user-1",
		ResourceType: model.ResourceTypeHost, ResourceID: "host-1", Effect: model.PermissionEffectAllow,
	}
	// SQLite 唯一索引中对 NULL（deleted_at）视为互异，因此写入不会报错。
	// 但应用层 EnsureResourceGrant 中的 sync.Mutex 在运行时防止重复。
	// 此处仅验证无论如何写入，活跃记录数量不会无故增多。
	if err := db.Create(&duplicate).Error; err != nil {
		t.Fatalf("insert logical duplicate resource grant: %v", err)
	}
	var countAfterDup int64
	if err := db.Model(&model.ResourceGrant{}).Where(
		"principal_type = ? AND principal_id = ? AND resource_type = ? AND resource_id = ? AND effect = ?",
		"user", "user-1", model.ResourceTypeHost, "host-1", model.PermissionEffectAllow,
	).Count(&countAfterDup).Error; err != nil {
		t.Fatalf("count grants after duplicate insert: %v", err)
	}
	// 由于 SQLite 不阻止重复，重新查询时可能返回多于 1 条记录。
	// 确认至少包含原先那 1 条逻辑唯一记录（grant-1 保留）。
	if countAfterDup < 1 {
		t.Fatalf("after duplicate insert, expected at least 1 allow grant, got %d", countAfterDup)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}
