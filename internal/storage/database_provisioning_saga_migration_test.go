package storage

import (
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	gormmysql "gorm.io/driver/mysql"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"jianmen/internal/crypto"
	"jianmen/internal/model"
)

const databaseProvisioningSagaMigrationVersion = "202607180009"

func TestDatabaseProvisioningSagaMigrationCreatesConstrainedOperationTable(t *testing.T) {
	if _, err := crypto.Init(t.TempDir()); err != nil {
		t.Fatalf("initialize crypto: %v", err)
	}
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&SchemaMigration{},
		&model.DatabaseInstance{},
		&model.DatabaseAccount{},
	); err != nil {
		t.Fatalf("create pre-009 schema: %v", err)
	}
	for _, migration := range migrations {
		if migration.Version == databaseProvisioningSagaMigrationVersion {
			continue
		}
		if err := db.Create(&SchemaMigration{
			Version: migration.Version, Name: migration.Name, AppliedAt: time.Now().UTC(),
		}).Error; err != nil {
			t.Fatalf("record previous migration %s: %v", migration.Version, err)
		}
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate provisioning saga schema: %v", err)
	}
	if !db.Migrator().HasTable(&model.DatabaseProvisioningOperation{}) {
		t.Fatal("database_provisioning_operations table was not created")
	}
	for _, column := range []string{
		"id", "instance_id", "admin_account_id", "upstream_username", "password",
		"host", "grants_json", "stage", "cleanup_status", "last_error",
		"attempt_count", "last_attempt_at", "revision", "lease_owner",
		"lease_token", "lease_expires_at",
	} {
		if !db.Migrator().HasColumn(&model.DatabaseProvisioningOperation{}, column) {
			t.Fatalf("provisioning operation is missing column %q", column)
		}
	}
	for _, index := range []string{
		"idx_database_provisioning_instance",
		"idx_database_provisioning_work",
		"uidx_database_provisioning_username",
	} {
		if !db.Migrator().HasIndex(&model.DatabaseProvisioningOperation{}, index) {
			var actual []string
			if err := db.Raw(`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'database_provisioning_operations' ORDER BY name`).Scan(&actual).Error; err != nil {
				t.Fatalf("list provisioning indexes: %v", err)
			}
			t.Fatalf("provisioning operation is missing index %q; actual indexes=%v", index, actual)
		}
	}

	instance := model.DatabaseInstance{
		Name: "orders", Protocol: "mysql", Address: "127.0.0.1", Status: "active",
	}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create instance: %v", err)
	}
	admin := model.DatabaseAccount{
		ID: "admin-1", InstanceID: instance.ID, UniqueName: "admin-1", Username: "admin", ResourceID: "A001", Status: "active",
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create administrator account: %v", err)
	}
	valid := model.DatabaseProvisioningOperation{
		ID: "jmo_validoperation0001", InstanceID: instance.ID,
		AdminAccountID: "admin-1", UpstreamUsername: "jm_validoperation0001",
		Password: model.NewEncryptedField("secret"), Host: "10.0.0.8",
		GrantsJSON: `[{"database":"orders","privilege":"read"}]`,
	}
	if err := db.Create(&valid).Error; err != nil {
		t.Fatalf("insert operation with defaults: %v", err)
	}
	if valid.Stage != "reserved" || valid.CleanupStatus != "none" ||
		valid.LastError != "" || valid.AttemptCount != 0 || valid.Revision != 1 {
		t.Fatalf("unexpected provisioning defaults: %#v", valid)
	}

	invalidStage := valid
	invalidStage.ID = "jmo_invalidstage00001"
	invalidStage.UpstreamUsername = "jm_invalidstage00001"
	invalidStage.Stage = "drop_any_identity"
	if err := db.Create(&invalidStage).Error; err == nil {
		t.Fatal("invalid provisioning stage bypassed database constraint")
	}
	invalidCleanup := valid
	invalidCleanup.ID = "jmo_invalidcleanup001"
	invalidCleanup.UpstreamUsername = "jm_invalidcleanup001"
	invalidCleanup.CleanupStatus = "ignore_failure"
	if err := db.Create(&invalidCleanup).Error; err == nil {
		t.Fatal("invalid cleanup status bypassed database constraint")
	}
	invalidRevision := valid
	invalidRevision.ID = "jmo_invalidrevision001"
	invalidRevision.UpstreamUsername = "jm_invalidrevision001"
	invalidRevision.Revision = -1
	if err := db.Create(&invalidRevision).Error; err == nil {
		t.Fatal("invalid fencing revision bypassed database constraint")
	}
	if err := db.Exec(
		`INSERT INTO database_provisioning_operations
			(id, instance_id, admin_account_id, upstream_username, password, grants_json, remark)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"jmo_missinghost000001",
		instance.ID,
		"admin-1",
		"jm_missinghost000001",
		"encrypted-placeholder",
		`[{"database":"orders","privilege":"read"}]`,
		"",
	).Error; err == nil {
		t.Fatal("missing required host bypassed NOT NULL constraint")
	}
}

func TestDatabaseProvisioningSagaMigrationKeepsLifecycleLinksRestricted(t *testing.T) {
	if _, err := crypto.Init(t.TempDir()); err != nil {
		t.Fatalf("initialize crypto: %v", err)
	}
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&SchemaMigration{},
		&model.DatabaseInstance{},
		&model.DatabaseAccount{},
	); err != nil {
		t.Fatalf("create pre-009 schema: %v", err)
	}
	for _, migration := range migrations {
		if migration.Version == databaseProvisioningSagaMigrationVersion {
			continue
		}
		if err := db.Create(&SchemaMigration{
			Version: migration.Version, Name: migration.Name, AppliedAt: time.Now().UTC(),
		}).Error; err != nil {
			t.Fatalf("record previous migration %s: %v", migration.Version, err)
		}
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate provisioning saga schema: %v", err)
	}

	instance := model.DatabaseInstance{
		ID: "instance-restricted", Name: "restricted", Protocol: "mysql", Address: "127.0.0.1", Status: "active",
	}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create instance: %v", err)
	}
	admin := model.DatabaseAccount{
		ID: "account-admin", InstanceID: instance.ID, UniqueName: "admin", Username: "admin", ResourceID: "A001", Status: "active",
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create administrator account: %v", err)
	}
	key := "restricted-create"
	operation := model.DatabaseProvisioningOperation{
		ID:                   "operation-restricted",
		Kind:                 "create",
		InstanceID:           instance.ID,
		ActorID:              "actor-restricted",
		IdempotencyKey:       &key,
		CanonicalRequestHash: "hash-restricted",
		AdminAccountID:       admin.ID,
		UpstreamUsername:     "jm_restricted",
		Password:             model.NewEncryptedField("secret"),
		Host:                 "%",
		GrantsJSON:           "[]",
	}
	if err := db.Create(&operation).Error; err != nil {
		t.Fatalf("create provisioning operation: %v", err)
	}
	managedOperationID := operation.ID
	managed := model.DatabaseAccount{
		ID:                      "account-managed",
		InstanceID:              instance.ID,
		UniqueName:              "managed",
		Username:                operation.UpstreamUsername,
		ResourceID:              "A002",
		Status:                  "active",
		Managed:                 true,
		UpstreamHost:            operation.Host,
		ProvisioningOperationID: &managedOperationID,
	}
	if err := db.Create(&managed).Error; err != nil {
		t.Fatalf("create managed account: %v", err)
	}

	for _, removal := range []struct {
		name  string
		value any
	}{
		{name: "operation from managed account", value: &operation},
		{name: "administrator from operation", value: &admin},
		{name: "instance from operation", value: &instance},
	} {
		if err := db.Delete(removal.value).Error; err == nil {
			t.Fatalf("delete %s succeeded despite required RESTRICT relation", removal.name)
		}
	}
}

func TestDatabaseProvisioningSagaModelDeclaresPortableSafetyConstraints(t *testing.T) {
	operationType := model.DatabaseProvisioningOperation{}
	for _, dialect := range []Driver{DriverSQLite, DriverMySQL, DriverPostgres} {
		contract, err := databaseProvisioningSchemaContract(dialect, operationType)
		if err != nil {
			t.Fatalf("%s provisioning schema contract: %v", dialect, err)
		}
		if !contract.NotNull || !contract.Defaults || !contract.Checks || !contract.Indexes {
			t.Fatalf("%s provisioning schema contract is incomplete: %#v", dialect, contract)
		}
	}
	for _, dialect := range []Driver{DriverMySQL, DriverPostgres} {
		ddl := databaseProvisioningDryRunDDL(t, dialect)
		for _, required := range []string{
			"not null",
			"default",
			"chk_database_provisioning_stage",
			"chk_database_provisioning_cleanup",
			"idx_database_provisioning_work",
			"idx_dpo_upstream_username_active",
		} {
			if !strings.Contains(strings.ToLower(ddl), required) {
				t.Fatalf("%s provisioning DDL is missing %q:\n%s", dialect, required, ddl)
			}
		}
	}
}

func databaseProvisioningDryRunDDL(t *testing.T, dialect Driver) string {
	t.Helper()
	var output strings.Builder
	logWriter := log.New(&output, "", 0)
	config := &gorm.Config{
		DryRun: true, DisableAutomaticPing: true,
		Logger: gormlogger.New(logWriter, gormlogger.Config{LogLevel: gormlogger.Info}),
	}
	var dialector gorm.Dialector
	switch dialect {
	case DriverMySQL:
		dialector = gormmysql.New(gormmysql.Config{
			DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8mb4&parseTime=True",
			SkipInitializeWithVersion: true,
		})
	case DriverPostgres:
		dialector = gormpostgres.New(gormpostgres.Config{
			DSN:                  "host=localhost user=gorm password=gorm dbname=gorm port=9920 sslmode=disable",
			PreferSimpleProtocol: true,
		})
	default:
		t.Fatalf("unsupported dry-run dialect %q", dialect)
	}
	db, err := gorm.Open(dialector, config)
	if err != nil {
		t.Fatalf("open %s dry-run database: %v", dialect, err)
	}
	if err := db.Migrator().CreateTable(&model.DatabaseProvisioningOperation{}); err != nil {
		t.Fatalf("create %s provisioning DDL: %v", dialect, err)
	}
	return output.String()
}

type provisioningSchemaSafetyContract struct {
	NotNull  bool
	Defaults bool
	Checks   bool
	Indexes  bool
}

func databaseProvisioningSchemaContract(
	dialect Driver,
	operation model.DatabaseProvisioningOperation,
) (provisioningSchemaSafetyContract, error) {
	switch dialect {
	case DriverSQLite, DriverMySQL, DriverPostgres:
	default:
		return provisioningSchemaSafetyContract{}, errors.New("unsupported dialect")
	}
	parsed, err := schema.Parse(&operation, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		return provisioningSchemaSafetyContract{}, err
	}
	requiredNotNull := []string{
		"InstanceID", "AdminAccountID", "UpstreamUsername", "Password", "Host",
		"GrantsJSON", "Stage", "CleanupStatus", "LastError", "AttemptCount",
		"Revision", "LeaseOwner", "LeaseToken",
	}
	notNull := true
	for _, name := range requiredNotNull {
		if field := parsed.LookUpField(name); field == nil || !field.NotNull {
			notNull = false
		}
	}
	defaults := parsed.LookUpField("Stage").DefaultValue == "reserved" &&
		parsed.LookUpField("CleanupStatus").DefaultValue == "none" &&
		parsed.LookUpField("AttemptCount").DefaultValue == "0" &&
		parsed.LookUpField("Revision").DefaultValue == "1"
	checks := parsed.ParseCheckConstraints()
	hasChecks := checks["chk_database_provisioning_stage"].Constraint != "" &&
		checks["chk_database_provisioning_cleanup"].Constraint != "" &&
		checks["chk_database_provisioning_revision"].Constraint != ""
	indexNames := map[string]bool{}
	for _, index := range parsed.ParseIndexes() {
		indexNames[index.Name] = true
	}
	return provisioningSchemaSafetyContract{
		NotNull:  notNull,
		Defaults: defaults,
		Checks:   hasChecks,
		Indexes: indexNames["idx_database_provisioning_instance"] &&
			indexNames["idx_database_provisioning_work"] &&
			indexNames["idx_dpo_actor_kind_idem_active"] &&
			indexNames["idx_dpo_upstream_username_active"],
	}, nil
}
