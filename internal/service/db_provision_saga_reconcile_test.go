package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestDatabaseProvisioningReconcilerClaimFailureNeverDrops(t *testing.T) {
	now := time.Now().UTC()
	repository := newProvisioningRepositoryFake()
	operation := fakeReconcileOperation(
		"reconcile00000000001",
		ProvisioningStageCleanupRequired,
		ProvisioningCleanupRequired,
		now,
	)
	repository.putOperation(operation)
	repository.claimDenied[operation.ID] = true
	provisioner := &databaseProvisionerFake{}
	provisioning := newProvisioningServiceForTest(
		t,
		repository,
		provisioner,
		func(options *DatabaseProvisioningOptions) { options.Now = func() time.Time { return now } },
	)
	result, err := provisioning.Reconcile(context.Background(), 20)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.ClaimSkipped != 1 || provisioner.dropCalls != 0 {
		t.Fatalf("claim failure performed cleanup: result=%#v drops=%d", result, provisioner.dropCalls)
	}
}

func TestDatabaseProvisioningReconcilerCountsInternalClaimFailure(t *testing.T) {
	now := time.Now().UTC()
	repository := newProvisioningRepositoryFake()
	operation := fakeReconcileOperation(
		"randomfail0000000001",
		ProvisioningStageCleanupRequired,
		ProvisioningCleanupRequired,
		now,
	)
	repository.putOperation(operation)
	provisioner := &databaseProvisionerFake{}
	provisioning := newProvisioningServiceForTest(
		t,
		repository,
		provisioner,
		func(options *DatabaseProvisioningOptions) {
			options.Now = func() time.Time { return now }
			options.Random = failingProvisioningRandomReader{}
		},
	)
	result, err := provisioning.Reconcile(context.Background(), 20)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.Failed != 1 || result.ClaimSkipped != 0 || provisioner.dropCalls != 0 {
		t.Fatalf("internal claim failure was not observable: %#v drops=%d",
			result, provisioner.dropCalls)
	}
}

func TestDatabaseProvisioningReconcilerDeletesStaleReservedWithoutDrop(t *testing.T) {
	now := time.Now().UTC()
	repository := newProvisioningRepositoryFake()
	operation := fakeReconcileOperation(
		"reserved000000000001",
		ProvisioningStageReserved,
		ProvisioningCleanupNone,
		now,
	)
	repository.putOperation(operation)
	provisioner := &databaseProvisionerFake{}
	provisioning := newProvisioningServiceForTest(
		t,
		repository,
		provisioner,
		func(options *DatabaseProvisioningOptions) { options.Now = func() time.Time { return now } },
	)
	result, err := provisioning.Reconcile(context.Background(), 20)
	if err != nil {
		t.Fatalf("reconcile stale reserved: %v", err)
	}
	operations := repository.operationsSnapshot()
	if result.DeletedReserved != 1 || provisioner.dropCalls != 0 || len(operations) != 1 ||
		operations[0].Stage != ProvisioningStageNotCreated || operations[0].LeaseOwner != "" {
		t.Fatalf("stale reservation was not retained as a terminal tombstone: %#v drops=%d",
			result, provisioner.dropCalls)
	}
}

func TestDatabaseProvisioningReconcilerContinuesAfterItemFailure(t *testing.T) {
	now := time.Now().UTC()
	repository := newProvisioningRepositoryFake()
	first := fakeReconcileOperation(
		"failure0000000000001",
		ProvisioningStageCleanupRequired,
		ProvisioningCleanupRequired,
		now,
	)
	second := fakeReconcileOperation(
		"success0000000000001",
		ProvisioningStageCleanupRequired,
		ProvisioningCleanupRequired,
		now,
	)
	repository.putOperation(first)
	repository.putOperation(second)
	repository.listOrder = []string{first.ID, second.ID}
	provisioner := &databaseProvisionerFake{
		drop: func(_ context.Context, username string) error {
			if username == first.Username {
				return errors.New("upstream password=top-secret")
			}
			return nil
		},
	}
	provisioning := newProvisioningServiceForTest(
		t,
		repository,
		provisioner,
		func(options *DatabaseProvisioningOptions) { options.Now = func() time.Time { return now } },
	)
	result, err := provisioning.Reconcile(context.Background(), 20)
	if err != nil {
		t.Fatalf("reconcile batch: %v", err)
	}
	if result.Failed != 1 || result.Cleaned != 1 || provisioner.dropCalls != 2 {
		t.Fatalf("single item stopped batch: result=%#v drops=%d", result, provisioner.dropCalls)
	}
}

func TestDatabaseProvisioningReconcilerLogsSanitizedRoundFailureAndStops(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	repository.listErr = errors.New("storage password=top-secret")
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	provisioning := newProvisioningServiceForTest(
		t,
		repository,
		&databaseProvisionerFake{},
		func(options *DatabaseProvisioningOptions) { options.Logger = logger },
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- provisioning.RunReconciler(ctx, time.Hour, 20)
	}()
	deadline := time.Now().Add(time.Second)
	for repository.listCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("reconciler shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("reconciler did not stop with application context")
	}
	logged := logs.String()
	if !strings.Contains(logged, "reconciliation round failed") ||
		!strings.Contains(logged, "scanned=0") ||
		!strings.Contains(logged, "failed=1") ||
		strings.Contains(logged, "top-secret") ||
		strings.Contains(logged, "storage password") {
		t.Fatalf("unsafe or missing reconciler log: %q", logged)
	}
}

func TestDatabaseProvisioningAuditCompletionFailureWarnsWithoutSecret(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	repository.completeAuditErr = errors.New("audit password=top-secret")
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	provisioning := newProvisioningServiceForTest(
		t,
		repository,
		&databaseProvisionerFake{},
		func(options *DatabaseProvisioningOptions) { options.Logger = logger },
	)
	if _, err := provisioning.Provision(
		context.Background(),
		ProvisionDatabaseAccountRequest{
			InstanceID: "instance-1", AdminAccountID: "admin-1",
			Grants: []DBGrant{{Database: "orders", Privilege: "read"}},
		},
	); err != nil {
		t.Fatalf("provision account: %v", err)
	}
	if logged := logs.String(); !strings.Contains(logged, "audit completion failed") ||
		strings.Contains(logged, "top-secret") {
		t.Fatalf("unsafe or missing audit completion warning: %q", logged)
	}
}
