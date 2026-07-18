package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestDeprovisionManagedDatabaseAccountDropsExactOperationIdentity(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	provisioner := &databaseProvisionerFake{}
	svc := newProvisioningServiceForTest(t, repository, provisioner, nil)
	op := fakeReconcileOperation("abcdefghijklmnopqrst", ProvisioningStageActiveManaged, ProvisioningCleanupNone, time.Now().UTC())
	op.LeaseExpiresAt = nil
	repository.putOperation(op)
	repository.recordActivated(op.ID, fakeProvisionedAccount(op))
	if err := svc.Deprovision(context.Background(), "account-"+op.ID); err != nil {
		t.Fatalf("deprovision: %v", err)
	}
	if provisioner.dropCalls != 1 {
		t.Fatalf("DROP calls = %d, want 1", provisioner.dropCalls)
	}
	if _, err := repository.DatabaseProvisioningOperation(context.Background(), op.ID); err == nil {
		t.Fatal("operation was retained after successful DROP")
	}
}

func TestDeprovisionFailureRetainsManagedDatabaseAccountForReconcile(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	provisioner := &databaseProvisionerFake{drop: func(context.Context, string) error { return errors.New("timeout") }}
	svc := newProvisioningServiceForTest(t, repository, provisioner, nil)
	op := fakeReconcileOperation("abcdefghijklmnopqrst", ProvisioningStageActiveManaged, ProvisioningCleanupNone, time.Now().UTC())
	op.LeaseExpiresAt = nil
	repository.putOperation(op)
	repository.recordActivated(op.ID, fakeProvisionedAccount(op))
	if err := svc.Deprovision(context.Background(), "account-"+op.ID); !errors.Is(err, ErrDatabaseDeprovisionFailed) {
		t.Fatalf("deprovision error = %v", err)
	}
	stored, err := repository.DatabaseProvisioningOperation(context.Background(), op.ID)
	if err != nil || stored.Stage != ProvisioningStageDropUncertain {
		t.Fatalf("uncertain operation = %#v, %v", stored, err)
	}
	if _, found, _ := repository.ProvisionedDatabaseAccountByOperation(context.Background(), op.ID); !found {
		t.Fatal("managed account was deleted before confirmed DROP")
	}
}

func TestDeprovisionConcurrentManagedDatabaseAccountDropsOnce(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	provisioner := &databaseProvisionerFake{drop: func(context.Context, string) error { time.Sleep(20 * time.Millisecond); return nil }}
	svc := newProvisioningServiceForTest(t, repository, provisioner, nil)
	op := fakeReconcileOperation("abcdefghijklmnopqrst", ProvisioningStageActiveManaged, ProvisioningCleanupNone, time.Now().UTC())
	op.LeaseExpiresAt = nil
	repository.putOperation(op)
	repository.recordActivated(op.ID, fakeProvisionedAccount(op))
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = svc.Deprovision(context.Background(), "account-"+op.ID) }()
	}
	wg.Wait()
	if provisioner.dropCalls != 1 {
		t.Fatalf("concurrent DROP calls = %d, want 1", provisioner.dropCalls)
	}
}

func TestReconcileDeprovisionUncertainConverges(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	provisioner := &databaseProvisionerFake{}
	svc := newProvisioningServiceForTest(t, repository, provisioner, nil)
	op := fakeReconcileOperation("abcdefghijklmnopqrst", ProvisioningStageDropUncertain, ProvisioningCleanupRequired, time.Now().UTC())
	repository.putOperation(op)
	repository.activated[op.ID] = fakeProvisionedAccount(op)
	result, err := svc.Reconcile(context.Background(), 10)
	if err != nil || result.Cleaned != 1 || provisioner.dropCalls != 1 {
		t.Fatalf("reconcile = %#v, %v; drops=%d", result, err, provisioner.dropCalls)
	}
}

func TestDeprovisionRejectsTamperedOperationIdentityWithoutDrop(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	provisioner := &databaseProvisionerFake{}
	svc := newProvisioningServiceForTest(t, repository, provisioner, nil)
	op := fakeReconcileOperation("abcdefghijklmnopqrst", ProvisioningStageActiveManaged, ProvisioningCleanupNone, time.Now().UTC())
	op.Host = "unsafe host name"
	op.LeaseExpiresAt = nil
	repository.putOperation(op)
	repository.recordActivated(op.ID, fakeProvisionedAccount(op))
	if err := svc.Deprovision(context.Background(), "account-"+op.ID); !errors.Is(err, ErrDatabaseDeprovisionFailed) {
		t.Fatalf("deprovision error = %v", err)
	}
	if provisioner.dropCalls != 0 {
		t.Fatalf("tampered operation issued DROP %d times", provisioner.dropCalls)
	}
}

