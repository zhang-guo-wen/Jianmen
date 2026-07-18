//go:build integration

package integration

import (
	"testing"

	"gorm.io/gorm"

	"jianmen/internal/crypto"
	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestDatabaseStorageMigrationUpgradesProvisioningSaga(t *testing.T) {
	for _, databaseCase := range metadataDatabaseCases() {
		databaseCase := databaseCase
		for _, fixture := range []struct {
			name        string
			setup       func(*testing.T, *gorm.DB, storage.Driver)
			wantAddedAt string
		}{
			{
				name:        "empty",
				setup:       func(*testing.T, *gorm.DB, storage.Driver) {},
				wantAddedAt: currentStorageMigrationVersions[0],
			},
			{
				name: "before_006",
				setup: func(t *testing.T, db *gorm.DB, driver storage.Driver) {
					createLegacyProvisioningBaseSchema(t, db, driver, false)
					seedAppliedMigrations(t, db, migrationVersionsBefore(t, databaseInstanceTLSMigrationVersion)...)
				},
				wantAddedAt: databaseInstanceTLSMigrationVersion,
			},
			{
				name: "before_008",
				setup: func(t *testing.T, db *gorm.DB, driver storage.Driver) {
					createLegacyProvisioningBaseSchema(t, db, driver, true)
					seedAppliedMigrations(t, db, migrationVersionsBefore(t, databaseAccountUniquenessMigrationVersion)...)
				},
				wantAddedAt: databaseAccountUniquenessMigrationVersion,
			},
		} {
			fixture := fixture
			t.Run(databaseCase.name+"/"+fixture.name, func(t *testing.T) {
				if _, err := crypto.Init(t.TempDir()); err != nil {
					t.Fatalf("initialize crypto: %v", err)
				}
				db := openMetadataDatabase(t, databaseCase)
				fixture.setup(t, db, databaseCase.driver)
				before := loadMigrationVersionSet(t, db)
				if err := storage.Migrate(db); err != nil {
					t.Fatalf("upgrade provisioning saga schema: %v", err)
				}
				assertOnlyMigrationVersionsAdded(t, before, loadMigrationVersionSet(t, db), migrationVersionsFrom(t, fixture.wantAddedAt)...)
				assertProvisioningSagaSchema(t, db, databaseCase.driver)

				beforeSecondMigration := loadMigrationVersionSet(t, db)
				if err := storage.Migrate(db); err != nil {
					t.Fatalf("upgrade provisioning saga schema a second time: %v", err)
				}
				assertOnlyMigrationVersionsAdded(t, beforeSecondMigration, loadMigrationVersionSet(t, db))
				assertMigrationRecord(t, db, databaseProvisioningSagaMigrationVersion, "database provisioning saga recovery state")
			})
		}
	}
}

func createLegacyProvisioningBaseSchema(t *testing.T, db *gorm.DB, driver storage.Driver, hasTLS bool) {
	t.Helper()
	timeType := "DATETIME(3)"
	if driver == storage.DriverPostgres {
		timeType = "TIMESTAMP(3)"
	}
	tlsColumns := ""
	if hasTLS {
		tlsColumns = ", tls_mode VARCHAR(16) NOT NULL DEFAULT 'verify-full', tls_server_name VARCHAR(255), tls_ca_pem TEXT"
	}
	if err := db.Exec(`CREATE TABLE database_instances (
		id VARCHAR(64) PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		protocol VARCHAR(32) NOT NULL DEFAULT 'mysql',
		address VARCHAR(255) NOT NULL,
		port INTEGER NOT NULL DEFAULT 3306` + tlsColumns + `,
		group_name VARCHAR(128),
		remark TEXT,
		status VARCHAR(32) NOT NULL DEFAULT 'active',
		created_at ` + timeType + `,
		updated_at ` + timeType + `,
		CONSTRAINT uni_database_instances_name UNIQUE (name)
	)`).Error; err != nil {
		t.Fatalf("create legacy database instances: %v", err)
	}
	if err := db.Exec(`CREATE TABLE database_accounts (
		id VARCHAR(64) PRIMARY KEY,
		instance_id VARCHAR(64) NOT NULL,
		unique_name VARCHAR(128) NOT NULL,
		username VARCHAR(128) NOT NULL,
		password TEXT,
		group_name VARCHAR(128),
		remark TEXT,
		expires_at ` + timeType + `,
		status VARCHAR(32) NOT NULL DEFAULT 'active',
		resource_seq INTEGER NOT NULL DEFAULT 0,
		resource_id VARCHAR(4),
		created_at ` + timeType + `,
		updated_at ` + timeType + `,
		CONSTRAINT uni_database_accounts_unique_name UNIQUE (unique_name),
		CONSTRAINT uni_database_accounts_resource_id UNIQUE (resource_id),
		CONSTRAINT fk_database_accounts_instance FOREIGN KEY (instance_id) REFERENCES database_instances(id) ON UPDATE CASCADE ON DELETE CASCADE
	)`).Error; err != nil {
		t.Fatalf("create legacy database accounts: %v", err)
	}
}

func migrationVersionsBefore(t *testing.T, firstPending string) []string {
	t.Helper()
	for index, version := range currentStorageMigrationVersions {
		if version == firstPending {
			return append([]string(nil), currentStorageMigrationVersions[:index]...)
		}
	}
	t.Fatalf("missing migration version %s", firstPending)
	return nil
}

func migrationVersionsFrom(t *testing.T, firstPending string) []string {
	t.Helper()
	for index, version := range currentStorageMigrationVersions {
		if version == firstPending {
			return append([]string(nil), currentStorageMigrationVersions[index:]...)
		}
	}
	t.Fatalf("missing migration version %s", firstPending)
	return nil
}

func assertProvisioningSagaSchema(t *testing.T, db *gorm.DB, driver storage.Driver) {
	t.Helper()
	if !db.Migrator().HasTable(&model.DatabaseProvisioningOperation{}) {
		t.Fatal("database_provisioning_operations table is missing")
	}
	for _, column := range []string{"managed", "upstream_host", "provisioning_operation_id"} {
		if !db.Migrator().HasColumn(&model.DatabaseAccount{}, column) {
			t.Fatalf("database accounts is missing provisioning column %s", column)
		}
	}
	for _, legacyColumn := range []string{
		"provisioning_admin_account_id",
		"provisioning_host",
		"provisioning_grants",
		"provisioning_stage",
		"provisioning_cleanup_status",
		"provisioning_last_error",
		"provisioning_attempt_count",
		"provisioning_last_attempt_at",
	} {
		if db.Migrator().HasColumn(&model.DatabaseAccount{}, legacyColumn) {
			t.Fatalf("database accounts retained obsolete provisioning column %s", legacyColumn)
		}
	}
	for _, index := range []struct {
		model any
		name  string
	}{
		{model: &model.DatabaseAccount{}, name: "uidx_database_accounts_provisioning_operation"},
		{model: &model.DatabaseAccount{}, name: "idx_database_accounts_managed_status"},
		{model: &model.DatabaseProvisioningOperation{}, name: "idx_database_provisioning_instance"},
		{model: &model.DatabaseProvisioningOperation{}, name: "idx_database_provisioning_work"},
		{model: &model.DatabaseProvisioningOperation{}, name: "uidx_database_provisioning_actor_kind_idempotency"},
		{model: &model.DatabaseProvisioningOperation{}, name: "uidx_database_provisioning_username"},
	} {
		if !db.Migrator().HasIndex(index.model, index.name) {
			t.Fatalf("provisioning schema is missing index %s", index.name)
		}
	}

	instance := model.DatabaseInstance{
		ID: "migration-instance", Name: "migration-instance", Protocol: "mysql", Address: "127.0.0.1", Port: 3306, Status: "active",
	}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create provisioning instance: %v", err)
	}
	admin := model.DatabaseAccount{
		ID: "migration-admin", InstanceID: instance.ID, UniqueName: "migration-admin", Username: "admin", ResourceID: "MA01", Status: "active",
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create provisioning administrator: %v", err)
	}
	key := "migration-key"
	operation := model.DatabaseProvisioningOperation{
		ID:                   "migration-operation",
		Kind:                 "create",
		InstanceID:           instance.ID,
		ActorID:              "migration-actor",
		IdempotencyKey:       &key,
		CanonicalRequestHash: "migration-hash",
		AdminAccountID:       admin.ID,
		UpstreamUsername:     "jm_migration",
		Password:             model.NewEncryptedField("migration-secret"),
		Host:                 "%",
		GrantsJSON:           "[]",
	}
	if err := db.Create(&operation).Error; err != nil {
		t.Fatalf("create provisioning operation: %v", err)
	}
	var persisted model.DatabaseProvisioningOperation
	if err := db.First(&persisted, "id = ?", operation.ID).Error; err != nil {
		t.Fatalf("load provisioning operation: %v", err)
	}
	if persisted.Stage != "reserved" || persisted.CleanupStatus != "none" || persisted.AttemptCount != 0 || persisted.Revision != 1 {
		t.Fatalf("provisioning defaults = %#v", persisted)
	}

	operationID := operation.ID
	managed := model.DatabaseAccount{
		ID:                      "migration-managed",
		InstanceID:              instance.ID,
		UniqueName:              "migration-managed",
		Username:                operation.UpstreamUsername,
		ResourceID:              "MA02",
		Status:                  "active",
		Managed:                 true,
		UpstreamHost:            operation.Host,
		ProvisioningOperationID: &operationID,
	}
	if err := db.Create(&managed).Error; err != nil {
		t.Fatalf("create managed account: %v", err)
	}

	duplicateKey := operation
	duplicateKey.ID = "migration-operation-duplicate-key"
	duplicateKey.UpstreamUsername = "jm_migration_duplicate_key"
	if err := db.Create(&duplicateKey).Error; err == nil {
		t.Fatal("database accepted duplicate actor/kind/idempotency key")
	}
	duplicateUsername := operation
	duplicateUsername.ID = "migration-operation-duplicate-username"
	duplicateUsername.ActorID = "migration-actor-two"
	secondKey := "migration-key-two"
	duplicateUsername.IdempotencyKey = &secondKey
	if err := db.Create(&duplicateUsername).Error; err == nil {
		t.Fatal("database accepted duplicate upstream username")
	}
	for _, invalid := range []struct {
		name   string
		change func(*model.DatabaseProvisioningOperation)
	}{
		{name: "stage", change: func(value *model.DatabaseProvisioningOperation) { value.Stage = "invalid" }},
		{name: "cleanup", change: func(value *model.DatabaseProvisioningOperation) { value.CleanupStatus = "invalid" }},
		{name: "attempt", change: func(value *model.DatabaseProvisioningOperation) { value.AttemptCount = -1 }},
		{name: "idempotency", change: func(value *model.DatabaseProvisioningOperation) { empty := " "; value.IdempotencyKey = &empty }},
	} {
		invalid := invalid
		candidate := operation
		candidate.ID = "migration-invalid-" + invalid.name
		candidate.UpstreamUsername = "jm_invalid_" + invalid.name
		candidate.ActorID = "migration-invalid-" + invalid.name
		candidateKey := "migration-invalid-key-" + invalid.name
		candidate.IdempotencyKey = &candidateKey
		invalid.change(&candidate)
		if err := db.Create(&candidate).Error; err == nil {
			t.Fatalf("database accepted invalid provisioning %s", invalid.name)
		}
	}
	if err := db.Exec("UPDATE database_provisioning_operations SET revision = ? WHERE id = ?", 0, operation.ID).Error; err == nil {
		t.Fatal("database accepted invalid provisioning revision")
	}
	invalidManaged := model.DatabaseAccount{
		ID: "migration-invalid-managed", InstanceID: instance.ID, UniqueName: "migration-invalid-managed", Username: "invalid-managed", ResourceID: "MA03", Status: "active", Managed: true,
	}
	if err := db.Create(&invalidManaged).Error; err == nil {
		t.Fatal("database accepted incomplete managed account")
	}
	missingInstance := operation
	missingInstance.ID = "migration-missing-instance"
	missingInstance.InstanceID = "missing-instance"
	missingInstance.UpstreamUsername = "jm_missing_instance"
	missingInstance.ActorID = "migration-missing-instance"
	missingInstanceKey := "migration-missing-instance-key"
	missingInstance.IdempotencyKey = &missingInstanceKey
	if err := db.Create(&missingInstance).Error; err == nil {
		t.Fatal("database accepted operation without referenced instance")
	}
	missingAdmin := operation
	missingAdmin.ID = "migration-missing-admin"
	missingAdmin.AdminAccountID = "missing-admin"
	missingAdmin.UpstreamUsername = "jm_missing_admin"
	missingAdmin.ActorID = "migration-missing-admin"
	missingAdminKey := "migration-missing-admin-key"
	missingAdmin.IdempotencyKey = &missingAdminKey
	if err := db.Create(&missingAdmin).Error; err == nil {
		t.Fatal("database accepted operation without referenced administrator")
	}

	for _, removal := range []struct {
		name  string
		value any
	}{
		{name: "operation", value: &operation},
		{name: "administrator", value: &admin},
		{name: "instance", value: &instance},
	} {
		if err := db.Delete(removal.value).Error; err == nil {
			t.Fatalf("%s deletion bypassed RESTRICT lifecycle constraint on %s", driver, removal.name)
		}
	}
}
