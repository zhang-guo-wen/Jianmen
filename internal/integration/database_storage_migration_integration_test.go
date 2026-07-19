//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"

	"jianmen/internal/crypto"
	"jianmen/internal/model"
	"jianmen/internal/storage"
	"jianmen/internal/store"
)

const (
	databaseInstanceTLSMigrationVersion       = "202607180006"
	databaseAccountUniquenessMigrationVersion = "202607180008"
	databaseProvisioningSagaMigrationVersion  = "202607180009"
	auditRetentionCleanupMigrationVersion     = "202607190002"
	webRDPAuditMigrationVersion               = "202607190003"
	auditSessionLeaseMigrationVersion         = "202607190004"
	systemSettingMigrationVersion             = "202607190005"
	auditDBQueryLargePayloadMigrationVersion  = "202607190006"
	databaseGatewayModeMigrationVersion       = "202607190007"
	databaseTLSDefaultMigrationVersion        = "202607190008"
)

var currentStorageMigrationVersions = []string{
	"202606290001",
	"202606290002",
	"202606290003",
	"202606290004",
	"202606290005",
	"202607130001",
	"202607160001",
	"202607160002",
	"202607170001",
	"202607170002",
	"202607180001",
	"202607180002",
	"202607180003",
	"202607180004",
	"202607180005",
	"202607180006",
	"202607180007",
	"202607180008",
	"202607180009",
	"202607190001",
	"202607190002",
	"202607190003",
	"202607190004",
	systemSettingMigrationVersion,
	auditDBQueryLargePayloadMigrationVersion,
	databaseGatewayModeMigrationVersion,
	databaseTLSDefaultMigrationVersion,
}

type metadataDatabaseCase struct {
	name       string
	driver     storage.Driver
	container  func(string) []string
	port       string
	connection func(string) string
}

type legacyDatabaseInstanceRow struct {
	ID        string
	Name      string
	Protocol  string
	Address   string
	Port      int
	GroupName string
	Remark    string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type legacyDatabaseAccountRow struct {
	ID          string
	InstanceID  string
	UniqueName  string
	Username    string
	Password    string
	PasswordRaw string
	GroupName   string
	Remark      string
	ExpiresAt   time.Time
	Status      string
	ResourceSeq int
	ResourceID  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func TestStorageMigrationFixtureQueriesBindParametersInOrder(t *testing.T) {
	tests := []struct {
		name       string
		driver     storage.Driver
		query      func(storage.Driver) string
		wantValues string
	}{
		{
			name:       "mysql database instance",
			driver:     storage.DriverMySQL,
			query:      legacyDatabaseInstanceInsertQuery,
			wantValues: "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		},
		{
			name:       "postgres database instance",
			driver:     storage.DriverPostgres,
			query:      legacyDatabaseInstanceInsertQuery,
			wantValues: "VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)",
		},
		{
			name:       "mysql database account",
			driver:     storage.DriverMySQL,
			query:      legacyDatabaseAccountInsertQuery,
			wantValues: "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		},
		{
			name:       "postgres database account",
			driver:     storage.DriverPostgres,
			query:      legacyDatabaseAccountInsertQuery,
			wantValues: "VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			query := strings.Join(strings.Fields(tt.query(tt.driver)), " ")
			if !strings.Contains(query, tt.wantValues) {
				t.Fatalf("insert placeholders are out of order:\nquery: %s\nwant:  %s", query, tt.wantValues)
			}
		})
	}
}

func TestStorageMigrationUpgradesLegacyDatabaseInstances(t *testing.T) {
	for _, tt := range metadataDatabaseCases() {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			db := openMetadataDatabase(t, tt)
			createCurrentSchemaWithOnlyMigrationPending(t, db, databaseInstanceTLSMigrationVersion)
			seedAllCurrentMigrationsExcept(t, db, databaseInstanceTLSMigrationVersion)
			assertOtherDatabaseMigrationsApplied(t, db, databaseInstanceTLSMigrationVersion)
			instance := legacyDatabaseInstanceFixture()
			seedLegacyDatabaseInstance(t, db, tt.driver, instance)

			if db.Migrator().HasColumn(&model.DatabaseInstance{}, "tls_mode") {
				t.Fatal("legacy database instance schema unexpectedly contains tls_mode")
			}
			beforeMigrations := loadMigrationVersionSet(t, db)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("migrate legacy database instance schema: %v", err)
			}
			afterMigrations := loadMigrationVersionSet(t, db)
			assertOnlyMigrationVersionsAdded(t, beforeMigrations, afterMigrations, databaseInstanceTLSMigrationVersion)

			assertMigratedDatabaseInstance(t, db, tt.driver, instance)
			assertMigrationRecord(t, db, databaseInstanceTLSMigrationVersion, "database instance upstream TLS policy")
			beforeSecondMigration := loadMigrationVersionSet(t, db)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("second migration of legacy database instance schema: %v", err)
			}
			assertOnlyMigrationVersionsAdded(t, beforeSecondMigration, loadMigrationVersionSet(t, db))
			assertMigrationRecord(t, db, databaseInstanceTLSMigrationVersion, "database instance upstream TLS policy")
		})
	}
}

