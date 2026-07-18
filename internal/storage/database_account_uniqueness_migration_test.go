package storage

import (
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

const databaseAccountUniquenessMigrationVersion = "202607180008"

func openLegacyDatabaseAccountSchema(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&SchemaMigration{}); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	if err := db.AutoMigrate(&model.DatabaseInstance{}); err != nil {
		t.Fatalf("create database instance table: %v", err)
	}
	for _, instance := range []model.DatabaseInstance{
		{ID: "instance-1", Name: "instance-1", Address: "127.0.0.1", Status: "active"},
		{ID: "instance-2", Name: "instance-2", Address: "127.0.0.2", Status: "active"},
	} {
		if err := db.Create(&instance).Error; err != nil {
			t.Fatalf("create database instance %s: %v", instance.ID, err)
		}
	}
	if err := db.Exec(`CREATE TABLE database_accounts (
		id text primary key,
		instance_id text not null,
		unique_name text not null,
		username text not null,
		password text,
		group_name text,
		remark text,
		expires_at datetime,
		status text not null,
		resource_seq integer not null,
		resource_id text,
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create previous database account schema: %v", err)
	}
	for _, migration := range migrations {
		if migration.Version == databaseAccountUniquenessMigrationVersion ||
			migration.Version == databaseProvisioningSagaMigrationVersion {
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
	return db
}

func TestDatabaseAccountUniquenessMigrationUpgradesExistingSchema(t *testing.T) {
	db := openLegacyDatabaseAccountSchema(t)

	if db.Migrator().HasIndex(&model.DatabaseAccount{}, "uidx_database_accounts_instance_username") {
		t.Fatal("database account unique index unexpectedly existed before migration")
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate existing database: %v", err)
	}
	if !db.Migrator().HasIndex(&model.DatabaseAccount{}, "uidx_database_accounts_instance_username") {
		t.Fatal("database account unique index is missing after migration")
	}

	first := model.DatabaseAccount{
		ID:         "account-1",
		InstanceID: "instance-1",
		UniqueName: "account-one",
		Username:   "reader",
		Status:     "active",
		ResourceID: "a001",
	}
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("insert first account: %v", err)
	}
	duplicate := model.DatabaseAccount{
		ID:         "account-2",
		InstanceID: "instance-1",
		UniqueName: "account-two",
		Username:   "reader",
		Status:     "active",
		ResourceID: "a002",
	}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("same-instance duplicate database account was accepted")
	}
	differentInstance := model.DatabaseAccount{
		ID:         "account-3",
		InstanceID: "instance-2",
		UniqueName: "account-three",
		Username:   "reader",
		Status:     "active",
		ResourceID: "a003",
	}
	if err := db.Create(&differentInstance).Error; err != nil {
		t.Fatalf("same username on different instance was rejected: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var migrationCount int64
	if err := db.Model(&SchemaMigration{}).
		Where("version = ?", databaseAccountUniquenessMigrationVersion).
		Count(&migrationCount).Error; err != nil {
		t.Fatalf("count migration records: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("migration record count = %d, want 1", migrationCount)
	}
	if !db.Migrator().HasIndex(&model.DatabaseAccount{}, "uidx_database_accounts_instance_username") {
		t.Fatal("database account unique index is missing after second migration")
	}
	duplicateAfterSecondMigration := model.DatabaseAccount{
		ID:         "account-4",
		InstanceID: "instance-1",
		UniqueName: "account-four",
		Username:   "reader",
		Status:     "active",
		ResourceID: "a004",
	}
	if err := db.Create(&duplicateAfterSecondMigration).Error; err == nil {
		t.Fatal("same-instance duplicate was accepted after second migration")
	}
}

func TestDatabaseAccountUniquenessMigrationDoesNotInstallFutureProvisioningSchema(t *testing.T) {
	db := openLegacyDatabaseAccountSchema(t)

	var migration Migration
	for _, candidate := range migrations {
		if candidate.Version == databaseAccountUniquenessMigrationVersion {
			migration = candidate
			break
		}
	}
	if migration.Run == nil {
		t.Fatalf("missing migration %s", databaseAccountUniquenessMigrationVersion)
	}
	if err := db.Transaction(migration.Run); err != nil {
		t.Fatalf("run 008 migration directly: %v", err)
	}
	for _, column := range []string{"managed", "upstream_host", "provisioning_operation_id"} {
		if db.Migrator().HasColumn(&model.DatabaseAccount{}, column) {
			t.Fatalf("008 migration unexpectedly installed 009 database account column %q", column)
		}
	}
	if db.Migrator().HasTable(&model.DatabaseProvisioningOperation{}) {
		t.Fatal("008 migration unexpectedly installed the 009 provisioning operation table")
	}
}

func TestCoreMetadataMigrationDefersAccountUsernameUniquenessUntil008(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	var coreMigration Migration
	var uniquenessMigration Migration
	for _, migration := range migrations {
		switch migration.Version {
		case "202606290002":
			coreMigration = migration
		case databaseAccountUniquenessMigrationVersion:
			uniquenessMigration = migration
		}
	}
	if coreMigration.Run == nil || uniquenessMigration.Run == nil {
		t.Fatal("required versioned migrations are missing")
	}
	if err := db.Transaction(coreMigration.Run); err != nil {
		t.Fatalf("run core metadata migration: %v", err)
	}
	if db.Migrator().HasIndex(&model.DatabaseAccount{}, "uidx_database_accounts_instance_username") {
		t.Fatal("core metadata migration installed the later 008 account uniqueness index")
	}
	if err := db.Transaction(uniquenessMigration.Run); err != nil {
		t.Fatalf("run account uniqueness migration: %v", err)
	}
	if !db.Migrator().HasIndex(&model.DatabaseAccount{}, "uidx_database_accounts_instance_username") {
		t.Fatal("008 account uniqueness migration did not create its index")
	}
}

func TestDatabaseAccountUniquenessMigrationRejectsLegacyDuplicates(t *testing.T) {
	db := openLegacyDatabaseAccountSchema(t)
	const credential = "credential-secret-marker"
	if err := db.Exec(`INSERT INTO database_accounts
		(id, instance_id, unique_name, username, password, status, resource_seq, resource_id)
		VALUES
		(?, ?, ?, ?, ?, ?, ?, ?),
		(?, ?, ?, ?, ?, ?, ?, ?)`,
		"account-1", "instance-1", "account-one", "reader", credential, "active", 1, "a001",
		"account-2", "instance-1", "account-two", "reader", credential, "active", 2, "a002",
	).Error; err != nil {
		t.Fatalf("seed duplicate database accounts: %v", err)
	}

	err := Migrate(db)
	if err == nil {
		t.Fatal("migration accepted legacy duplicate database accounts")
	}
	message := err.Error()
	for _, fragment := range []string{
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
	var migrationCount int64
	if err := db.Model(&SchemaMigration{}).
		Where("version = ?", databaseAccountUniquenessMigrationVersion).
		Count(&migrationCount).Error; err != nil {
		t.Fatalf("count migration records: %v", err)
	}
	if migrationCount != 0 {
		t.Fatalf("failed migration record count = %d, want 0", migrationCount)
	}
}
