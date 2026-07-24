package storage

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"jianmen/internal/model"
)

const tenMiB = 10 * 1024 * 1024

func TestAuditDBQueryLargePayloadMigrationPreservesRowsAndStoresTenMiB(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&SchemaMigration{},
		&auditDBQueryBeforeLargePayload{},
		&systemSettingBeforeDatabaseClientMessageLimit{},
	); err != nil {
		t.Fatalf("create legacy database audit schema: %v", err)
	}

	now := time.Now().UTC()
	legacy := auditDBQueryBeforeLargePayload{
		ID:             "legacy-query",
		AuditSessionID: "legacy-session",
		Timestamp:      now,
		SQLText:        "SELECT 'preserve me'",
		QueryKind:      "query",
		DurationMs:     12,
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatalf("seed legacy database audit query: %v", err)
	}
	legacySettings := systemSettingBeforeDatabaseClientMessageLimit{
		ID:                          model.SystemSettingSingletonID,
		WebRDPConnectTimeoutSeconds: 15,
		RecordingEnabled:            true,
		RecordingRecordCommands:     true,
		RecordingRetentionDays:      30,
		RecordingMaxReplayBytes:     1024,
		RecordingCleanupBatchSize:   100,
		Revision:                    3,
		AppliedRevision:             2,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	}
	if err := db.Create(&legacySettings).Error; err != nil {
		t.Fatalf("seed legacy system settings: %v", err)
	}
	for _, migration := range migrations {
		if migration.Version == auditDBQueryLargePayloadMigrationVersion {
			continue
		}
		if err := db.Create(&SchemaMigration{
			Version:   migration.Version,
			Name:      migration.Name,
			AppliedAt: now,
		}).Error; err != nil {
			t.Fatalf("record previous migration %s: %v", migration.Version, err)
		}
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate database audit query payload storage: %v", err)
	}
	if !db.Migrator().HasIndex(
		&auditDBQueryLargePayloadSchema{},
		"idx_audit_db_queries_session_timestamp_id",
	) {
		t.Fatal("database audit query pagination index was not created")
	}

	var preserved auditDBQueryLargePayloadSchema
	if err := db.First(&preserved, "id = ?", legacy.ID).Error; err != nil {
		t.Fatalf("load preserved database audit query: %v", err)
	}
	if preserved.SQLText != legacy.SQLText ||
		preserved.QueryKind != legacy.QueryKind ||
		preserved.DurationMs != legacy.DurationMs {
		t.Fatalf("legacy database audit query changed during migration: %#v", preserved)
	}
	var migratedSettings model.SystemSetting
	if err := db.First(
		&migratedSettings,
		"id = ?",
		model.SystemSettingSingletonID,
	).Error; err != nil {
		t.Fatalf("load migrated system settings: %v", err)
	}
	if migratedSettings.DatabaseMaxClientMessageBytes !=
		defaultDatabaseClientMessageBytes {
		t.Fatalf(
			"database client message limit = %d, want %d",
			migratedSettings.DatabaseMaxClientMessageBytes,
			defaultDatabaseClientMessageBytes,
		)
	}
	if migratedSettings.Revision != legacySettings.Revision ||
		migratedSettings.RecordingRetentionDays !=
			legacySettings.RecordingRetentionDays {
		t.Fatalf("legacy system settings changed during migration: %#v", migratedSettings)
	}

	payload := strings.Repeat("x", tenMiB)
	large := auditDBQueryLargePayloadSchema{
		ID:               "large-query",
		AuditSessionID:   "large-session",
		Timestamp:        now.Add(time.Second),
		SQLText:          payload,
		OriginalSQLBytes: int64(len(payload)),
		SQLTruncated:     true,
		QueryKind:        "query",
	}
	if err := db.Create(&large).Error; err != nil {
		t.Fatalf("store 10 MiB database audit query: %v", err)
	}
	var loaded auditDBQueryLargePayloadSchema
	if err := db.First(&loaded, "id = ?", large.ID).Error; err != nil {
		t.Fatalf("load 10 MiB database audit query: %v", err)
	}
	if loaded.SQLText != payload {
		t.Fatalf("large database audit query was not preserved: got %d bytes, want %d", len(loaded.SQLText), len(payload))
	}
	if loaded.OriginalSQLBytes != int64(len(payload)) || !loaded.SQLTruncated {
		t.Fatalf("large database audit query metadata changed: %#v", loaded)
	}

	var migrationCount int64
	if err := db.Model(&SchemaMigration{}).
		Where("version = ?", auditDBQueryLargePayloadMigrationVersion).
		Count(&migrationCount).Error; err != nil {
		t.Fatalf("count database audit query payload migration records: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("database audit query payload migration records = %d, want 1", migrationCount)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate database audit query payload storage a second time: %v", err)
	}
	if err := db.Transaction(migrateAuditDBQueryLargePayload); err != nil {
		t.Fatalf("run database audit query payload migration directly a second time: %v", err)
	}
}

func TestAuditDBQuerySQLTextUsesLargeCrossDialectTypes(t *testing.T) {
	parsed, err := schema.Parse(
		&model.AuditDBQuery{},
		&sync.Map{},
		schema.NamingStrategy{},
	)
	if err != nil {
		t.Fatalf("parse database audit query schema: %v", err)
	}
	field := parsed.LookUpField("SQLText")
	if field == nil {
		t.Fatal("database audit query SQLText field is missing")
	}
	tests := []struct {
		name      string
		dialector gorm.Dialector
		wantType  string
	}{
		{
			name:      "mysql",
			dialector: mysql.New(mysql.Config{}),
			wantType:  "longtext",
		},
		{
			name:      "postgres",
			dialector: postgres.New(postgres.Config{}),
			wantType:  "text",
		},
		{
			name:      "sqlite",
			dialector: sqlite.Open(":memory:"),
			wantType:  "text",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := strings.ToLower(tt.dialector.DataTypeOf(field)); got != tt.wantType {
				t.Fatalf("database audit query SQL type = %q, want %q", got, tt.wantType)
			}
		})
	}
}
