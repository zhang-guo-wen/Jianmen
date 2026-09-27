package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestDatabaseProvisioningLifecycleModelFields(t *testing.T) {
	accountType := reflect.TypeFor[DatabaseAccount]()
	assertModelField(t, accountType, "Managed", reflect.TypeFor[bool](), "json:-")
	assertModelField(t, accountType, "UpstreamHost", reflect.TypeFor[string](), "json:-")
	assertModelField(t, accountType, "ProvisioningOperationID", reflect.TypeFor[*string](), "json:-")
	assertModelField(t, accountType, "ProvisioningOperation", reflect.TypeFor[*DatabaseProvisioningOperation](), "json:-")
	provisioningOperationField, exists := accountType.FieldByName("ProvisioningOperation")
	if !exists {
		t.Fatal("DatabaseAccount.ProvisioningOperation is missing")
	}
	provisioningConstraint := strings.ToLower(provisioningOperationField.Tag.Get("gorm"))
	if !strings.Contains(provisioningConstraint, "onupdate:restrict") ||
		strings.Contains(provisioningConstraint, "onupdate:cascade") {
		t.Fatalf(
			"DatabaseAccount.ProvisioningOperation must restrict key updates: %q",
			provisioningOperationField.Tag.Get("gorm"),
		)
	}

	operationType := reflect.TypeFor[DatabaseProvisioningOperation]()
	remarkField, exists := operationType.FieldByName("Remark")
	if !exists {
		t.Fatal("DatabaseProvisioningOperation.Remark is missing")
	}
	if strings.Contains(strings.ToLower(remarkField.Tag.Get("gorm")), "default") {
		t.Fatalf(
			"DatabaseProvisioningOperation.Remark must not declare a database default: %q",
			remarkField.Tag.Get("gorm"),
		)
	}
	// FullAudit 嵌入的字段（CreatedAt/UpdatedAt 有 json tag），不在此测试中校验
	fullAuditFields := map[string]bool{
		"CreatedBy": true, "UpdatedBy": true,
		"CreatedAt": true, "UpdatedAt": true, "ActiveMarker": true,
	}
	for _, name := range []string{
		"Kind", "InstanceID", "ActorID", "IdempotencyKey", "CanonicalRequestHash",
		"AdminAccountID", "UpstreamUsername", "Password", "Host", "GrantsJSON", "GroupName",
		"Remark", "ExpiresAt", "Stage", "CleanupStatus", "TerminalAt", "ActiveRetainedAt",
		"LastError", "AttemptCount", "LastAttemptAt", "Revision", "LeaseOwner", "LeaseToken",
		"LeaseExpiresAt", "Instance",
	} {
		if fullAuditFields[name] {
			continue
		}
		assertModelJSONTag(t, operationType, name, "-")
	}
	if _, ok := operationType.FieldByName("ManagedAccountID"); ok {
		t.Fatal("ManagedAccountID must not exist; the account-side operation ID is authoritative")
	}
	if _, ok := operationType.FieldByName("ManagedAccount"); ok {
		t.Fatal("ManagedAccount must not exist; the account-side operation ID is authoritative")
	}
}

func TestDatabaseProvisioningOperationJSONDoesNotExposeSecretsOrFencingState(t *testing.T) {
	payload, err := json.Marshal(DatabaseProvisioningOperation{
		ID:                   "operation-id",
		ActorID:              "actor-secret",
		IdempotencyKey:       stringPointer("idempotency-secret"),
		CanonicalRequestHash: "canonical-request-secret",
		AdminAccountID:       "admin-secret",
		UpstreamUsername:     "upstream-secret",
		Password:             NewEncryptedField("password-secret"),
		Host:                 "host-secret",
		GrantsJSON:           "grant-secret",
		LeaseOwner:           "owner-secret",
		LeaseToken:           "fencing-secret",
	})
	if err != nil {
		t.Fatalf("marshal operation: %v", err)
	}
	for _, secret := range []string{
		"operation-id", "actor-secret", "idempotency-secret", "canonical-request-secret", "admin-secret",
		"upstream-secret", "password-secret", "host-secret", "grant-secret", "owner-secret", "fencing-secret",
	} {
		if strings.Contains(string(payload), secret) {
			t.Errorf("serialized operation leaks %q: %s", secret, payload)
		}
	}
}

