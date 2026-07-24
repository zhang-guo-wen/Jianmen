package store

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func TestManagedDatabaseDeprovisionSQLiteSoftDeletesAccountAndOperationAtomically(t *testing.T) {
	repository, db, account, operation, started := prepareManagedDatabaseDeprovision(t)

	if ok, err := repository.CompleteDatabaseDeprovision(context.Background(), started.Fence(), account.ID); err != nil || !ok {
		t.Fatalf("complete = %t, %v", ok, err)
	}
	assertDatabaseDeprovisionState(t, db, account.ID, operation.ID, false)
	assertDatabaseAccountResourceCount(t, db, account.ID, 0)
}

func TestManagedDatabaseDeprovisionSQLiteRollsBackAllTombstones(t *testing.T) {
	repository, db, account, operation, started := prepareManagedDatabaseDeprovision(t)
	if err := db.Exec(`
		CREATE TRIGGER reject_deprovision_operation_tombstone
		BEFORE UPDATE OF deleted_at ON database_provisioning_operations
		WHEN NEW.deleted_at IS NULL
		BEGIN
			SELECT RAISE(ABORT, 'reject operation tombstone');
		END;
	`).Error; err != nil {
		t.Fatalf("create operation tombstone trigger: %v", err)
	}

	if ok, err := repository.CompleteDatabaseDeprovision(context.Background(), started.Fence(), account.ID); err == nil || ok {
		t.Fatalf("complete with rejected operation tombstone = %t, %v; want false, error", ok, err)
	}
	assertDatabaseDeprovisionState(t, db, account.ID, operation.ID, true)
	assertDatabaseAccountResourceCount(t, db, account.ID, 1)
}

func prepareManagedDatabaseDeprovision(t *testing.T) (*DBStore, *gorm.DB, model.DatabaseAccount, model.DatabaseProvisioningOperation, service.DatabaseProvisioningOperation) {
	t.Helper()
	repository, db := newDatabaseProvisioningStoreTest(t)
	migrateProvisioningOperationsForStoreTest(t, db)
	instance, admin := seedProvisioningStoreAdministrator(t, db)
	const token = "abcdefghijklmnopqrst"
	op := model.DatabaseProvisioningOperation{ID: "jmo_" + token, InstanceID: instance.ID, AdminAccountID: admin.ID, UpstreamUsername: "jm_" + token, Password: model.NewEncryptedField("secret"), Host: "10.0.0.8", GrantsJSON: "[]", Stage: service.ProvisioningStageActiveManaged, CleanupStatus: service.ProvisioningCleanupNone, Revision: 1}
	if err := db.Create(&op).Error; err != nil {
		t.Fatalf("create operation: %v", err)
	}
	account := model.DatabaseAccount{InstanceID: instance.ID, UniqueName: "managed-deprovision", Username: op.UpstreamUsername, Password: op.Password, Status: "active", Managed: true, UpstreamHost: op.Host, ProvisioningOperationID: &op.ID, ResourceID: "D100"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create managed account: %v", err)
	}
	if err := repository.syncResource(model.ResourceTypeDatabaseAccount, account.ID, account.UniqueName, instance.ID); err != nil {
		t.Fatalf("create managed account resource: %v", err)
	}
	claimed, managed, err := repository.BeginDatabaseDeprovision(context.Background(), account.ID, service.DatabaseProvisioningLease{Owner: "worker", Token: "lease", Duration: time.Minute})
	if err != nil || !managed {
		t.Fatalf("begin = %#v, %t, %v", claimed, managed, err)
	}
	started, ok, err := repository.TransitionDatabaseProvisioningOperation(context.Background(), claimed.Fence(), service.DatabaseProvisioningTransition{Stage: service.ProvisioningStageDropStarted, CleanupStatus: service.ProvisioningCleanupInProgress})
	if err != nil || !ok {
		t.Fatalf("start DROP = %#v, %t, %v", started, ok, err)
	}
	return repository, db, account, op, started
}

func assertDatabaseDeprovisionState(t *testing.T, db *gorm.DB, accountID, operationID string, wantActive bool) {
	t.Helper()
	var account model.DatabaseAccount
	if err := db.First(&account, "id = ?", accountID).Error; err != nil {
		t.Fatalf("load managed account tombstone: %v", err)
	}
	var operation model.DatabaseProvisioningOperation
	if err := db.First(&operation, "id = ?", operationID).Error; err != nil {
		t.Fatalf("load provisioning operation tombstone: %v", err)
	}
	if !matchesDatabaseDeprovisionMarker(account.DeletedAt, wantActive) {
		t.Fatalf("managed account deleted_at = %v, want active=%v", account.DeletedAt, wantActive)
	}
	if !matchesDatabaseDeprovisionMarker(operation.DeletedAt, wantActive) {
		t.Fatalf("operation deleted_at = %v, want active=%v", operation.DeletedAt, wantActive)
	}

	wantActiveCount := int64(0)
	if wantActive {
		wantActiveCount = 1
	}
	var accountCount int64
	if err := db.Model(&model.DatabaseAccount{}).Scopes(ActiveScope).Where("id = ?", accountID).Count(&accountCount).Error; err != nil {
		t.Fatalf("count active managed accounts: %v", err)
	}
	if accountCount != wantActiveCount {
		t.Fatalf("active managed account count = %d, want %d", accountCount, wantActiveCount)
	}
	var operationCount int64
	if err := db.Model(&model.DatabaseProvisioningOperation{}).Scopes(ActiveScope).Where("id = ?", operationID).Count(&operationCount).Error; err != nil {
		t.Fatalf("count active provisioning operations: %v", err)
	}
	if operationCount != wantActiveCount {
		t.Fatalf("active operation count = %d, want %d", operationCount, wantActiveCount)
	}
}

