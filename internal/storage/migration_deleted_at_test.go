package storage

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

const deletedAtToIntMigrationVersion = "202607230003"

func TestMigrateDeletedAtToIntUpgradesLegacySQLiteSchema(t *testing.T) {
	db := openDeletedAtMigrationSQLite(t)

	if err := db.AutoMigrate(&SchemaMigration{}); err != nil {
		t.Fatalf("create schema migrations table: %v", err)
	}
	for _, migration := range migrations {
		if migration.Version == deletedAtToIntMigrationVersion {
			continue
		}
		if err := db.Create(&SchemaMigration{
			Version:   migration.Version,
			Name:      migration.Name,
			AppliedAt: time.Now().UTC(),
		}).Error; err != nil {
			t.Fatalf("record applied migration %s: %v", migration.Version, err)
		}
	}
	if !hasAppliedMigration(t, db, "202607230002") {
		t.Fatal("legacy fixture did not record audit field migration 202607230002")
	}

	if err := db.AutoMigrate(
		&roleBeforeDeletedAtInt{},
		&permissionBeforeDeletedAtInt{},
	); err != nil {
		t.Fatalf("create legacy audited tables: %v", err)
	}
	if err := db.Exec(`CREATE TABLE role_permissions (
		id text PRIMARY KEY,
		role_id text NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
		permission_id text NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
		created_at datetime
	)`).Error; err != nil {
		t.Fatalf("create legacy role permissions table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE role_update_events (role_id text NOT NULL)`).Error; err != nil {
		t.Fatalf("create role update event table: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER trg_roles_update
		AFTER UPDATE ON roles
		BEGIN
			INSERT INTO role_update_events (role_id) VALUES (NEW.id);
		END`).Error; err != nil {
		t.Fatalf("create legacy role trigger: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO roles (id, name) VALUES ('parent-role', 'parent')`,
		`INSERT INTO roles (id, name) VALUES ('active-role', 'shared-name')`,
		`INSERT INTO roles (id, name, deleted_at) VALUES ('deleted-role', 'shared-name', '2026-07-23 12:34:56')`,
		`INSERT INTO permissions (id, action, effect) VALUES ('parent-permission', 'host:view', 'allow')`,
		`INSERT INTO role_permissions (id, role_id, permission_id) VALUES ('rp-1', 'parent-role', 'parent-permission')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed legacy audited schema: %v", err)
		}
	}
	if err := db.Migrator().DropIndex("roles", "idx_roles_name_deleted"); err != nil {
		t.Fatalf("drop legacy role index: %v", err)
	}
	if err := db.Exec(`CREATE INDEX idx_roles_name_deleted ON roles (name)`).Error; err != nil {
		t.Fatalf("create malformed same-name role index: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate legacy SQLite deleted_at: %v", err)
	}
	if !hasAppliedMigration(t, db, deletedAtToIntMigrationVersion) {
		t.Fatalf("migration %s was not recorded", deletedAtToIntMigrationVersion)
	}
	if db.Migrator().HasColumn("role_permissions", "deleted_at") {
		t.Fatal("association table unexpectedly gained deleted_at")
	}
	var associationCount int64
	if err := db.Table("role_permissions").Where("id = ?", "rp-1").Count(&associationCount).Error; err != nil {
		t.Fatalf("load role permission after parent rebuild: %v", err)
	}
	if associationCount != 1 {
		t.Fatalf("role permission count after parent rebuild = %d, want 1", associationCount)
	}
	assertSQLiteForeignKeysEnabled(t, db)
	assertNoSQLiteForeignKeyViolations(t, db)
	assertSQLiteIndexColumns(
		t,
		db,
		"idx_roles_name_deleted",
		[]string{"name", "deleted_at"},
	)

	activeMarker := loadDeletedAtMarker(t, db, "roles", "active-role")
	if !activeMarker.Valid || activeMarker.Int64 != 1 {
		t.Fatalf("active role deleted_at = %#v, want 1", activeMarker)
	}
	deletedMarker := loadDeletedAtMarker(t, db, "roles", "deleted-role")
	if deletedMarker.Valid {
		t.Fatalf("deleted role deleted_at = %#v, want NULL", deletedMarker)
	}

	duplicate := model.Role{ID: "duplicate-active-role", Name: "shared-name", Status: "active"}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("active duplicate role was accepted after index migration")
	}
	if err := db.Exec(`UPDATE roles SET deleted_at = NULL WHERE id = 'active-role'`).Error; err != nil {
		t.Fatalf("write NULL to migrated deleted_at column: %v", err)
	}
	var triggerEvents int64
	if err := db.Table("role_update_events").
		Where("role_id = ?", "active-role").
		Count(&triggerEvents).Error; err != nil {
		t.Fatalf("load restored trigger events: %v", err)
	}
	if triggerEvents != 1 {
		t.Fatalf("restored trigger events = %d, want 1", triggerEvents)
	}
	replacement := model.Role{ID: "replacement-role", Name: "shared-name", Status: "active"}
	if err := db.Create(&replacement).Error; err != nil {
		t.Fatalf("recreate role after tombstoning active row: %v", err)
	}
	replacementMarker := loadDeletedAtMarker(t, db, "roles", replacement.ID)
	if !replacementMarker.Valid || replacementMarker.Int64 != 1 {
		t.Fatalf("replacement role deleted_at = %#v, want 1", replacementMarker)
	}
	assertColumnNullable(t, db, "roles", "deleted_at")
}

type roleBeforeDeletedAtInt struct {
	ID          string `gorm:"primaryKey;size:64"`
	Name        string `gorm:"uniqueIndex:idx_roles_name_deleted,priority:1;size:128;not null"`
	Description string `gorm:"type:text"`
	Builtin     bool
	Status      string `gorm:"size:32;not null;default:active"`
	CreatedBy   string `gorm:"size:64;not null;default:''"`
	UpdatedBy   string `gorm:"size:64;not null;default:''"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time `gorm:"index;not null;default:'0001-01-01 00:00:00';uniqueIndex:idx_roles_name_deleted,priority:2"`
}

func (roleBeforeDeletedAtInt) TableName() string {
	return "roles"
}

type permissionBeforeDeletedAtInt struct {
	ID           string `gorm:"primaryKey;size:64"`
	Name         string `gorm:"size:128"`
	Action       string `gorm:"uniqueIndex:idx_permissions_logic_deleted,priority:1;size:128"`
	ResourceType string `gorm:"uniqueIndex:idx_permissions_logic_deleted,priority:2;size:64"`
	ResourceID   string `gorm:"uniqueIndex:idx_permissions_logic_deleted,priority:3;size:64"`
	Effect       string `gorm:"uniqueIndex:idx_permissions_logic_deleted,priority:4;size:16;not null;default:allow"`
	Description  string `gorm:"type:text"`
	CreatedBy    string `gorm:"size:64;not null;default:''"`
	UpdatedBy    string `gorm:"size:64;not null;default:''"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time `gorm:"index;not null;default:'0001-01-01 00:00:00';uniqueIndex:idx_permissions_logic_deleted,priority:5"`
}

func (permissionBeforeDeletedAtInt) TableName() string {
	return "permissions"
}

func TestTablesWithDeletedAtAreDerivedFromFullAuditModels(t *testing.T) {
	db := openDeletedAtMigrationSQLite(t)
	tables, err := tablesWithDeletedAt(db)
	if err != nil {
		t.Fatalf("derive deleted_at tables: %v", err)
	}
	got := make(map[string]bool, len(tables))
	for _, table := range tables {
		got[table] = true
	}
	for _, table := range []string{
		"user_public_keys",
		"sessions",
		"temporary_credentials",
		"system_initializations",
		"resource_sequences",
	} {
		if !got[table] {
			t.Errorf("audited table %s is missing", table)
		}
	}
	for _, table := range []string{
		"role_permissions",
		"user_roles",
		"user_group_members",
	} {
		if got[table] {
			t.Errorf("association table %s unexpectedly included", table)
		}
	}
}

func TestSQLiteMigrationRollsBackWhenVersionRecordFails(t *testing.T) {
	db := openDeletedAtMigrationSQLite(t)
	if err := db.AutoMigrate(&SchemaMigration{}); err != nil {
		t.Fatalf("create schema migrations table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE rollback_probe (
		id integer PRIMARY KEY,
		value text NOT NULL
	)`).Error; err != nil {
		t.Fatalf("create rollback probe: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_test_migration_record
		BEFORE INSERT ON schema_migrations
		WHEN NEW.version = 'rollback-test'
		BEGIN
			SELECT RAISE(ABORT, 'injected migration record failure');
		END`).Error; err != nil {
		t.Fatalf("create migration record failure trigger: %v", err)
	}

	migration := Migration{
		Version:                  "rollback-test",
		Name:                     "rollback test",
		SQLiteDisableForeignKeys: true,
		Run: func(tx *gorm.DB) error {
			if err := tx.Exec(
				`INSERT INTO rollback_probe (id, value) VALUES (1, 'written')`,
			).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE TABLE rollback_created (id integer PRIMARY KEY)`).Error
		},
	}
	if err := runSQLiteMigrationWithForeignKeysDisabled(db, migration); err == nil {
		t.Fatal("migration unexpectedly succeeded when version record was rejected")
	}

	var probeCount int64
	if err := db.Table("rollback_probe").Count(&probeCount).Error; err != nil {
		t.Fatalf("count rollback probe rows: %v", err)
	}
	if probeCount != 0 {
		t.Fatalf("rollback probe rows = %d, want 0", probeCount)
	}
	if db.Migrator().HasTable("rollback_created") {
		t.Fatal("migration DDL was not rolled back")
	}
	if hasAppliedMigration(t, db, migration.Version) {
		t.Fatal("failed migration was recorded as applied")
	}
	assertSQLiteForeignKeysEnabled(t, db)
}

func TestMigrateAuditUniqueIndexesDropsParallelBusinessOnlyIndex(t *testing.T) {
	db := openDeletedAtMigrationSQLite(t)
	if err := db.AutoMigrate(&model.Role{}); err != nil {
		t.Fatalf("create role table: %v", err)
	}
	const legacyIndex = "legacy_roles_name_unique"
	if err := db.Exec(
		`CREATE UNIQUE INDEX ` + legacyIndex + ` ON roles (name)`,
	).Error; err != nil {
		t.Fatalf("create parallel business-only index: %v", err)
	}

	if err := MigrateAuditUniqueIndexes(db); err != nil {
		t.Fatalf("migrate audit unique indexes: %v", err)
	}

	if db.Migrator().HasIndex("roles", legacyIndex) {
		t.Fatalf("parallel business-only index %s still exists", legacyIndex)
	}
	assertSQLiteIndexColumns(
		t,
		db,
		"idx_roles_name_deleted",
		[]string{"name", "deleted_at"},
	)
}

func openDeletedAtMigrationSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	return db
}