func TestDatabaseProvisioningSQLiteConstraints(t *testing.T) {
	db := openDatabaseProvisioningSQLite(t)
	instance := DatabaseInstance{ID: "instance-1", Name: "db-1", Address: "127.0.0.1", Port: 3306}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create instance: %v", err)
	}

	operation := newProvisioningOperation("operation-1", "actor-a", "create", "retry-key")
	operation.InstanceID = instance.ID
	if err := db.Create(&operation).Error; err != nil {
		t.Fatalf("create operation: %v", err)
	}

	operationID := operation.ID
	managed := newDatabaseAccount("managed-1", instance.ID)
	managed.Managed = true
	managed.UpstreamHost = "localhost"
	managed.ProvisioningOperationID = &operationID
	if err := db.Create(&managed).Error; err != nil {
		t.Fatalf("create managed account: %v", err)
	}

	duplicateAccount := newDatabaseAccount("managed-2", instance.ID)
	duplicateAccount.Managed = true
	duplicateAccount.UpstreamHost = "localhost"
	duplicateAccount.ProvisioningOperationID = &operationID
	assertConstraintViolation(t, db.Create(&duplicateAccount).Error, "account operation unique")

	assertConstraintViolation(t, db.Delete(&operation).Error, "operation delete restrict")
	var retained DatabaseAccount
	if err := db.First(&retained, "id = ?", managed.ID).Error; err != nil {
		t.Fatalf("managed account disappeared after restricted delete: %v", err)
	}
}

func TestDatabaseProvisioningSQLiteIdempotencyUniqueness(t *testing.T) {
	db := openDatabaseProvisioningSQLite(t)
	instance := DatabaseInstance{ID: "instance-idempotency", Name: "db-idempotency", Address: "127.0.0.1", Port: 3306}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create idempotency instance: %v", err)
	}
	createOperation := func(id, actor, kind, key, username string) {
		t.Helper()
		op := newProvisioningOperation(id, actor, kind, key)
		op.InstanceID = instance.ID
		op.UpstreamUsername = username
		if err := db.Create(&op).Error; err != nil {
			t.Fatalf("create operation %s: %v", id, err)
		}
	}
	createOperation("operation-1", "actor-a", "create", "same-key", "user-1")

	duplicate := newProvisioningOperation("operation-2", "actor-a", "create", "same-key")
	duplicate.InstanceID = instance.ID
	duplicate.UpstreamUsername = "user-2"
	assertConstraintViolation(t, db.Create(&duplicate).Error, "same actor, kind, key")

	createOperation("operation-3", "actor-b", "create", "same-key", "user-3")
	createOperation("operation-4", "actor-a", "deprovision", "same-key", "user-4")

	emptyKey := newProvisioningOperation("operation-5", "actor-c", "create", "")
	emptyKey.InstanceID = instance.ID
	emptyKey.UpstreamUsername = "user-5"
	assertConstraintViolation(t, db.Create(&emptyKey).Error, "empty idempotency key")

	withoutKeyA := newProvisioningOperation("operation-6", "actor-d", "create", "")
	withoutKeyA.InstanceID = instance.ID
	withoutKeyA.IdempotencyKey = nil
	withoutKeyA.UpstreamUsername = "user-6"
	if err := db.Create(&withoutKeyA).Error; err != nil {
		t.Fatalf("create operation without idempotency key: %v", err)
	}
	withoutKeyB := newProvisioningOperation("operation-7", "actor-d", "create", "")
	withoutKeyB.InstanceID = instance.ID
	withoutKeyB.IdempotencyKey = nil
	withoutKeyB.UpstreamUsername = "user-7"
	if err := db.Create(&withoutKeyB).Error; err != nil {
		t.Fatalf("create second operation without idempotency key: %v", err)
	}
}