func matchesDatabaseDeprovisionMarker(deletedAt *int, wantActive bool) bool {
	if !wantActive {
		return deletedAt == nil
	}
	return deletedAt != nil && *deletedAt == model.DeletedMarkerActive
}

func assertDatabaseAccountResourceCount(t *testing.T, db *gorm.DB, accountID string, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&model.Resource{}).
		Where("type = ? AND resource_id = ?", model.ResourceTypeDatabaseAccount, accountID).
		Count(&count).Error; err != nil {
		t.Fatalf("count managed account resources: %v", err)
	}
	if count != want {
		t.Fatalf("managed account resource count = %d, want %d", count, want)
	}
}

func TestManagedDatabaseAccountUpdateRejectsIdentityButAllowsMetadata(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	migrateProvisioningOperationsForStoreTest(t, db)
	instance, admin := seedProvisioningStoreAdministrator(t, db)
	opID := "jmo_abcdefghijklmnopqrst"
	op := model.DatabaseProvisioningOperation{ID: opID, InstanceID: instance.ID, AdminAccountID: admin.ID, UpstreamUsername: "jm_abcdefghijklmnopqrst", Password: model.NewEncryptedField("secret"), Host: "10.0.0.8", GrantsJSON: "[]", Stage: service.ProvisioningStageActiveManaged, CleanupStatus: service.ProvisioningCleanupNone, Revision: 1}
	if err := db.Create(&op).Error; err != nil {
		t.Fatalf("create operation: %v", err)
	}
	account := model.DatabaseAccount{InstanceID: instance.ID, UniqueName: "managed-update", Username: "jm_abcdefghijklmnopqrst", Password: model.NewEncryptedField("secret"), Status: "active", Managed: true, UpstreamHost: "10.0.0.8", ProvisioningOperationID: &opID, ResourceID: "D101"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create managed account: %v", err)
	}
	if _, err := repository.UpdateDatabaseAccount(context.Background(), account.ID, "other", "", "", "", nil, "active"); err == nil {
		t.Fatal("managed username update was allowed")
	}
	if _, err := repository.UpdateDatabaseAccount(context.Background(), account.ID, account.Username, "other-secret", "", "", nil, "active"); err == nil {
		t.Fatal("managed password update was allowed")
	}
	if _, err := repository.UpdateDatabaseAccount(context.Background(), account.ID, account.Username, "", "ops", "retained", nil, "disabled"); err != nil {
		t.Fatalf("managed metadata update: %v", err)
	}
}

func TestBeginDatabaseDeprovisionRejectsManagedIdentityMismatch(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.DatabaseProvisioningOperation, *model.DatabaseAccount)
	}{
		{
			name: "operation host changed to another valid host",
			mutate: func(operation *model.DatabaseProvisioningOperation, _ *model.DatabaseAccount) {
				operation.Host = "10.0.0.9"
			},
		},
		{
			name: "operation username differs from retained account",
			mutate: func(_ *model.DatabaseProvisioningOperation, account *model.DatabaseAccount) {
				account.Username = "jm_zzzzzzzzzzzzzzzzzzzz"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, db := newDatabaseProvisioningStoreTest(t)
			migrateProvisioningOperationsForStoreTest(t, db)
			instance, admin := seedProvisioningStoreAdministrator(t, db)
			const token = "abcdefghijklmnopqrst"
			op := model.DatabaseProvisioningOperation{ID: "jmo_" + token, InstanceID: instance.ID, AdminAccountID: admin.ID, UpstreamUsername: "jm_" + token, Password: model.NewEncryptedField("secret"), Host: "10.0.0.8", GrantsJSON: "[]", Stage: service.ProvisioningStageActiveManaged, CleanupStatus: service.ProvisioningCleanupNone, Revision: 1}
			if err := db.Create(&op).Error; err != nil {
				t.Fatalf("create operation: %v", err)
			}
			account := model.DatabaseAccount{InstanceID: instance.ID, UniqueName: "managed-mismatch-" + token, Username: op.UpstreamUsername, Password: op.Password, Status: "active", Managed: true, UpstreamHost: op.Host, ProvisioningOperationID: &op.ID, ResourceID: "D102"}
			test.mutate(&op, &account)
			if err := db.Create(&account).Error; err != nil {
				t.Fatalf("create managed account: %v", err)
			}
			if err := db.Model(&model.DatabaseProvisioningOperation{}).Where("id = ?", op.ID).Updates(map[string]any{"upstream_username": op.UpstreamUsername, "host": op.Host}).Error; err != nil {
				t.Fatalf("tamper operation: %v", err)
			}
			if _, managed, err := repository.BeginDatabaseDeprovision(context.Background(), account.ID, service.DatabaseProvisioningLease{Owner: "worker", Token: "lease", Duration: time.Minute}); err == nil || !managed {
				t.Fatalf("begin mismatch = managed %t, err %v", managed, err)
			}
		})
	}
}