func hasAppliedMigration(t *testing.T, db *gorm.DB, version string) bool {
	t.Helper()
	var count int64
	if err := db.Model(&SchemaMigration{}).Where("version = ?", version).Count(&count).Error; err != nil {
		t.Fatalf("load migration %s: %v", version, err)
	}
	return count == 1
}

func assertColumnNullable(t *testing.T, db *gorm.DB, table, name string) {
	t.Helper()
	var columns []struct {
		Name    string `gorm:"column:name"`
		NotNull int    `gorm:"column:notnull"`
	}
	if err := db.Raw("PRAGMA table_info('" + table + "')").Scan(&columns).Error; err != nil {
		t.Fatalf("inspect SQLite %s columns: %v", table, err)
	}
	for _, column := range columns {
		if column.Name != name {
			continue
		}
		if column.NotNull != 0 {
			t.Fatalf("%s.%s remains NOT NULL", table, name)
		}
		return
	}
	t.Fatalf("%s has no %s column", table, name)
}

func loadDeletedAtMarker(
	t *testing.T,
	db *gorm.DB,
	table string,
	id string,
) sql.NullInt64 {
	t.Helper()
	var marker sql.NullInt64
	if err := db.Table(table).
		Select("deleted_at").
		Where("id = ?", id).
		Scan(&marker).Error; err != nil {
		t.Fatalf("load %s %s deleted_at: %v", table, id, err)
	}
	return marker
}