func TestStorageMigrationChangesDatabaseTLSDefaultWithoutRewritingRows(t *testing.T) {
	for _, tt := range metadataDatabaseCases() {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			db := openMetadataDatabase(t, tt)
			if err := db.Exec(`CREATE TABLE database_instances (
				id VARCHAR(64) PRIMARY KEY,
				name VARCHAR(255) NOT NULL,
				protocol VARCHAR(32) NOT NULL,
				address VARCHAR(255) NOT NULL,
				port INTEGER NOT NULL,
				tls_mode VARCHAR(16) NOT NULL DEFAULT 'verify-full',
				tls_server_name VARCHAR(255),
				tls_ca_pem TEXT,
				status VARCHAR(32)
			)`).Error; err != nil {
				t.Fatalf("create previous database TLS schema: %v", err)
			}
			if err := db.Exec(`INSERT INTO database_instances
				(id, name, protocol, address, port, tls_mode, status)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				"existing-tls-instance",
				"existing TLS instance",
				"mysql",
				"db.internal",
				3306,
				"verify-full",
				"active",
			).Error; err != nil {
				t.Fatalf("seed existing database TLS policy: %v", err)
			}
			seedAllCurrentMigrationsExcept(t, db, databaseTLSDefaultMigrationVersion)
			before := loadMigrationVersionSet(t, db)

			if err := storage.Migrate(db); err != nil {
				t.Fatalf("change database TLS default: %v", err)
			}
			assertOnlyMigrationVersionsAdded(
				t,
				before,
				loadMigrationVersionSet(t, db),
				databaseTLSDefaultMigrationVersion,
			)
			assertMigrationRecord(
				t,
				db,
				databaseTLSDefaultMigrationVersion,
				"database instance upstream TLS default",
			)

			var existingMode string
			if err := db.Table("database_instances").
				Select("tls_mode").
				Where("id = ?", "existing-tls-instance").
				Scan(&existingMode).Error; err != nil {
				t.Fatalf("load existing database TLS policy: %v", err)
			}
			if existingMode != "verify-full" {
				t.Fatalf("existing database TLS policy = %q, want verify-full", existingMode)
			}
			if err := db.Exec(`INSERT INTO database_instances
				(id, name, protocol, address, port, status)
				VALUES (?, ?, ?, ?, ?, ?)`,
				"default-tls-instance",
				"default TLS instance",
				"mysql",
				"db.internal",
				3306,
				"active",
			).Error; err != nil {
				t.Fatalf("insert database instance with default TLS policy: %v", err)
			}
			var defaultMode string
			if err := db.Table("database_instances").
				Select("tls_mode").
				Where("id = ?", "default-tls-instance").
				Scan(&defaultMode).Error; err != nil {
				t.Fatalf("load default database TLS policy: %v", err)
			}
			if defaultMode != "disable" {
				t.Fatalf("database TLS default = %q, want disable", defaultMode)
			}
			if err := db.Exec(`INSERT INTO database_instances
				(id, name, protocol, address, port, tls_mode, status)
				VALUES (?, ?, ?, ?, ?, NULL, ?)`,
				"null-tls-instance",
				"null TLS instance",
				"mysql",
				"db.internal",
				3306,
				"active",
			).Error; err == nil {
				t.Fatal("database accepted an explicit NULL TLS policy")
			}
			beforeSecondMigration := loadMigrationVersionSet(t, db)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("second database TLS default migration: %v", err)
			}
			assertOnlyMigrationVersionsAdded(
				t,
				beforeSecondMigration,
				loadMigrationVersionSet(t, db),
			)
		})
	}
}

func TestStorageMigrationUpgradesLegacyDatabaseAccounts(t *testing.T) {
	for _, tt := range metadataDatabaseCases() {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			db := openMetadataDatabase(t, tt)
			createCurrentSchemaWithOnlyMigrationPending(t, db, databaseAccountUniquenessMigrationVersion)
			seedAllCurrentMigrationsExcept(t, db, databaseAccountUniquenessMigrationVersion)
			assertOtherDatabaseMigrationsApplied(t, db, databaseAccountUniquenessMigrationVersion)
			instance := legacyDatabaseInstanceFixture()
			account := legacyDatabaseAccountFixture(t)
			seedLegacyDatabaseInstance(t, db, tt.driver, instance)
			seedLegacyDatabaseAccount(t, db, tt.driver, account)

			if db.Migrator().HasIndex(&model.DatabaseAccount{}, "uidx_database_accounts_instance_username") {
				t.Fatal("legacy database account schema unexpectedly contains instance/username unique index")
			}
			beforeMigrations := loadMigrationVersionSet(t, db)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("migrate legacy database account schema: %v", err)
			}
			afterMigrations := loadMigrationVersionSet(t, db)
			assertOnlyMigrationVersionsAdded(t, beforeMigrations, afterMigrations, databaseAccountUniquenessMigrationVersion)

			assertLegacyDatabaseAccountPreserved(t, db, account)
			if !db.Migrator().HasIndex(&model.DatabaseAccount{}, "uidx_database_accounts_instance_username") {
				t.Fatal("database account instance/username unique index is missing after migration")
			}
			assertDatabaseAccountUniqueIndexColumns(t, db, tt.driver)
			assertDatabaseAccountInstanceUsernameUniqueness(t, db, tt.driver, "upgrade", "u")
			assertMigrationRecord(t, db, databaseAccountUniquenessMigrationVersion, "database account instance username uniqueness")
			beforeSecondMigration := loadMigrationVersionSet(t, db)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("second migration of legacy database account schema: %v", err)
			}
			assertOnlyMigrationVersionsAdded(t, beforeSecondMigration, loadMigrationVersionSet(t, db))
			assertMigrationRecord(t, db, databaseAccountUniquenessMigrationVersion, "database account instance username uniqueness")
		})
	}
}

func TestStorageMigrationUpgradesLegacyAuditSessions(t *testing.T) {
	for _, tt := range metadataDatabaseCases() {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			db := openMetadataDatabase(t, tt)
			createCurrentSchemaWithOnlyMigrationPending(t, db, auditRetentionCleanupMigrationVersion)
			seedAllCurrentMigrationsExcept(t, db, auditRetentionCleanupMigrationVersion)
			assertOtherDatabaseMigrationsApplied(t, db, auditRetentionCleanupMigrationVersion)
			now := time.Now().UTC()
			if err := db.Exec(`INSERT INTO audit_sessions
				(id, started_at, ended_at, state, replay_dir, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				"legacy-audit-session",
				now.Add(-time.Hour),
				now,
				"ended",
				"data/replay/ssh/legacy-audit-session",
				now,
				now,
			).Error; err != nil {
				t.Fatalf("seed legacy audit session: %v", err)
			}

			if db.Migrator().HasColumn(&model.AuditSession{}, "cleanup_status") {
				t.Fatal("legacy audit schema unexpectedly contains cleanup_status")
			}
			beforeMigrations := loadMigrationVersionSet(t, db)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("migrate legacy audit schema: %v", err)
			}
			assertOnlyMigrationVersionsAdded(
				t,
				beforeMigrations,
				loadMigrationVersionSet(t, db),
				auditRetentionCleanupMigrationVersion,
			)
			for _, column := range []string{"cleanup_status", "cleanup_at", "cleanup_error"} {
				if !db.Migrator().HasColumn(&model.AuditSession{}, column) {
					t.Fatalf("audit retention column %s is missing", column)
				}
			}
			if !db.Migrator().HasIndex(&model.AuditSession{}, "idx_audit_sessions_cleanup") {
				t.Fatal("audit cleanup index is missing")
			}
			var session model.AuditSession
			if err := db.First(&session, "id = ?", "legacy-audit-session").Error; err != nil {
				t.Fatalf("load migrated audit session: %v", err)
			}
			if session.CleanupStatus != "ready" || session.ReplayDir != "data/replay/ssh/legacy-audit-session" {
				t.Fatalf("migrated audit session = %#v", session)
			}
			assertMigrationRecord(t, db, auditRetentionCleanupMigrationVersion, "audit retention cleanup state")
			beforeSecondMigration := loadMigrationVersionSet(t, db)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("second migration of legacy audit schema: %v", err)
			}
			assertOnlyMigrationVersionsAdded(t, beforeSecondMigration, loadMigrationVersionSet(t, db))
		})
	}
}

func TestStorageMigrationUpgradesLegacyAuditSessionLeases(t *testing.T) {
	for _, tt := range metadataDatabaseCases() {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			db := openMetadataDatabase(t, tt)
			createCurrentSchemaWithOnlyMigrationPending(t, db, auditSessionLeaseMigrationVersion)
			seedAllCurrentMigrationsExcept(t, db, auditSessionLeaseMigrationVersion)
			assertOtherDatabaseMigrationsApplied(t, db, auditSessionLeaseMigrationVersion)
			now := time.Now().UTC()
			if err := db.Exec(`INSERT INTO audit_sessions
				(id, started_at, state, cleanup_status, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?)`,
				"legacy-started-audit-session",
				now.Add(-time.Hour),
				"started",
				"ready",
				now,
				now,
			).Error; err != nil {
				t.Fatalf("seed legacy started audit session: %v", err)
			}

			for _, column := range []string{"lease_owner", "heartbeat_at", "lease_expires_at"} {
				if db.Migrator().HasColumn(&model.AuditSession{}, column) {
					t.Fatalf("legacy audit schema unexpectedly contains lease column %s", column)
				}
			}
			beforeMigrations := loadMigrationVersionSet(t, db)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("migrate legacy audit lease schema: %v", err)
			}
			assertOnlyMigrationVersionsAdded(
				t,
				beforeMigrations,
				loadMigrationVersionSet(t, db),
				auditSessionLeaseMigrationVersion,
			)
			for _, column := range []string{"lease_owner", "heartbeat_at", "lease_expires_at"} {
				if !db.Migrator().HasColumn(&model.AuditSession{}, column) {
					t.Fatalf("audit lease column %s is missing", column)
				}
			}
			for _, index := range []string{
				"idx_audit_sessions_lease_owner_state",
				"idx_audit_sessions_lease_expiry",
			} {
				if !db.Migrator().HasIndex(&model.AuditSession{}, index) {
					t.Fatalf("audit lease index %s is missing", index)
				}
			}
			var session model.AuditSession
			if err := db.First(&session, "id = ?", "legacy-started-audit-session").Error; err != nil {
				t.Fatalf("load migrated legacy audit session: %v", err)
			}
			if session.LeaseOwner != "" ||
				session.HeartbeatAt != nil ||
				session.LeaseExpiresAt != nil {
				t.Fatalf("audit lease migration guessed legacy activity: %#v", session)
			}
			assertMigrationRecord(t, db, auditSessionLeaseMigrationVersion, "audit session lease recovery")
			beforeSecondMigration := loadMigrationVersionSet(t, db)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("second migration of legacy audit lease schema: %v", err)
			}
			assertOnlyMigrationVersionsAdded(t, beforeSecondMigration, loadMigrationVersionSet(t, db))
		})
	}
}