func TestDatabaseProvisioningSQLiteStageAndManagedConsistencyConstraints(t *testing.T) {
	db := openDatabaseProvisioningSQLite(t)
	instance := DatabaseInstance{ID: "instance-stage", Name: "db-stage", Address: "127.0.0.1", Port: 3306}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create stage instance: %v", err)
	}
	validStages := []string{
		"reserved",
		"create_started",
		"create_uncertain",
		"upstream_created",
		"grant_started",
		"activation_pending",
		"cleanup_required",
		"cleanup_in_progress",
		"not_created",
		"active_managed",
		"deprovision_requested",
		"drop_started",
		"drop_uncertain",
		"dropped",
	}
	for index, stage := range validStages {
		op := newProvisioningOperation("stage-"+string(rune('1'+index)), "actor-stage", "create", "stage-key-"+stage)
		op.InstanceID = instance.ID
		op.UpstreamUsername = "stage-user-" + stage
		op.Stage = stage
		if err := db.Create(&op).Error; err != nil {
			t.Errorf("stage %q rejected: %v", stage, err)
		}
	}
	invalidStage := newProvisioningOperation("stage-invalid", "actor-stage", "create", "stage-key-invalid")
	invalidStage.InstanceID = instance.ID
	invalidStage.Stage = "not-a-stage"
	assertConstraintViolation(t, db.Create(&invalidStage).Error, "invalid stage")

	consistencyInstance := DatabaseInstance{ID: "instance-2", Name: "db-2", Address: "127.0.0.1", Port: 3307}
	if err := db.Create(&consistencyInstance).Error; err != nil {
		t.Fatalf("create consistency instance: %v", err)
	}
	op := newProvisioningOperation("consistency-op", "actor-consistency", "create", "consistency-key")
	op.InstanceID = consistencyInstance.ID
	if err := db.Create(&op).Error; err != nil {
		t.Fatalf("create consistency operation: %v", err)
	}
	opID := op.ID

	unmanaged := newDatabaseAccount("unmanaged-valid", consistencyInstance.ID)
	if err := db.Create(&unmanaged).Error; err != nil {
		t.Fatalf("valid unmanaged account rejected: %v", err)
	}
	managed := newDatabaseAccount("managed-valid", consistencyInstance.ID)
	managed.Managed = true
	managed.UpstreamHost = "localhost"
	managed.ProvisioningOperationID = &opID
	if err := db.Create(&managed).Error; err != nil {
		t.Fatalf("valid managed account rejected: %v", err)
	}

	invalidCases := []DatabaseAccount{
		func() DatabaseAccount {
			a := newDatabaseAccount("invalid-host", consistencyInstance.ID)
			a.UpstreamHost = "localhost"
			return a
		}(),
		func() DatabaseAccount {
			a := newDatabaseAccount("invalid-managed", consistencyInstance.ID)
			a.Managed = true
			a.UpstreamHost = "localhost"
			return a
		}(),
		func() DatabaseAccount {
			a := newDatabaseAccount("invalid-host-empty", consistencyInstance.ID)
			a.Managed = true
			a.ProvisioningOperationID = &opID
			return a
		}(),
		func() DatabaseAccount {
			a := newDatabaseAccount("invalid-unmanaged-op", consistencyInstance.ID)
			a.ProvisioningOperationID = &opID
			return a
		}(),
	}
	for _, account := range invalidCases {
		assertConstraintViolation(t, db.Create(&account).Error, "managed account consistency")
	}
}

func openDatabaseProvisioningSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		t.Fatalf("enable sqlite foreign keys: %v", err)
	}
	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("read sqlite foreign keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("sqlite foreign keys = %d, want 1", foreignKeys)
	}
	if err := db.AutoMigrate(&DatabaseInstance{}, &DatabaseProvisioningOperation{}, &DatabaseAccount{}); err != nil {
		t.Fatalf("auto migrate provisioning models: %v", err)
	}
	return db
}

func newProvisioningOperation(id, actor, kind, key string) DatabaseProvisioningOperation {
	return DatabaseProvisioningOperation{
		ID:                   id,
		Kind:                 kind,
		ActorID:              actor,
		IdempotencyKey:       stringPointer(key),
		CanonicalRequestHash: "hash-" + id,
		AdminAccountID:       "admin-" + id,
		UpstreamUsername:     "user-" + id,
		Password:             NewEncryptedField("password-" + id),
		Host:                 "localhost",
		GrantsJSON:           "{}",
	}
}

func newDatabaseAccount(id, instanceID string) DatabaseAccount {
	return DatabaseAccount{
		ID:         id,
		InstanceID: instanceID,
		UniqueName: "unique-" + id,
		Username:   "username-" + id,
		ResourceID: id,
	}
}

func assertConstraintViolation(t *testing.T, err error, name string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected constraint violation", name)
	}
}

func assertModelJSONTag(t *testing.T, modelType reflect.Type, name, want string) {
	t.Helper()
	field, ok := modelType.FieldByName(name)
	if !ok {
		t.Fatalf("%s field is missing from %s", name, modelType.Name())
	}
	if got := field.Tag.Get("json"); got != want {
		t.Errorf("%s json tag = %q, want %q", name, got, want)
	}
}

func assertModelField(t *testing.T, modelType reflect.Type, name string, wantType reflect.Type, wantJSON string) {
	t.Helper()
	field, ok := modelType.FieldByName(name)
	if !ok {
		t.Fatalf("%s field is missing from %s", name, modelType.Name())
	}
	if field.Type != wantType {
		t.Errorf("%s type = %s, want %s", name, field.Type, wantType)
	}
	if got := field.Tag.Get("json"); got != strings.TrimPrefix(wantJSON, "json:") {
		t.Errorf("%s json tag = %q, want %q", name, got, strings.TrimPrefix(wantJSON, "json:"))
	}
}

func stringPointer(value string) *string { return &value }
