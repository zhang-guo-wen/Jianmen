package storage

import (
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestSystemSettingMigrationCreatesSingletonAndRevisionSchema(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&SchemaMigration{}); err != nil {
		t.Fatalf("create schema migrations table: %v", err)
	}
	now := time.Now().UTC()
	for _, migration := range migrations {
		if migration.Version == systemSettingMigrationVersion {
			continue
		}
		if err := db.Create(&SchemaMigration{
			Version:   migration.Version,
			Name:      migration.Name,
			AppliedAt: now,
		}).Error; err != nil {
			t.Fatalf("record migration %s: %v", migration.Version, err)
		}
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate system settings: %v", err)
	}
	for _, table := range []any{
		&model.SystemSetting{},
		&model.SystemSettingRevision{},
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing table for %T", table)
		}
	}
	for _, column := range []string{
		"web_rdp_enabled",
		"web_rdp_connect_timeout_seconds",
		"web_rdp_allow_unrecorded",
		"recording_enabled",
		"recording_record_input",
		"recording_record_commands",
		"recording_retention_days",
		"recording_max_replay_bytes",
		"recording_cleanup_batch_size",
		"revision",
		"applied_revision",
		"applied_at",
		"updated_by_id",
		"updated_by_username",
	} {
		if !db.Migrator().HasColumn(&model.SystemSetting{}, column) {
			t.Fatalf("system setting column %s is missing", column)
		}
	}
	if db.Migrator().HasColumn(
		&model.SystemSetting{},
		"DatabaseMaxClientMessageBytes",
	) {
		t.Fatal("historical system setting migration installed a future database limit column")
	}
	for _, column := range []string{
		"revision",
		"snapshot_json",
		"changed_fields_json",
		"updated_by_id",
		"updated_by_username",
		"created_at",
	} {
		if !db.Migrator().HasColumn(&model.SystemSettingRevision{}, column) {
			t.Fatalf("system setting revision column %s is missing", column)
		}
	}
	if !db.Migrator().HasIndex(
		&model.SystemSettingRevision{},
		"idx_system_setting_revisions_revision",
	) {
		t.Fatal("system setting revision unique index is missing")
	}
	var count int64
	if err := db.Model(&SchemaMigration{}).
		Where("version = ?", systemSettingMigrationVersion).
		Count(&count).Error; err != nil {
		t.Fatalf("count system setting migration record: %v", err)
	}
	if count != 1 {
		t.Fatalf("system setting migration records = %d, want 1", count)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate system settings: %v", err)
	}
}