func TestStorageMigrationAddsSystemSettings(t *testing.T) {
	for _, tt := range metadataDatabaseCases() {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			db := openMetadataDatabase(t, tt)
			createCurrentSchemaWithOnlyMigrationPending(t, db, systemSettingMigrationVersion)
			seedAllCurrentMigrationsExcept(t, db, systemSettingMigrationVersion)
			assertOtherDatabaseMigrationsApplied(t, db, systemSettingMigrationVersion)
			if db.Migrator().HasTable(&model.SystemSetting{}) ||
				db.Migrator().HasTable(&model.SystemSettingRevision{}) {
				t.Fatal("legacy schema unexpectedly contains system settings tables")
			}

			beforeMigrations := loadMigrationVersionSet(t, db)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("migrate system settings schema: %v", err)
			}
			assertOnlyMigrationVersionsAdded(
				t,
				beforeMigrations,
				loadMigrationVersionSet(t, db),
				systemSettingMigrationVersion,
			)
			if !db.Migrator().HasTable(&model.SystemSetting{}) ||
				!db.Migrator().HasTable(&model.SystemSettingRevision{}) {
				t.Fatal("system settings migration did not create both tables")
			}
			if !db.Migrator().HasIndex(
				&model.SystemSettingRevision{},
				"idx_system_setting_revisions_revision",
			) {
				t.Fatal("system settings revision unique index is missing")
			}
			assertMigrationRecord(
				t,
				db,
				systemSettingMigrationVersion,
				"system configuration management",
			)
			beforeSecondMigration := loadMigrationVersionSet(t, db)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("second system settings migration: %v", err)
			}
			assertOnlyMigrationVersionsAdded(t, beforeSecondMigration, loadMigrationVersionSet(t, db))
		})
	}
}