func assertSQLiteForeignKeysEnabled(t *testing.T, db *gorm.DB) {
	t.Helper()
	var enabled int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&enabled).Error; err != nil {
		t.Fatalf("read SQLite foreign key state: %v", err)
	}
	if enabled != 1 {
		t.Fatalf("SQLite foreign_keys = %d, want 1", enabled)
	}
}

func assertNoSQLiteForeignKeyViolations(t *testing.T, db *gorm.DB) {
	t.Helper()
	var violations []struct {
		Table  string `gorm:"column:table"`
		RowID  int64  `gorm:"column:rowid"`
		Parent string `gorm:"column:parent"`
		FKID   int    `gorm:"column:fkid"`
	}
	if err := db.Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil {
		t.Fatalf("check SQLite foreign keys: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("SQLite foreign key violations after migration: %#v", violations)
	}
}

func assertSQLiteIndexColumns(
	t *testing.T,
	db *gorm.DB,
	index string,
	want []string,
) {
	t.Helper()
	var rows []struct {
		Sequence int    `gorm:"column:seqno"`
		Name     string `gorm:"column:name"`
	}
	if err := db.Raw("PRAGMA index_info('" + index + "')").Scan(&rows).Error; err != nil {
		t.Fatalf("inspect SQLite index %s: %v", index, err)
	}
	got := make([]string, len(rows))
	for i, row := range rows {
		got[i] = row.Name
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SQLite index %s columns = %v, want %v", index, got, want)
	}
}
