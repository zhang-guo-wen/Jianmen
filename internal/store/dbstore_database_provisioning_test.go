package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/crypto"
	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/storage"
)

var _ service.DatabaseProvisioningRepository = (*DBStore)(nil)

func TestDatabaseProvisioningOperationPersistsEncryptedIntentWithoutResource(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	migrateProvisioningOperationsForStoreTest(t, db)
	instance, admin := seedProvisioningStoreAdministrator(t, db)
	operation, _, err := repository.CreateDatabaseProvisioningOperation(
		context.Background(),
		service.DatabaseProvisioningOperationInput{
			ID: "jmo_operation00000000001", InstanceID: instance.ID,
			AdminAccountID: admin.ID, Username: "jm_operation00000000001",
			Password: "generated-top-secret", Host: "10.0.0.8",
			GrantsJSON: `[{"database":"orders","privilege":"read"}]`,
			Lease: service.DatabaseProvisioningLease{
				Owner: "request", Token: "request-lease", Duration: time.Minute,
			},
		},
	)
	if err != nil {
		t.Fatalf("create provisioning operation: %v", err)
	}
	if operation.Stage != service.ProvisioningStageReserved ||
		operation.CleanupStatus != service.ProvisioningCleanupNone ||
		operation.Revision != 1 ||
		operation.Password != "generated-top-secret" {
		t.Fatalf("unexpected provisioning operation: %#v", operation)
	}
	var storedPassword string
	if err := db.Raw(
		"SELECT password FROM database_provisioning_operations WHERE id = ?",
		operation.ID,
	).Scan(&storedPassword).Error; err != nil {
		t.Fatalf("read encrypted operation password: %v", err)
	}
	if storedPassword == "generated-top-secret" ||
		strings.Contains(storedPassword, "generated-top-secret") {
		t.Fatalf("operation password was stored in plaintext: %q", storedPassword)
	}
	var resources int64
	if err := db.Model(&model.Resource{}).
		Where("type = ? AND resource_id = ?",
			model.ResourceTypeDatabaseAccount, operation.ID).
		Count(&resources).Error; err != nil {
		t.Fatalf("count pending resources: %v", err)
	}
	if resources != 0 {
		t.Fatalf("pending operation created ordinary resource rows: %d", resources)
	}
}

func TestDatabaseProvisioningCreateOrGetUsesActorScopedIdempotencyIdentity(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	migrateProvisioningOperationsForStoreTest(t, db)
	instance, admin := seedProvisioningStoreAdministrator(t, db)
	input := service.DatabaseProvisioningOperationInput{
		ID: "jmo_idempotency000000001", InstanceID: instance.ID, AdminAccountID: admin.ID,
		Username: "jm_idempotency000000001", Password: "generated-top-secret", Host: "10.0.0.8",
		GrantsJSON: `[ {"database":"orders","privilege":"read"} ]`, ActorID: "operator-1",
		IdempotencyKey: "sqlite-idempotency-key-001", RequestHash: strings.Repeat("a", 64),
		Lease: service.DatabaseProvisioningLease{Owner: "request", Token: "request-lease", Duration: time.Minute},
	}
	first, _, created, err := repository.CreateOrGetDatabaseProvisioningOperation(context.Background(), input)
	if err != nil || !created {
		t.Fatalf("first create-or-get = %#v, %t, %v", first, created, err)
	}
	input.ID = "jmo_idempotency000000002"
	input.Username = "jm_idempotency000000002"
	second, _, created, err := repository.CreateOrGetDatabaseProvisioningOperation(context.Background(), input)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("same unique identity was not atomically reused: %#v, %t, %v", second, created, err)
	}
	input.RequestHash = strings.Repeat("b", 64)
	if _, _, _, err := repository.CreateOrGetDatabaseProvisioningOperation(context.Background(), input); !errors.Is(err, service.ErrDatabaseProvisioningIdempotencyConflict) {
		t.Fatalf("same idempotency identity with different request = %v, want conflict", err)
	}
}