func TestStorageMigrationExpandsDatabaseAuditQueryPayload(t *testing.T) {
	for _, tt := range metadataDatabaseCases() {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			db := openMetadataDatabase(t, tt)

			// Build the legacy schema by executing every historical migration
			// through 202607190005. Pre-recording 006 makes Migrate skip only
			// that migration without creating the latest schema and manually
			// downgrading it.
			seedAppliedMigrations(t, db, auditDBQueryLargePayloadMigrationVersion)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("build schema from historical migrations through 005: %v", err)
			}
			if err := db.Delete(
				&storage.SchemaMigration{},
				"version = ?",
				auditDBQueryLargePayloadMigrationVersion,
			).Error; err != nil {
				t.Fatalf("make database audit query payload migration pending: %v", err)
			}
			assertOtherDatabaseMigrationsApplied(
				t,
				db,
				auditDBQueryLargePayloadMigrationVersion,
			)
			legacyMigrations := loadMigrationVersionSet(t, db)
			if _, ok := legacyMigrations[auditDBQueryLargePayloadMigrationVersion]; ok {
				t.Fatal("database audit query payload migration is already recorded in legacy fixture")
			}
			for _, version := range currentStorageMigrationVersions {
				if version == auditDBQueryLargePayloadMigrationVersion {
					continue
				}
				if _, ok := legacyMigrations[version]; !ok {
					t.Fatalf("historical migration %s was not executed for legacy fixture", version)
				}
			}
			if db.Migrator().HasColumn(
				&model.SystemSetting{},
				"DatabaseMaxClientMessageBytes",
			) {
				t.Fatal("historical system settings schema unexpectedly contains database client message limit")
			}
			for _, field := range []string{"OriginalSQLBytes", "SQLTruncated"} {
				if db.Migrator().HasColumn(&model.AuditDBQuery{}, field) {
					t.Fatalf("historical database audit schema unexpectedly contains %s", field)
				}
			}

			legacy := model.AuditDBQuery{
				ID:             "legacy-database-audit-query",
				AuditSessionID: "legacy-database-audit-session",
				Timestamp:      time.Now().UTC(),
				SQLText:        "SELECT 'preserve me'",
				QueryKind:      "query",
				DurationMs:     17,
			}
			if err := db.
				Omit("OriginalSQLBytes", "SQLTruncated").
				Create(&legacy).Error; err != nil {
				t.Fatalf("seed legacy database audit query: %v", err)
			}
			legacySettings := model.SystemSetting{
				ID:                          model.SystemSettingSingletonID,
				WebRDPConnectTimeoutSeconds: 15,
				RecordingEnabled:            true,
				RecordingRecordCommands:     true,
				RecordingRetentionDays:      30,
				RecordingMaxReplayBytes:     1024,
				RecordingCleanupBatchSize:   100,
				Revision:                    2,
				AppliedRevision:             1,
			}
			if err := db.Omit("DatabaseMaxClientMessageBytes").
				Create(&legacySettings).Error; err != nil {
				t.Fatalf("seed legacy system settings: %v", err)
			}
			if tt.driver == storage.DriverMySQL {
				if got := databaseAuditSQLTextType(t, db); got != "text" {
					t.Fatalf("legacy MySQL audit query type = %q, want text", got)
				}
			}

			beforeMigrations := loadMigrationVersionSet(t, db)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("migrate database audit query payload storage: %v", err)
			}
			assertOnlyMigrationVersionsAdded(
				t,
				beforeMigrations,
				loadMigrationVersionSet(t, db),
				auditDBQueryLargePayloadMigrationVersion,
			)

			wantType := "text"
			if tt.driver == storage.DriverMySQL {
				wantType = "longtext"
			}
			if got := databaseAuditSQLTextType(t, db); got != wantType {
				t.Fatalf("migrated database audit query type = %q, want %q", got, wantType)
			}
			if !db.Migrator().HasIndex(
				&model.AuditDBQuery{},
				"idx_audit_db_queries_session_timestamp_id",
			) {
				t.Fatal("database audit query pagination index was not created")
			}

			var preserved model.AuditDBQuery
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
			if migratedSettings.DatabaseMaxClientMessageBytes != 10*1024*1024 {
				t.Fatalf(
					"database client message limit = %d, want %d",
					migratedSettings.DatabaseMaxClientMessageBytes,
					10*1024*1024,
				)
			}

			payload := strings.Repeat("x", 10*1024*1024)
			large := model.AuditDBQuery{
				ID:               "large-database-audit-query",
				AuditSessionID:   "large-database-audit-session",
				Timestamp:        legacy.Timestamp.Add(time.Second),
				SQLText:          payload,
				OriginalSQLBytes: int64(len(payload)),
				SQLTruncated:     true,
				QueryKind:        "query",
			}
			quietDB := db.Session(&gorm.Session{Logger: gormLogger.Discard})
			if err := quietDB.Create(&large).Error; err != nil {
				t.Fatalf("store 10 MiB database audit query: %v", err)
			}
			var storedBytes int64
			if err := db.Table("audit_db_queries").
				Select("OCTET_LENGTH(sql_text)").
				Where("id = ?", large.ID).
				Scan(&storedBytes).Error; err != nil {
				t.Fatalf("measure stored database audit query: %v", err)
			}
			if storedBytes != int64(len(payload)) {
				t.Fatalf("stored database audit query = %d bytes, want %d", storedBytes, len(payload))
			}
			var storedMetadata model.AuditDBQuery
			if err := db.Select(
				"id",
				"original_sql_bytes",
				"sql_truncated",
			).First(&storedMetadata, "id = ?", large.ID).Error; err != nil {
				t.Fatalf("load stored database audit SQL metadata: %v", err)
			}
			if storedMetadata.OriginalSQLBytes != int64(len(payload)) || !storedMetadata.SQLTruncated {
				t.Fatalf("stored database audit SQL metadata = %#v, want %#v", storedMetadata, large)
			}
			previews, previewTotal, err := store.NewDBStore(db).
				ListAuditDBQueryPreviews(
					context.Background(),
					large.AuditSessionID,
					store.AuditDBQueryPreviewParams{
						Search: "xxx",
						Limit:  1,
					},
				)
			if err != nil {
				t.Fatalf("load bounded database audit query preview: %v", err)
			}
			if previewTotal != 1 ||
				len(previews) != 1 ||
				len(previews[0].SQLText) != 64*1024 ||
				previews[0].SQLStoredBytes != int64(len(payload)) ||
				previews[0].OriginalSQLBytes != int64(len(payload)) ||
				!previews[0].SQLTruncated {
				t.Fatalf(
					"bounded database audit query preview = %#v, total = %d",
					previews,
					previewTotal,
				)
			}

			literalSessionID := "literal-search-database-audit-session"
			literalQueries := []model.AuditDBQuery{
				{
					ID: "literal-percent-query", AuditSessionID: literalSessionID,
					Timestamp: legacy.Timestamp.Add(2 * time.Second),
					SQLText:   "SELECT 'discount 10%!'",
				},
				{
					ID: "literal-underscore-query", AuditSessionID: literalSessionID,
					Timestamp: legacy.Timestamp.Add(3 * time.Second),
					SQLText:   "SELECT customer_id",
				},
				{
					ID: "literal-wildcard-decoy-query", AuditSessionID: literalSessionID,
					Timestamp: legacy.Timestamp.Add(4 * time.Second),
					SQLText:   "SELECT customerXid, 'discount 100'",
				},
			}
			if err := db.Create(&literalQueries).Error; err != nil {
				t.Fatalf("store literal search database audit queries: %v", err)
			}
			for _, searchCase := range []struct {
				search string
				wantID string
			}{
				{search: "10%!", wantID: "literal-percent-query"},
				{search: "customer_id", wantID: "literal-underscore-query"},
				{search: "%!", wantID: "literal-percent-query"},
			} {
				items, total, err := store.NewDBStore(db).ListAuditDBQueryPreviews(
					context.Background(),
					literalSessionID,
					store.AuditDBQueryPreviewParams{
						Search: searchCase.search,
						Limit:  10,
					},
				)
				if err != nil {
					t.Fatalf("literal database audit query search %q: %v", searchCase.search, err)
				}
				if total != 1 || len(items) != 1 || items[0].ID != searchCase.wantID {
					t.Fatalf(
						"literal database audit query search %q = %#v, total = %d, want %s",
						searchCase.search,
						items,
						total,
						searchCase.wantID,
					)
				}
			}

			assertMigrationRecord(
				t,
				db,
				auditDBQueryLargePayloadMigrationVersion,
				"large database proxy client message support",
			)

			// MySQL DDL is not transactionally rolled back. Simulate the
			// process stopping after AutoMigrate changed the schema but before
			// schema_migrations was recorded, then verify a retry is safe.
			if tt.driver == storage.DriverMySQL {
				if err := db.Delete(
					&storage.SchemaMigration{},
					"version = ?",
					auditDBQueryLargePayloadMigrationVersion,
				).Error; err != nil {
					t.Fatalf("remove migration record after applied MySQL DDL: %v", err)
				}
				beforeRetry := loadMigrationVersionSet(t, db)
				if err := storage.Migrate(db); err != nil {
					t.Fatalf("retry database audit query payload migration after applied MySQL DDL: %v", err)
				}
				assertOnlyMigrationVersionsAdded(
					t,
					beforeRetry,
					loadMigrationVersionSet(t, db),
					auditDBQueryLargePayloadMigrationVersion,
				)
				if got := databaseAuditSQLTextType(t, db); got != "longtext" {
					t.Fatalf("retried MySQL audit query type = %q, want longtext", got)
				}
				var retriedBytes int64
				if err := db.Table("audit_db_queries").
					Select("OCTET_LENGTH(sql_text)").
					Where("id = ?", large.ID).
					Scan(&retriedBytes).Error; err != nil {
					t.Fatalf("measure database audit query after MySQL retry: %v", err)
				}
				if retriedBytes != int64(len(payload)) {
					t.Fatalf(
						"database audit query after MySQL retry = %d bytes, want %d",
						retriedBytes,
						len(payload),
					)
				}
				assertMigrationRecord(
					t,
					db,
					auditDBQueryLargePayloadMigrationVersion,
					"large database proxy client message support",
				)
			}

			beforeSecondMigration := loadMigrationVersionSet(t, db)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("second database audit query payload migration: %v", err)
			}
			assertOnlyMigrationVersionsAdded(
				t,
				beforeSecondMigration,
				loadMigrationVersionSet(t, db),
			)
		})
	}
}

func databaseAuditSQLTextType(t *testing.T, db *gorm.DB) string {
	t.Helper()
	columns, err := db.Migrator().ColumnTypes(&model.AuditDBQuery{})
	if err != nil {
		t.Fatalf("load database audit query column types: %v", err)
	}
	for _, column := range columns {
		if strings.EqualFold(column.Name(), "sql_text") {
			return strings.ToLower(column.DatabaseTypeName())
		}
	}
	t.Fatal("database audit query sql_text column is missing")
	return ""
}

