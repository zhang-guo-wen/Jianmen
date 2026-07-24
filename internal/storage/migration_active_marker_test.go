package storage

import (
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

func TestMigrateAuditFieldsUpgradesLegacySQLiteSchema(t *testing.T) {
	db := openActiveMarkerMigrationSQLite(t)

	if err := db.AutoMigrate(&SchemaMigration{}); err != nil {
		t.Fatalf("create schema migrations table: %v", err)
	}
	for _, migration := range migrations {
		if migration.Version == auditFieldsMigrationVersion {
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
	// Local development databases may already contain the three unpublished
	// audit migration records. The squashed migration must still run under its
	// new version and upgrade their physical schema.
	for _, version := range []string{"202607230001", "202607230002", "202607230003"} {
		if err := db.Create(&SchemaMigration{
			Version:   version,
			Name:      "superseded audit migration",
			AppliedAt: time.Now().UTC(),
		}).Error; err != nil {
			t.Fatalf("record superseded migration %s: %v", version, err)
		}
	}
	if err := db.AutoMigrate(
		&roleWithLegacyTimeMarker{},
		&permissionWithLegacyTimeMarker{},
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
	if !hasAppliedMigration(t, db, auditFieldsMigrationVersion) {
		t.Fatalf("migration %s was not recorded", auditFieldsMigrationVersion)
	}
	if db.Migrator().HasColumn("role_permissions", "active_marker") {
		t.Fatal("association table unexpectedly gained active_marker")
	}
	if db.Migrator().HasColumn("roles", "deleted_at") {
		t.Fatal("roles retained the legacy deleted_at column")
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
		"idx_roles_name_active",
		[]string{"name", "active_marker"},
	)
	if db.Migrator().HasIndex("roles", "idx_roles_name_deleted") {
		t.Fatal("legacy _deleted index remains")
	}

	activeMarker := loadActiveMarker(t, db, "roles", "active-role")
	if !activeMarker.Valid || activeMarker.Int64 != 1 {
		t.Fatalf("active role active_marker = %#v, want 1", activeMarker)
	}
	deletedMarker := loadActiveMarker(t, db, "roles", "deleted-role")
	if deletedMarker.Valid {
		t.Fatalf("deleted role active_marker = %#v, want NULL", deletedMarker)
	}

	duplicate := model.Role{ID: "duplicate-active-role", Name: "shared-name", Status: "active"}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("active duplicate role was accepted after index migration")
	}
	var triggerEventsBeforeUpdate int64
	if err := db.Table("role_update_events").
		Where("role_id = ?", "active-role").
		Count(&triggerEventsBeforeUpdate).Error; err != nil {
		t.Fatalf("load migration trigger events: %v", err)
	}
	if err := db.Exec(`UPDATE roles SET active_marker = NULL WHERE id = 'active-role'`).Error; err != nil {
		t.Fatalf("write NULL to migrated active_marker column: %v", err)
	}
	var triggerEvents int64
	if err := db.Table("role_update_events").
		Where("role_id = ?", "active-role").
		Count(&triggerEvents).Error; err != nil {
		t.Fatalf("load restored trigger events: %v", err)
	}
	if triggerEvents != triggerEventsBeforeUpdate+1 {
		t.Fatalf(
			"restored trigger events = %d, want %d",
			triggerEvents,
			triggerEventsBeforeUpdate+1,
		)
	}
	replacement := model.Role{ID: "replacement-role", Name: "shared-name", Status: "active"}
	if err := db.Create(&replacement).Error; err != nil {
		t.Fatalf("recreate role after tombstoning active row: %v", err)
	}
	replacementMarker := loadActiveMarker(t, db, "roles", replacement.ID)
	if !replacementMarker.Valid || replacementMarker.Int64 != 1 {
		t.Fatalf("replacement role active_marker = %#v, want 1", replacementMarker)
	}
	assertColumnNullable(t, db, "roles", "active_marker")
}

func TestMigrateAuditFieldsUpgradesLegacyIntegerMarker(t *testing.T) {
	db := openActiveMarkerMigrationSQLite(t)
	if err := db.AutoMigrate(&roleWithLegacyIntegerMarker{}); err != nil {
		t.Fatalf("create legacy integer marker table: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO roles (id, name, status, deleted_at) VALUES ('active-role', 'shared-name', 'active', 1)`,
		`INSERT INTO roles (id, name, status, deleted_at) VALUES ('removed-role', 'shared-name', 'disabled', NULL)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed legacy integer marker roles: %v", err)
		}
	}

	if err := migrateAuditFields(db); err != nil {
		t.Fatalf("migrate legacy integer marker: %v", err)
	}
	if db.Migrator().HasColumn("roles", "deleted_at") {
		t.Fatal("roles retained the legacy integer deleted_at column")
	}
	if marker := loadActiveMarker(t, db, "roles", "active-role"); !marker.Valid || marker.Int64 != 1 {
		t.Fatalf("active marker = %#v, want 1", marker)
	}
	if marker := loadActiveMarker(t, db, "roles", "removed-role"); marker.Valid {
		t.Fatalf("removed marker = %#v, want NULL", marker)
	}
}

func TestMigrateAuditFieldsConvertsRegisteredProvisioningOperations(t *testing.T) {
	db := openActiveMarkerMigrationSQLite(t)
	if err := db.AutoMigrate(
		&model.DatabaseInstance{},
		&model.DatabaseProvisioningOperation{},
	); err != nil {
		t.Fatalf("create provisioning tables: %v", err)
	}
	instance := model.DatabaseInstance{
		ID:       "instance-1",
		Name:     "primary",
		Protocol: "mysql",
		Address:  "127.0.0.1",
		Port:     3306,
		Status:   "active",
	}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create instance: %v", err)
	}
	for _, operation := range []struct {
		id       string
		username string
	}{
		{id: "operation-active", username: "active_user"},
		{id: "operation-removed", username: "removed_user"},
	} {
		if err := db.Exec(
			`INSERT INTO database_provisioning_operations
				(id, instance_id, admin_account_id, upstream_username, password, host, grants_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			operation.id,
			instance.ID,
			"admin-1",
			operation.username,
			"encrypted-placeholder",
			"127.0.0.1",
			"[]",
		).Error; err != nil {
			t.Fatalf("create operation %s: %v", operation.id, err)
		}
	}
	if err := db.Exec(
		`ALTER TABLE database_provisioning_operations
		 RENAME COLUMN active_marker TO deleted_at`,
	).Error; err != nil {
		t.Fatalf("rename provisioning marker to legacy column: %v", err)
	}
	if err := db.Exec(
		`UPDATE database_provisioning_operations
		 SET deleted_at = NULL
		 WHERE id = 'operation-removed'`,
	).Error; err != nil {
		t.Fatalf("tombstone legacy provisioning operation: %v", err)
	}

	if err := migrateAuditFields(db); err != nil {
		t.Fatalf("migrate provisioning marker: %v", err)
	}
	if db.Migrator().HasColumn("database_provisioning_operations", "deleted_at") {
		t.Fatal("provisioning operations retained deleted_at")
	}
	if marker := loadActiveMarker(
		t,
		db,
		"database_provisioning_operations",
		"operation-active",
	); !marker.Valid || marker.Int64 != 1 {
		t.Fatalf("active provisioning marker = %#v, want 1", marker)
	}
	if marker := loadActiveMarker(
		t,
		db,
		"database_provisioning_operations",
		"operation-removed",
	); marker.Valid {
		t.Fatalf("removed provisioning marker = %#v, want NULL", marker)
	}
	assertSQLiteIndexColumns(
		t,
		db,
		"idx_dpo_upstream_username_active",
		[]string{"upstream_username", "active_marker"},
	)
}

func TestAuditRefactorUsesSingleFinalMigration(t *testing.T) {
	var versions []string
	for _, migration := range migrations {
		if migration.Version == auditFieldsMigrationVersion ||
			migration.Version == "202607230001" ||
			migration.Version == "202607230002" ||
			migration.Version == "202607230003" {
			versions = append(versions, migration.Version)
		}
	}
	if !reflect.DeepEqual(versions, []string{auditFieldsMigrationVersion}) {
		t.Fatalf("audit refactor migration versions = %v, want [%s]", versions, auditFieldsMigrationVersion)
	}
}

func TestFreshMigrationCreatesOnlyFinalActiveMarkerSchema(t *testing.T) {
	db := openActiveMarkerMigrationSQLite(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate fresh database: %v", err)
	}
	for _, table := range []string{
		"users",
		"roles",
		"resource_grants",
		"database_provisioning_operations",
	} {
		if !db.Migrator().HasColumn(table, "active_marker") {
			t.Errorf("%s is missing active_marker", table)
		}
		if db.Migrator().HasColumn(table, "deleted_at") {
			t.Errorf("%s unexpectedly contains deleted_at", table)
		}
	}
}

func TestLegacyMarkerConversionSupportsConfiguredDialects(t *testing.T) {
	for _, test := range []struct {
		dialect string
		cast    string
	}{
		{dialect: "sqlite", cast: "TEXT"},
		{dialect: "postgres", cast: "TEXT"},
		{dialect: "mysql", cast: "CHAR"},
	} {
		expression, err := legacyDeletedAtConversionExpression(test.dialect)
		if err != nil {
			t.Fatalf("%s conversion expression: %v", test.dialect, err)
		}
		if !strings.Contains(expression, "AS "+test.cast) ||
			!strings.Contains(expression, "ELSE NULL") {
			t.Fatalf("%s conversion expression = %q", test.dialect, expression)
		}
	}
}

type roleWithLegacyTimeMarker struct {
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

func (roleWithLegacyTimeMarker) TableName() string {
	return "roles"
}

type roleWithLegacyIntegerMarker struct {
	ID        string `gorm:"primaryKey;size:64"`
	Name      string `gorm:"uniqueIndex:idx_roles_name_deleted,priority:1;size:128;not null"`
	Status    string `gorm:"size:32;not null;default:active"`
	DeletedAt *int   `gorm:"index;default:1;uniqueIndex:idx_roles_name_deleted,priority:2"`
}

func (roleWithLegacyIntegerMarker) TableName() string {
	return "roles"
}

type permissionWithLegacyTimeMarker struct {
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

func (permissionWithLegacyTimeMarker) TableName() string {
	return "permissions"
}

func TestTablesWithActiveMarkerAreDerivedFromFullAuditModels(t *testing.T) {
	db := openActiveMarkerMigrationSQLite(t)
	tables, err := tablesWithActiveMarker(db)
	if err != nil {
		t.Fatalf("derive active_marker tables: %v", err)
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
		"database_provisioning_operations",
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
	db := openActiveMarkerMigrationSQLite(t)
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
	db := openActiveMarkerMigrationSQLite(t)
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
		"idx_roles_name_active",
		[]string{"name", "active_marker"},
	)
}

func openActiveMarkerMigrationSQLite(t *testing.T) *gorm.DB {
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

func loadActiveMarker(
	t *testing.T,
	db *gorm.DB,
	table string,
	id string,
) sql.NullInt64 {
	t.Helper()
	var marker sql.NullInt64
	if err := db.Table(table).
		Select("active_marker").
		Where("id = ?", id).
		Scan(&marker).Error; err != nil {
		t.Fatalf("load %s %s active_marker: %v", table, id, err)
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
