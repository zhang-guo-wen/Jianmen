package store

import (
	"context"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func TestManagedDatabaseDeprovisionSQLiteDeletesAccountAndOperationAtomically(t *testing.T) {
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
	claimed, managed, err := repository.BeginDatabaseDeprovision(context.Background(), account.ID, service.DatabaseProvisioningLease{Owner: "worker", Token: "lease", Duration: time.Minute})
	if err != nil || !managed {
		t.Fatalf("begin = %#v, %t, %v", claimed, managed, err)
	}
	started, ok, err := repository.TransitionDatabaseProvisioningOperation(context.Background(), claimed.Fence(), service.DatabaseProvisioningTransition{Stage: service.ProvisioningStageDropStarted, CleanupStatus: service.ProvisioningCleanupInProgress})
	if err != nil || !ok {
		t.Fatalf("start DROP = %#v, %t, %v", started, ok, err)
	}
	if ok, err := repository.CompleteDatabaseDeprovision(context.Background(), started.Fence(), account.ID); err != nil || !ok {
		t.Fatalf("complete = %t, %v", ok, err)
	}
	var count int64
	if err := db.Model(&model.DatabaseAccount{}).Where("id = ?", account.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("managed account count = %d, %v", count, err)
	}
	if err := db.Model(&model.DatabaseProvisioningOperation{}).Where("id = ?", op.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("operation count = %d, %v", count, err)
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
	if _, err := repository.UpdateDatabaseAccount(account.ID, "other", "", "", "", nil, "active"); err == nil {
		t.Fatal("managed username update was allowed")
	}
	if _, err := repository.UpdateDatabaseAccount(account.ID, account.Username, "other-secret", "", "", nil, "active"); err == nil {
		t.Fatal("managed password update was allowed")
	}
	if _, err := repository.UpdateDatabaseAccount(account.ID, account.Username, "", "ops", "retained", nil, "disabled"); err != nil {
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
