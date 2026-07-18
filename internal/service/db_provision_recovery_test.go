package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRecoveryAdministratorAllowsExpiredCredentialOnlyForCleanup(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	expired := time.Now().UTC().Add(-time.Minute)
	repository.admin.ExpiresAt = &expired
	provisioner := &databaseProvisionerFake{}
	service := newProvisioningServiceForTest(t, repository, provisioner, nil)
	op := fakeReconcileOperation("abcdefghijklmnopqrst", ProvisioningStageCleanupRequired, ProvisioningCleanupRequired, time.Now().UTC())
	repository.putOperation(op)

	if result, err := service.Reconcile(context.Background(), 1); err != nil || result.Cleaned != 1 || provisioner.dropCalls != 1 {
		t.Fatalf("expired recovery administrator was not usable: %#v, %v; drops=%d", result, err, provisioner.dropCalls)
	}
}

func TestRecoveryAdministratorRejectsExpiredCredentialForNewUse(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	expired := time.Now().UTC().Add(-time.Minute)
	repository.admin.ExpiresAt = &expired
	provisioner := &databaseProvisionerFake{}
	service := newProvisioningServiceForTest(t, repository, provisioner, nil)

	if _, err := service.ListDatabases(context.Background(), ListProvisioningDatabasesRequest{InstanceID: "instance-1", AdminAccountID: "admin-1"}); !errors.Is(err, ErrDatabaseProvisioningFailed) {
		t.Fatalf("expired administrator list error = %v", err)
	}
	if provisioner.listCalls != 0 {
		t.Fatal("public database listing used expired administrator")
	}
	_, err := service.Provision(context.Background(), ProvisionDatabaseAccountRequest{
		InstanceID: "instance-1", AdminAccountID: "admin-1", Host: "10.0.0.8",
		Grants: []DBGrant{{Database: "orders", Privilege: "read"}}, Actor: DatabaseProvisioningActor{UserID: "operator"},
	})
	if !errors.Is(err, ErrDatabaseProvisioningFailed) || provisioner.createCalls != 0 {
		t.Fatalf("expired administrator provision error = %v; creates=%d", err, provisioner.createCalls)
	}
}