func TestDatabaseProvisioningCreateOrGetSQLiteConcurrentUniqueIdentity(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	migrateProvisioningOperationsForStoreTest(t, db)
	instance, admin := seedProvisioningStoreAdministrator(t, db)
	input := service.DatabaseProvisioningOperationInput{
		ID: "jmo_concurrent0000000001", InstanceID: instance.ID, AdminAccountID: admin.ID,
		Username: "jm_concurrent0000000001", Password: "generated-top-secret", Host: "10.0.0.8",
		GrantsJSON: `[{"database":"orders","privilege":"read"}]`, ActorID: "operator-1",
		IdempotencyKey: "sqlite-concurrent-key-001", RequestHash: strings.Repeat("a", 64),
		Lease: service.DatabaseProvisioningLease{Owner: "request", Token: "request-lease", Duration: time.Minute},
	}
	const callers = 8
	start := make(chan struct{})
	results := make(chan service.DatabaseProvisioningOperation, callers)
	errorsSeen := make(chan error, callers)
	var wait sync.WaitGroup
	for index := 0; index < callers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			operation, _, _, err := repository.CreateOrGetDatabaseProvisioningOperation(context.Background(), input)
			if err != nil {
				errorsSeen <- err
				return
			}
			results <- operation
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatalf("concurrent create-or-get: %v", err)
	}
	for operation := range results {
		if operation.ID == "" {
			t.Fatal("concurrent create-or-get returned an empty operation")
		}
	}
	var count int64
	if err := db.Model(&model.DatabaseProvisioningOperation{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("SQLite idempotency unique identity count = %d, %v", count, err)
	}
}

func TestDatabaseProvisioningAdminLoadsOnlySameInstanceCredential(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	instance, admin := seedProvisioningStoreAdministrator(t, db)
	gotInstance, gotAdmin, err := repository.DatabaseProvisioningAdmin(
		context.Background(),
		instance.ID,
		admin.ID,
	)
	if err != nil {
		t.Fatalf("load provisioning administrator: %v", err)
	}
	if gotInstance.ID != instance.ID || gotAdmin.Password.GetPlaintext() != "admin-secret" {
		t.Fatalf("unexpected provisioning administrator: %#v %#v", gotInstance, gotAdmin)
	}
	other := model.DatabaseInstance{
		Name: "other", Protocol: "mysql", Address: "127.0.0.2", Status: "active",
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatalf("create other instance: %v", err)
	}
	if _, _, err := repository.DatabaseProvisioningAdmin(
		context.Background(), other.ID, admin.ID,
	); err == nil {
		t.Fatal("loaded administrator credential from another instance")
	}
}

func TestDatabaseProvisioningAuditStoresOnlySafeMetadata(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	auditID, err := repository.BeginDatabaseProvisioningAudit(
		context.Background(),
		service.DatabaseProvisioningAudit{
			Actor: service.DatabaseProvisioningActor{
				UserID: "operator-1", Username: "operator", ClientIP: "127.0.0.1",
			},
			InstanceID: "instance-1", AccountID: "admin-1",
			Operation: "provision_account", Result: "started",
		},
	)
	if err != nil {
		t.Fatalf("begin provisioning audit: %v", err)
	}
	if err := repository.CompleteDatabaseProvisioningAudit(
		context.Background(), auditID, "failure",
	); err != nil {
		t.Fatalf("complete provisioning audit: %v", err)
	}
	var event model.AuditEvent
	if err := db.First(&event, "id = ?", auditID).Error; err != nil {
		t.Fatalf("load provisioning audit: %v", err)
	}
	if event.ActorID != "operator-1" || event.ResourceID != "admin-1" ||
		event.Action != "use_provisioning_credential" ||
		!strings.Contains(event.Detail, `"result":"failure"`) {
		t.Fatalf("unexpected provisioning audit: %#v", event)
	}
	if strings.Contains(event.Detail, "password") || event.ResourceName != "" {
		t.Fatalf("provisioning audit exposed sensitive details: %#v", event)
	}
}

func seedProvisioningStoreAdministrator(
	t *testing.T,
	db *gorm.DB,
) (model.DatabaseInstance, model.DatabaseAccount) {
	t.Helper()
	instance := model.DatabaseInstance{
		Name: "orders", Protocol: "mysql", Address: "127.0.0.1",
		Port: 3306, Status: "active",
	}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create instance: %v", err)
	}
	admin := model.DatabaseAccount{
		InstanceID: instance.ID, UniqueName: "admin", Username: "root",
		Password: model.NewEncryptedField("admin-secret"), Status: "active",
		ResourceID: "D001",
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create administrator: %v", err)
	}
	return instance, admin
}

func newDatabaseProvisioningStoreTest(t *testing.T) (*DBStore, *gorm.DB) {
	t.Helper()
	dataDir := t.TempDir()
	if _, err := crypto.Init(dataDir); err != nil {
		t.Fatalf("initialize crypto: %v", err)
	}
	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    filepath.Join(dataDir, "provisioning.db"),
	})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("open SQL database handle: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return NewDBStore(db), db
}