func TestDeprovisionRejectsManagedAccountIdentityMismatchWithoutDrop(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*DatabaseProvisioningOperation, *ProvisionedDatabaseAccount)
	}{
		{
			name: "operation host changed to another valid host",
			mutate: func(operation *DatabaseProvisioningOperation, _ *ProvisionedDatabaseAccount) {
				operation.Host = "10.0.0.9"
			},
		},
		{
			name: "operation username differs from retained account",
			mutate: func(_ *DatabaseProvisioningOperation, account *ProvisionedDatabaseAccount) {
				account.Username = "jm_zzzzzzzzzzzzzzzzzzzz"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newProvisioningRepositoryFake()
			provisioner := &databaseProvisionerFake{}
			svc := newProvisioningServiceForTest(t, repository, provisioner, nil)
			op := fakeReconcileOperation("abcdefghijklmnopqrst", ProvisioningStageActiveManaged, ProvisioningCleanupNone, time.Now().UTC())
			op.LeaseExpiresAt = nil
			account := fakeProvisionedAccount(op)
			test.mutate(&op, &account)
			repository.putOperation(op)
			repository.recordActivated(op.ID, account)

			if err := svc.Deprovision(context.Background(), account.ID); !errors.Is(err, ErrDatabaseDeprovisionFailed) {
				t.Fatalf("deprovision error = %v", err)
			}
			if provisioner.dropCalls != 0 {
				t.Fatalf("identity mismatch issued DROP %d times", provisioner.dropCalls)
			}
		})
	}
}

func TestReconcileDropStartedExpiredLeaseConvergesAfterCrash(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	provisioner := &databaseProvisionerFake{}
	svc := newProvisioningServiceForTest(t, repository, provisioner, nil)
	op := fakeReconcileOperation("abcdefghijklmnopqrst", ProvisioningStageDropStarted, ProvisioningCleanupInProgress, time.Now().UTC())
	repository.putOperation(op)
	repository.recordActivated(op.ID, fakeProvisionedAccount(op))
	repository.mu.Lock()
	stored := repository.operations[op.ID]
	stored.Stage, stored.CleanupStatus = ProvisioningStageDropStarted, ProvisioningCleanupInProgress
	repository.operations[op.ID] = stored
	repository.mu.Unlock()

	result, err := svc.Reconcile(context.Background(), 10)
	if err != nil || result.Cleaned != 1 || provisioner.dropCalls != 1 {
		t.Fatalf("reconcile = %#v, %v; drops=%d", result, err, provisioner.dropCalls)
	}
	if _, err := repository.DatabaseProvisioningOperation(context.Background(), op.ID); err == nil {
		t.Fatal("drop_started operation was not converged")
	}
}

func TestReconcileRenewFailureLeavesDeprovisionRecoverable(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	repository.renewDenied = true
	provisioner := &databaseProvisionerFake{}
	svc := newProvisioningServiceForTest(t, repository, provisioner, nil)
	op := fakeReconcileOperation("abcdefghijklmnopqrst", ProvisioningStageDropStarted, ProvisioningCleanupInProgress, time.Now().UTC())
	repository.putOperation(op)
	repository.recordActivated(op.ID, fakeProvisionedAccount(op))
	repository.mu.Lock()
	stored := repository.operations[op.ID]
	stored.Stage, stored.CleanupStatus = ProvisioningStageDropStarted, ProvisioningCleanupInProgress
	repository.operations[op.ID] = stored
	repository.mu.Unlock()

	if result, err := svc.Reconcile(context.Background(), 10); err != nil || result.Failed != 1 {
		t.Fatalf("failed reconcile = %#v, %v", result, err)
	}
	stored, err := repository.DatabaseProvisioningOperation(context.Background(), op.ID)
	if err != nil || stored.Stage != ProvisioningStageDropUncertain {
		t.Fatalf("renew failure did not preserve recoverable operation: %#v, %v", stored, err)
	}
	repository.renewDenied = false
	if result, err := svc.Reconcile(context.Background(), 10); err != nil || result.Cleaned != 1 || provisioner.dropCalls != 1 {
		t.Fatalf("recovery reconcile = %#v, %v; drops=%d", result, err, provisioner.dropCalls)
	}
}