func TestStorageMigrationRejectsLegacyDatabaseAccountDuplicatesAndRetries(t *testing.T) {
	for _, tt := range metadataDatabaseCases() {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			db := openMetadataDatabase(t, tt)
			createCurrentSchemaWithOnlyMigrationPending(t, db, databaseAccountUniquenessMigrationVersion)
			seedAllCurrentMigrationsExcept(t, db, databaseAccountUniquenessMigrationVersion)
			assertOtherDatabaseMigrationsApplied(t, db, databaseAccountUniquenessMigrationVersion)
			instance := legacyDatabaseInstanceFixture()
			seedLegacyDatabaseInstance(t, db, tt.driver, instance)
			const credential = "legacy-credential-that-must-not-leak"
			firstDuplicate := legacyDuplicateDatabaseAccountFixture(t, "account-duplicate-1", "duplicate-reader-one", "D001", 81, credential)
			secondDuplicate := legacyDuplicateDatabaseAccountFixture(t, "account-duplicate-2", "duplicate-reader-two", "D002", 82, credential)
			seedLegacyDatabaseAccount(t, db, tt.driver, firstDuplicate)
			seedLegacyDatabaseAccount(t, db, tt.driver, secondDuplicate)
			assertLegacyDuplicateFixture(t, db, instance.ID, "reader", []string{firstDuplicate.ResourceID, secondDuplicate.ResourceID})

			beforeFailure := loadMigrationVersionSet(t, db)
			err := storage.Migrate(db)
			if err == nil {
				t.Fatal("migration accepted legacy duplicate database accounts")
			}
			afterFailure := loadMigrationVersionSet(t, db)
			assertOnlyMigrationVersionsAdded(t, beforeFailure, afterFailure)
			message := err.Error()
			for _, fragment := range []string{
				"migration " + databaseAccountUniquenessMigrationVersion,
				"duplicate database accounts share the same instance_id and username",
				"delete or rename duplicate accounts before retrying",
			} {
				if !strings.Contains(message, fragment) {
					t.Fatalf("migration error %q does not contain actionable fragment %q", message, fragment)
				}
			}
			if strings.Contains(message, credential) {
				t.Fatalf("migration error leaked credentials: %q", message)
			}
			for _, ciphertext := range []string{firstDuplicate.PasswordRaw, secondDuplicate.PasswordRaw} {
				if strings.Contains(message, ciphertext) {
					t.Fatalf("migration error leaked encrypted credentials: %q", message)
				}
			}
			assertMigrationRecordCount(t, db, databaseAccountUniquenessMigrationVersion, 0)

			if err := db.Exec("DELETE FROM database_accounts WHERE id = ?", secondDuplicate.ID).Error; err != nil {
				t.Fatalf("remove duplicate database account before retry: %v", err)
			}
			beforeRetry := loadMigrationVersionSet(t, db)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("retry migration after removing duplicate: %v", err)
			}
			afterRetry := loadMigrationVersionSet(t, db)
			assertOnlyMigrationVersionsAdded(t, beforeRetry, afterRetry, databaseAccountUniquenessMigrationVersion)
			if !db.Migrator().HasIndex(&model.DatabaseAccount{}, "uidx_database_accounts_instance_username") {
				t.Fatal("database account instance/username unique index is missing after retry")
			}
			assertMigrationRecord(t, db, databaseAccountUniquenessMigrationVersion, "database account instance username uniqueness")
			beforeSecondMigration := loadMigrationVersionSet(t, db)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("second migration after duplicate retry: %v", err)
			}
			assertOnlyMigrationVersionsAdded(t, beforeSecondMigration, loadMigrationVersionSet(t, db))
		})
	}
}

func metadataDatabaseCases() []metadataDatabaseCase {
	return []metadataDatabaseCase{
		{
			name:   "mysql8",
			driver: storage.DriverMySQL,
			container: func(host string) []string {
				return []string{
					"-e", "MYSQL_ROOT_PASSWORD=metadata-root-password",
					"-e", "MYSQL_DATABASE=jianmen_metadata",
					"-p", host + "::3306",
					"mysql:8.0",
				}
			},
			port: "3306/tcp",
			connection: func(addr string) string {
				return fmt.Sprintf("root:metadata-root-password@tcp(%s)/jianmen_metadata?charset=utf8mb4&parseTime=True&loc=UTC&timeout=2s", addr)
			},
		},
		{
			name:   "postgres16",
			driver: storage.DriverPostgres,
			container: func(host string) []string {
				return []string{
					"-e", "POSTGRES_USER=metadata",
					"-e", "POSTGRES_PASSWORD=metadata-password",
					"-e", "POSTGRES_DB=jianmen_metadata",
					"-p", host + "::5432",
					"postgres:16",
				}
			},
			port: "5432/tcp",
			connection: func(addr string) string {
				return fmt.Sprintf(
					"postgres://%s:%s@%s/%s?sslmode=disable",
					url.QueryEscape("metadata"),
					url.QueryEscape("metadata-password"),
					addr,
					url.PathEscape("jianmen_metadata"),
				) + "&connect_timeout=2"
			},
		},
	}
}

func openMetadataDatabase(t *testing.T, tt metadataDatabaseCase) *gorm.DB {
	t.Helper()
	requireDocker(t)
	bindHost := dockerBindHost(t)
	containerID := runContainer(t, "jianmen-it-storage-"+tt.name, tt.container(bindHost)...)
	if tt.driver == storage.DriverMySQL {
		waitMySQLContainerInitialized(t, containerID)
	}
	addr := containerAddress(t, containerID, tt.port)
	config := storage.Config{Driver: tt.driver, DSN: tt.connection(addr)}
	waitMetadataDatabase(t, config)

	db, err := storage.Open(config)
	if err != nil {
		t.Fatalf("open metadata database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get metadata sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&storage.SchemaMigration{}); err != nil {
		t.Fatalf("create schema migrations table: %v", err)
	}
	return db
}

func waitMetadataDatabase(t *testing.T, config storage.Config) {
	t.Helper()
	waitFor(t, 2*time.Minute, time.Second, func() error {
		db, err := storage.Open(config)
		if err != nil {
			return err
		}
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		defer sqlDB.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return sqlDB.PingContext(ctx)
	})
}

func createCurrentSchemaWithOnlyMigrationPending(t *testing.T, db *gorm.DB, pendingVersion string) {
	t.Helper()
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("create current metadata schema before isolating migration %s: %v", pendingVersion, err)
	}
	if err := db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&storage.SchemaMigration{}).Error; err != nil {
		t.Fatalf("clear current migration records before isolating migration %s: %v", pendingVersion, err)
	}
	switch pendingVersion {
	case databaseInstanceTLSMigrationVersion:
		for _, column := range []string{"tls_ca_pem", "tls_server_name", "tls_mode"} {
			if err := db.Migrator().DropColumn(&model.DatabaseInstance{}, column); err != nil {
				t.Fatalf("remove migration %s column %s from fixture: %v", pendingVersion, column, err)
			}
		}
	case databaseAccountUniquenessMigrationVersion:
		if err := db.Migrator().DropIndex(&model.DatabaseAccount{}, "uidx_database_accounts_instance_username"); err != nil {
			t.Fatalf("remove migration %s unique index from fixture: %v", pendingVersion, err)
		}
	case auditRetentionCleanupMigrationVersion:
		for _, column := range []string{"cleanup_error", "cleanup_at", "cleanup_status"} {
			if err := db.Migrator().DropColumn(&model.AuditSession{}, column); err != nil {
				t.Fatalf("remove migration %s column %s from fixture: %v", pendingVersion, column, err)
			}
		}
	case auditSessionLeaseMigrationVersion:
		for _, column := range []string{"lease_expires_at", "heartbeat_at", "lease_owner"} {
			if err := db.Migrator().DropColumn(&model.AuditSession{}, column); err != nil {
				t.Fatalf("remove migration %s column %s from fixture: %v", pendingVersion, column, err)
			}
		}
	case systemSettingMigrationVersion:
		if err := db.Migrator().DropTable(
			&model.SystemSettingRevision{},
			&model.SystemSetting{},
		); err != nil {
			t.Fatalf("remove migration %s tables from fixture: %v", pendingVersion, err)
		}
	default:
		t.Fatalf("unsupported isolated migration version %s", pendingVersion)
	}
}

func assertOtherDatabaseMigrationsApplied(t *testing.T, db *gorm.DB, pendingVersion string) {
	t.Helper()
	if pendingVersion != databaseInstanceTLSMigrationVersion {
		for _, column := range []string{"tls_mode", "tls_server_name", "tls_ca_pem"} {
			if !db.Migrator().HasColumn(&model.DatabaseInstance{}, column) {
				t.Fatalf("fixture for pending migration %s is missing applied 006 column %s", pendingVersion, column)
			}
		}
	}
	if !db.Migrator().HasIndex(&model.Permission{}, "idx_permissions_logic") {
		t.Fatalf("fixture for pending migration %s is missing applied 007 permission unique index", pendingVersion)
	}
	if pendingVersion != databaseAccountUniquenessMigrationVersion &&
		!db.Migrator().HasIndex(&model.DatabaseAccount{}, "uidx_database_accounts_instance_username") {
		t.Fatalf("fixture for pending migration %s is missing applied 008 account unique index", pendingVersion)
	}
	if pendingVersion != auditRetentionCleanupMigrationVersion {
		for _, column := range []string{"cleanup_status", "cleanup_at", "cleanup_error"} {
			if !db.Migrator().HasColumn(&model.AuditSession{}, column) {
				t.Fatalf("fixture for pending migration %s is missing applied audit column %s", pendingVersion, column)
			}
		}
	}
	if pendingVersion != auditSessionLeaseMigrationVersion {
		for _, column := range []string{"lease_owner", "heartbeat_at", "lease_expires_at"} {
			if !db.Migrator().HasColumn(&model.AuditSession{}, column) {
				t.Fatalf("fixture for pending migration %s is missing applied lease column %s", pendingVersion, column)
			}
		}
	}
	if pendingVersion != systemSettingMigrationVersion {
		for _, table := range []any{
			&model.SystemSetting{},
			&model.SystemSettingRevision{},
		} {
			if !db.Migrator().HasTable(table) {
				t.Fatalf("fixture for pending migration %s is missing table for %T", pendingVersion, table)
			}
		}
	}
}

