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

	"jianmen/internal/crypto"
	"jianmen/internal/model"
	"jianmen/internal/storage"
)

const (
	databaseInstanceTLSMigrationVersion       = "202607180006"
	databaseAccountUniquenessMigrationVersion = "202607180008"
	databaseProvisioningSagaMigrationVersion  = "202607180009"
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
}

func seedAppliedMigrations(t *testing.T, db *gorm.DB, versions ...string) {
	t.Helper()
	names := map[string]string{
		"202606290001": "prepare metadata sequences",
		"202606290002": "core metadata schema",
		"202606290003": "reconcile metadata resources",
		"202606290004": "global compact session identity",
		"202606290005": "metadata query indexes",
		"202607130001": "user groups and resource grants",
		"202607160001": "AI access tokens",
		"202607160002": "encrypted AI token values",
		"202607170001": "container management endpoints",
		"202607170002": "user expiry and temporary authorization metadata",
		"202607180001": "database backed super administrator identity",
		"202607180002": "temporary access connection password lifecycle",
		"202607180003": "atomic system initialization guard",
		"202607180004": "browser sessions and websocket tickets",
		"202607180005": "remove reversible AI token secrets",
		"202607180006": "database instance upstream TLS policy",
		"202607180007": "permission logical uniqueness",
		"202607180008": "database account instance username uniqueness",
		"202607180009": "database provisioning saga recovery state",
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
	if instance.TLSMode != "verify-full" {
		t.Fatalf("migrated TLS mode = %q, want verify-full", instance.TLSMode)
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
	if defaults.TLSMode != "verify-full" || defaults.TLSServerName.Valid || defaults.TLSCAPEM.Valid {
		t.Fatalf("database-level TLS defaults = %#v, want verify-full and NULL optional values", defaults)
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