func seedAppliedMigrations(t *testing.T, db *gorm.DB, versions ...string) {
	t.Helper()
	names := map[string]string{
		"202606290001":                           "prepare metadata sequences",
		"202606290002":                           "core metadata schema",
		"202606290003":                           "reconcile metadata resources",
		"202606290004":                           "global compact session identity",
		"202606290005":                           "metadata query indexes",
		"202607130001":                           "user groups and resource grants",
		"202607160001":                           "AI access tokens",
		"202607160002":                           "encrypted AI token values",
		"202607170001":                           "container management endpoints",
		"202607170002":                           "user expiry and temporary authorization metadata",
		"202607180001":                           "database backed super administrator identity",
		"202607180002":                           "temporary access connection password lifecycle",
		"202607180003":                           "atomic system initialization guard",
		"202607180004":                           "browser sessions and websocket tickets",
		"202607180005":                           "remove reversible AI token secrets",
		"202607180006":                           "database instance upstream TLS policy",
		"202607180007":                           "permission logical uniqueness",
		"202607180008":                           "database account instance username uniqueness",
		"202607180009":                           "database provisioning saga recovery state",
		"202607190001":                           "resource grant logical uniqueness",
		"202607190002":                           "audit retention cleanup state",
		"202607190003":                           "web RDP access control and audit schema",
		"202607190004":                           "audit session lease recovery",
		systemSettingMigrationVersion:            "system configuration management",
		auditDBQueryLargePayloadMigrationVersion: "large database proxy client message support",
		databaseGatewayModeMigrationVersion:      "database gateway mode system setting",
	}
	for _, version := range versions {
		name, ok := names[version]
		if !ok {
			t.Fatalf("missing migration name for version %s", version)
		}
		if err := db.Create(&storage.SchemaMigration{Version: version, Name: name, AppliedAt: time.Now().UTC()}).Error; err != nil {
			t.Fatalf("record previous migration %s: %v", version, err)
		}
	}
}

func seedAllCurrentMigrationsExcept(t *testing.T, db *gorm.DB, excludedVersion string) {
	t.Helper()
	versions := make([]string, 0, len(currentStorageMigrationVersions)-1)
	for _, version := range currentStorageMigrationVersions {
		if version != excludedVersion {
			versions = append(versions, version)
		}
	}
	if len(versions) != len(currentStorageMigrationVersions)-1 {
		t.Fatalf("excluded migration version %s is not in current migration list", excludedVersion)
	}
	seedAppliedMigrations(t, db, versions...)
}

func legacyDatabaseInstanceFixture() legacyDatabaseInstanceRow {
	return legacyDatabaseInstanceRow{
		ID:        "instance-legacy",
		Name:      "legacy-orders",
		Protocol:  "postgres",
		Address:   "legacy-db.internal",
		Port:      55432,
		GroupName: "legacy-primary",
		Remark:    "preserve this instance remark",
		Status:    "disabled",
		CreatedAt: time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt: time.Date(2025, time.February, 3, 4, 5, 6, 0, time.UTC),
	}
}

func legacyDatabaseAccountFixture(t *testing.T) legacyDatabaseAccountRow {
	t.Helper()
	const plaintext = "legacy-account-password"
	return legacyDatabaseAccountRow{
		ID:          "account-legacy",
		InstanceID:  "instance-legacy",
		UniqueName:  "legacy-reporting-reader",
		Username:    "report_reader",
		Password:    plaintext,
		PasswordRaw: encryptLegacyPassword(t, plaintext),
		GroupName:   "legacy-reporting",
		Remark:      "preserve this account remark",
		ExpiresAt:   time.Date(2028, time.March, 4, 5, 6, 7, 0, time.UTC),
		Status:      "disabled",
		ResourceSeq: 73,
		ResourceID:  "L001",
		CreatedAt:   time.Date(2025, time.April, 5, 6, 7, 8, 0, time.UTC),
		UpdatedAt:   time.Date(2025, time.May, 6, 7, 8, 9, 0, time.UTC),
	}
}

func legacyDuplicateDatabaseAccountFixture(
	t *testing.T,
	id, uniqueName, resourceID string,
	resourceSeq int,
	password string,
) legacyDatabaseAccountRow {
	t.Helper()
	return legacyDatabaseAccountRow{
		ID:          id,
		InstanceID:  "instance-legacy",
		UniqueName:  uniqueName,
		Username:    "reader",
		Password:    password,
		PasswordRaw: encryptLegacyPassword(t, password),
		GroupName:   "legacy-duplicates",
		Remark:      "duplicate migration fixture",
		ExpiresAt:   time.Date(2028, time.June, 7, 8, 9, 10, 0, time.UTC),
		Status:      "active",
		ResourceSeq: resourceSeq,
		ResourceID:  resourceID,
		CreatedAt:   time.Date(2025, time.June, 7, 8, 9, 10, 0, time.UTC),
		UpdatedAt:   time.Date(2025, time.July, 8, 9, 10, 11, 0, time.UTC),
	}
}

func encryptLegacyPassword(t *testing.T, plaintext string) string {
	t.Helper()
	if len(crypto.GetKey()) != 32 {
		if _, err := crypto.Init(t.TempDir()); err != nil {
			t.Fatalf("initialize encryption for legacy password fixture: %v", err)
		}
	}
	value, err := model.NewEncryptedField(plaintext).Value()
	if err != nil {
		t.Fatalf("encrypt legacy password fixture: %v", err)
	}
	encrypted, ok := value.(string)
	if !ok || encrypted == "" {
		t.Fatalf("encrypted legacy password fixture has unexpected type/value %T", value)
	}
	return encrypted
}

func legacyDatabaseInstanceInsertQuery(driver storage.Driver) string {
	if driver == storage.DriverPostgres {
		return `INSERT INTO database_instances
			(id, name, protocol, address, port, group_name, remark, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	}
	return `INSERT INTO database_instances
		(id, name, protocol, address, port, group_name, remark, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
}

func legacyDatabaseAccountInsertQuery(driver storage.Driver) string {
	if driver == storage.DriverPostgres {
		return `INSERT INTO database_accounts
			(id, instance_id, unique_name, username, password, group_name, remark, expires_at, status, resource_seq, resource_id, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`
	}
	return `INSERT INTO database_accounts
		(id, instance_id, unique_name, username, password, group_name, remark, expires_at, status, resource_seq, resource_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
}

func seedLegacyDatabaseInstance(t *testing.T, db *gorm.DB, driver storage.Driver, row legacyDatabaseInstanceRow) {
	t.Helper()
	if err := db.Exec(legacyDatabaseInstanceInsertQuery(driver),
		row.ID, row.Name, row.Protocol, row.Address, row.Port,
		row.GroupName, row.Remark, row.Status, row.CreatedAt, row.UpdatedAt,
	).Error; err != nil {
		t.Fatalf("seed legacy database instance %s: %v", row.ID, err)
	}
}

func seedLegacyDatabaseAccount(t *testing.T, db *gorm.DB, driver storage.Driver, row legacyDatabaseAccountRow) {
	t.Helper()
	if err := db.Exec(legacyDatabaseAccountInsertQuery(driver),
		row.ID, row.InstanceID, row.UniqueName, row.Username, row.PasswordRaw,
		row.GroupName, row.Remark, row.ExpiresAt, row.Status, row.ResourceSeq,
		row.ResourceID, row.CreatedAt, row.UpdatedAt,
	).Error; err != nil {
		t.Fatalf("seed legacy database account %s: %v", row.ID, err)
	}
}

func assertMigratedDatabaseInstance(t *testing.T, db *gorm.DB, driver storage.Driver, want legacyDatabaseInstanceRow) {
	t.Helper()
	for _, column := range []string{"tls_mode", "tls_server_name", "tls_ca_pem"} {
		if !db.Migrator().HasColumn(&model.DatabaseInstance{}, column) {
			t.Fatalf("migrated database instance is missing TLS column %s", column)
		}
	}
	var instance model.DatabaseInstance
	if err := db.First(&instance, "id = ?", want.ID).Error; err != nil {
		t.Fatalf("load migrated database instance: %v", err)
	}
	if instance.TLSMode != "disable" {
		t.Fatalf("migrated TLS mode = %q, want disable", instance.TLSMode)
	}
	if instance.ID != want.ID ||
		instance.Name != want.Name ||
		instance.Protocol != want.Protocol ||
		instance.Address != want.Address ||
		instance.Port != want.Port ||
		instance.GroupName != want.GroupName ||
		instance.Remark != want.Remark ||
		instance.Status != want.Status ||
		instance.TLSServerName != "" ||
		instance.TLSCAPEM != "" ||
		!instance.CreatedAt.Equal(want.CreatedAt) ||
		!instance.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("migrated database instance lost legacy data: %#v", instance)
	}

	defaultRow := want
	defaultRow.ID = "instance-default-tls"
	defaultRow.Name = "default TLS instance"
	seedLegacyDatabaseInstance(t, db, driver, defaultRow)
	var defaults struct {
		TLSMode       string         `gorm:"column:tls_mode"`
		TLSServerName sql.NullString `gorm:"column:tls_server_name"`
		TLSCAPEM      sql.NullString `gorm:"column:tls_ca_pem"`
	}
	if err := db.Table("database_instances").
		Select("tls_mode, tls_server_name, tls_ca_pem").
		Where("id = ?", defaultRow.ID).
		Take(&defaults).Error; err != nil {
		t.Fatalf("load database-level TLS defaults: %v", err)
	}
	if defaults.TLSMode != "disable" || defaults.TLSServerName.Valid || defaults.TLSCAPEM.Valid {
		t.Fatalf("database-level TLS defaults = %#v, want disable and NULL optional values", defaults)
	}
	if err := db.Exec(`INSERT INTO database_instances
		(id, name, protocol, address, port, status, tls_mode)
		VALUES ('instance-null-tls', 'null TLS instance', 'mysql', 'null-tls.internal', 3306, 'active', NULL)`).Error; err == nil {
		t.Fatal("database accepted an explicit NULL tls_mode despite NOT NULL migration constraint")
	}
}

func assertLegacyDatabaseAccountPreserved(t *testing.T, db *gorm.DB, want legacyDatabaseAccountRow) {
	t.Helper()
	var row struct {
		ID          string
		InstanceID  string `gorm:"column:instance_id"`
		UniqueName  string `gorm:"column:unique_name"`
		Username    string
		Password    string
		GroupName   string `gorm:"column:group_name"`
		Remark      string
		ExpiresAt   *time.Time `gorm:"column:expires_at"`
		Status      string
		ResourceSeq int       `gorm:"column:resource_seq"`
		ResourceID  string    `gorm:"column:resource_id"`
		CreatedAt   time.Time `gorm:"column:created_at"`
		UpdatedAt   time.Time `gorm:"column:updated_at"`
	}
	if err := db.Table("database_accounts").Where("id = ?", want.ID).Take(&row).Error; err != nil {
		t.Fatalf("load migrated database account: %v", err)
	}
	if row.ID != want.ID ||
		row.InstanceID != want.InstanceID ||
		row.UniqueName != want.UniqueName ||
		row.Username != want.Username ||
		row.Password != want.PasswordRaw ||
		row.GroupName != want.GroupName ||
		row.Remark != want.Remark ||
		row.ExpiresAt == nil ||
		!row.ExpiresAt.Equal(want.ExpiresAt) ||
		row.Status != want.Status ||
		row.ResourceSeq != want.ResourceSeq ||
		row.ResourceID != want.ResourceID ||
		!row.CreatedAt.Equal(want.CreatedAt) ||
		!row.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("migrated database account lost legacy data: %#v", row)
	}
	var account model.DatabaseAccount
	if err := db.Where("id = ?", want.ID).Take(&account).Error; err != nil {
		t.Fatalf("load migrated database account through encrypted model field: %v", err)
	}
	if account.Password.GetPlaintext() != want.Password {
		t.Fatalf("migrated database account password plaintext = %q, want preserved value", account.Password.GetPlaintext())
	}
}

func assertLegacyDuplicateFixture(t *testing.T, db *gorm.DB, instanceID, username string, wantResourceIDs []string) {
	t.Helper()
	var rows []struct {
		ID         string
		UniqueName string `gorm:"column:unique_name"`
		ResourceID string `gorm:"column:resource_id"`
	}
	if err := db.Table("database_accounts").
		Select("id, unique_name, resource_id").
		Where("instance_id = ? AND username = ?", instanceID, username).
		Order("id").
		Scan(&rows).Error; err != nil {
		t.Fatalf("load duplicate migration fixture: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("duplicate migration fixture row count = %d, want 2", len(rows))
	}
	wantResources := make(map[string]struct{}, len(wantResourceIDs))
	for _, resourceID := range wantResourceIDs {
		wantResources[resourceID] = struct{}{}
	}
	seenResources := make(map[string]struct{}, len(rows))
	seenUniqueNames := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := wantResources[row.ResourceID]; !ok {
			t.Fatalf("duplicate migration fixture has unexpected resource_id %q", row.ResourceID)
		}
		if _, exists := seenResources[row.ResourceID]; exists {
			t.Fatalf("duplicate migration fixture reused resource_id %q", row.ResourceID)
		}
		if _, exists := seenUniqueNames[row.UniqueName]; exists {
			t.Fatalf("duplicate migration fixture reused unique_name %q", row.UniqueName)
		}
		seenResources[row.ResourceID] = struct{}{}
		seenUniqueNames[row.UniqueName] = struct{}{}
	}
}

func assertMigrationRecord(t *testing.T, db *gorm.DB, version, name string) {
	t.Helper()
	var row storage.SchemaMigration
	if err := db.Where("version = ?", version).Take(&row).Error; err != nil {
		t.Fatalf("load migration record %s: %v", version, err)
	}
	if row.Name != name {
		t.Fatalf("migration %s name = %q, want %q", version, row.Name, name)
	}
	assertMigrationRecordCount(t, db, version, 1)
}

func assertMigrationRecordCount(t *testing.T, db *gorm.DB, version string, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&storage.SchemaMigration{}).Where("version = ?", version).Count(&count).Error; err != nil {
		t.Fatalf("count migration records for %s: %v", version, err)
	}
	if count != want {
		t.Fatalf("migration %s record count = %d, want %d", version, count, want)
	}
}

func loadMigrationVersionSet(t *testing.T, db *gorm.DB) map[string]struct{} {
	t.Helper()
	var versions []string
	if err := db.Model(&storage.SchemaMigration{}).Order("version").Pluck("version", &versions).Error; err != nil {
		t.Fatalf("load schema migration versions: %v", err)
	}
	result := make(map[string]struct{}, len(versions))
	for _, version := range versions {
		result[version] = struct{}{}
	}
	return result
}

func assertOnlyMigrationVersionsAdded(
	t *testing.T,
	before map[string]struct{},
	after map[string]struct{},
	wantAdded ...string,
) {
	t.Helper()
	var added []string
	var removed []string
	for version := range after {
		if _, exists := before[version]; !exists {
			added = append(added, version)
		}
	}
	for version := range before {
		if _, exists := after[version]; !exists {
			removed = append(removed, version)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(wantAdded)
	if len(removed) != 0 || strings.Join(added, ",") != strings.Join(wantAdded, ",") {
		t.Fatalf("schema migration version delta added=%v removed=%v, want added=%v removed=[]", added, removed, wantAdded)
	}
}

func TestDatabaseAccountUniquenessMigrationAgainstMetadataDatabases(t *testing.T) {
	for _, tt := range metadataDatabaseCases() {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			db := openMetadataDatabase(t, tt)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("run versioned metadata migrations: %v", err)
			}
			assertDatabaseAccountInstanceUsernameUniqueness(t, db, tt.driver, "first", "a")
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("run versioned metadata migrations a second time: %v", err)
			}
			assertDatabaseAccountInstanceUsernameUniqueness(t, db, tt.driver, "second", "b")
		})
	}
}

func assertDatabaseAccountInstanceUsernameUniqueness(
	t *testing.T,
	db *gorm.DB,
	driver storage.Driver,
	prefix string,
	resourcePrefix string,
) {
	t.Helper()
	firstInstance := model.DatabaseInstance{
		ID: prefix + "-instance-one", Name: prefix + "-instance-one", Protocol: "mysql",
		Address: "127.0.0.1", Port: 3306, TLSMode: "disable", Status: "active",
	}
	secondInstance := model.DatabaseInstance{
		ID: prefix + "-instance-two", Name: prefix + "-instance-two", Protocol: "mysql",
		Address: "127.0.0.2", Port: 3306, TLSMode: "disable", Status: "active",
	}
	if err := db.Create(&firstInstance).Error; err != nil {
		t.Fatalf("create first database instance: %v", err)
	}
	if err := db.Create(&secondInstance).Error; err != nil {
		t.Fatalf("create second database instance: %v", err)
	}

	firstAccount := model.DatabaseAccount{
		ID: prefix + "-account-one", InstanceID: firstInstance.ID, UniqueName: prefix + "-account-one",
		Username: "reader", Status: "active", ResourceID: resourcePrefix + "001",
	}
	if err := db.Create(&firstAccount).Error; err != nil {
		t.Fatalf("create first database account: %v", err)
	}
	sameInstanceDifferentUsername := model.DatabaseAccount{
		ID: prefix + "-account-different-username", InstanceID: firstInstance.ID, UniqueName: prefix + "-account-different-username",
		Username: "writer", Status: "active", ResourceID: resourcePrefix + "002",
	}
	if err := db.Create(&sameInstanceDifferentUsername).Error; err != nil {
		t.Fatalf("different username on the same instance was rejected: %v", err)
	}
	duplicate := model.DatabaseAccount{
		ID: prefix + "-account-two", InstanceID: firstInstance.ID, UniqueName: prefix + "-account-two",
		Username: "reader", Status: "active", ResourceID: resourcePrefix + "003",
	}
	err := db.Create(&duplicate).Error
	if err == nil {
		t.Fatal("same-instance duplicate database account was accepted")
	}
	assertDatabaseAccountUniqueViolation(t, driver, err)
	differentInstance := model.DatabaseAccount{
		ID: prefix + "-account-three", InstanceID: secondInstance.ID, UniqueName: prefix + "-account-three",
		Username: "reader", Status: "active", ResourceID: resourcePrefix + "004",
	}
	if err := db.Create(&differentInstance).Error; err != nil {
		t.Fatalf("same username on another instance was rejected: %v", err)
	}
}

func assertDatabaseAccountUniqueIndexColumns(t *testing.T, db *gorm.DB, driver storage.Driver) {
	t.Helper()
	var columns []string
	switch driver {
	case storage.DriverMySQL:
		if err := db.Raw(`
			SELECT column_name
			FROM information_schema.statistics
			WHERE table_schema = DATABASE()
				AND table_name = 'database_accounts'
				AND index_name = 'uidx_database_accounts_instance_username'
				AND non_unique = 0
			ORDER BY seq_in_index
		`).Scan(&columns).Error; err != nil {
			t.Fatalf("load MySQL database account unique index columns: %v", err)
		}
	case storage.DriverPostgres:
		if err := db.Raw(`
			SELECT pg_get_indexdef(indexes.indexrelid, positions.position, true) AS column_name
			FROM pg_index AS indexes
			JOIN pg_class AS index_rel ON index_rel.oid = indexes.indexrelid
			JOIN pg_class AS table_rel ON table_rel.oid = indexes.indrelid
			JOIN pg_namespace AS namespaces ON namespaces.oid = table_rel.relnamespace
			CROSS JOIN LATERAL generate_series(1, indexes.indnkeyatts) AS positions(position)
			WHERE namespaces.nspname = current_schema()
				AND table_rel.relname = 'database_accounts'
				AND index_rel.relname = 'uidx_database_accounts_instance_username'
				AND indexes.indisunique
			ORDER BY positions.position
		`).Scan(&columns).Error; err != nil {
			t.Fatalf("load PostgreSQL database account unique index columns: %v", err)
		}
	default:
		t.Fatalf("unsupported metadata database driver %q", driver)
	}
	want := []string{"instance_id", "username"}
	if len(columns) != len(want) || columns[0] != want[0] || columns[1] != want[1] {
		t.Fatalf("database account unique index columns = %v, want %v", columns, want)
	}
}

func assertDatabaseAccountUniqueViolation(t *testing.T, driver storage.Driver, err error) {
	t.Helper()
	switch driver {
	case storage.DriverMySQL:
		var mysqlErr *mysqlDriver.MySQLError
		if !errors.As(err, &mysqlErr) {
			t.Fatalf("same-instance duplicate error type = %T, want *mysql.MySQLError: %v", err, err)
		}
		if mysqlErr.Number != 1062 {
			t.Fatalf("same-instance duplicate MySQL error number = %d, want 1062: %v", mysqlErr.Number, err)
		}
		if !strings.Contains(mysqlErr.Message, "uidx_database_accounts_instance_username") {
			t.Fatalf("same-instance duplicate violated unexpected MySQL index: %v", err)
		}
	case storage.DriverPostgres:
		var postgresErr *pgconn.PgError
		if !errors.As(err, &postgresErr) {
			t.Fatalf("same-instance duplicate error type = %T, want *pgconn.PgError: %v", err, err)
		}
		if postgresErr.Code != "23505" {
			t.Fatalf("same-instance duplicate PostgreSQL error code = %q, want %q: %v", postgresErr.Code, "23505", err)
		}
		if postgresErr.ConstraintName != "uidx_database_accounts_instance_username" {
			t.Fatalf("same-instance duplicate violated PostgreSQL constraint %q, want uidx_database_accounts_instance_username: %v", postgresErr.ConstraintName, err)
		}
	default:
		t.Fatalf("unsupported metadata database driver %q", driver)
	}
}
