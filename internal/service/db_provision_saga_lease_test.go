package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDatabaseProvisioningRenewFailurePreventsCreate(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	repository.renewDenied = true
	provisioner := &databaseProvisionerFake{}
	provisioning := newProvisioningServiceForTest(t, repository, provisioner, nil)

	_, err := provisioning.Provision(
		context.Background(),
		ProvisionDatabaseAccountRequest{
			InstanceID: "instance-1", AdminAccountID: "admin-1",
			Host:   "10.0.0.8",
			Grants: []DBGrant{{Database: "orders", Privilege: "read"}},
		},
	)
	if !errors.Is(err, ErrDatabaseProvisioningFailed) {
		t.Fatalf("provision error = %v, want fail closed", err)
	}
	if provisioner.createCalls != 0 || provisioner.grantCalls != 0 || provisioner.dropCalls != 0 {
		t.Fatalf("renew failure invoked upstream: create=%d grant=%d drop=%d",
			provisioner.createCalls, provisioner.grantCalls, provisioner.dropCalls)
	}
}

func TestDatabaseProvisioningRenewFailurePreventsReconcileDrop(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	operation := fakeReconcileOperation(
		"renewfail00000000001",
		ProvisioningStageCleanupRequired,
		ProvisioningCleanupRequired,
		time.Now().UTC(),
	)
	repository.putOperation(operation)
	repository.renewDenied = true
	provisioner := &databaseProvisionerFake{}
	provisioning := newProvisioningServiceForTest(t, repository, provisioner, nil)

	result, err := provisioning.Reconcile(context.Background(), 1)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.Failed != 1 || provisioner.dropCalls != 0 {
		t.Fatalf("renew failure invoked DROP: result=%#v drops=%d", result, provisioner.dropCalls)
	}
}

func TestDatabaseProvisioningRenewFailurePreventsGrant(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	repository.renewDeniedAt = 2
	provisioner := &databaseProvisionerFake{}
	provisioning := newProvisioningServiceForTest(t, repository, provisioner, nil)

	_, err := provisioning.Provision(
		context.Background(),
		ProvisionDatabaseAccountRequest{
			InstanceID: "instance-1", AdminAccountID: "admin-1",
			Host:   "10.0.0.8",
			Grants: []DBGrant{{Database: "orders", Privilege: "read"}},
		},
	)
	if !errors.Is(err, ErrDatabaseProvisioningFailed) {
		t.Fatalf("provision error = %v, want fail closed", err)
	}
	if provisioner.createCalls != 1 || provisioner.grantCalls != 0 || provisioner.dropCalls != 0 {
		t.Fatalf("grant renewal failure invoked upstream: create=%d grant=%d drop=%d",
			provisioner.createCalls, provisioner.grantCalls, provisioner.dropCalls)
	}
}

func TestDatabaseProvisioningRenewErrorPreventsCreate(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	repository.renewErr = errors.New("renew database lease failed")
	provisioner := &databaseProvisionerFake{}
	provisioning := newProvisioningServiceForTest(t, repository, provisioner, nil)

	_, err := provisioning.Provision(
		context.Background(),
		ProvisionDatabaseAccountRequest{
			InstanceID: "instance-1", AdminAccountID: "admin-1",
			Host: "10.0.0.8", Grants: []DBGrant{{Database: "orders", Privilege: "read"}},
		},
	)
	if !errors.Is(err, ErrDatabaseProvisioningFailed) {
		t.Fatalf("provision error = %v, want fail closed", err)
	}
	if provisioner.createCalls != 0 || provisioner.grantCalls != 0 || provisioner.dropCalls != 0 {
		t.Fatalf("renew error invoked upstream: create=%d grant=%d drop=%d",
			provisioner.createCalls, provisioner.grantCalls, provisioner.dropCalls)
	}
}

func TestDatabaseProvisioningRenewWindowIncludesRenewRoundTrip(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	repository.renewDelay = 75 * time.Millisecond
	repository.renewWindow = 20 * time.Millisecond
	provisioner := &databaseProvisionerFake{}
	provisioning := newProvisioningServiceForTest(
		t, repository, provisioner,
		func(options *DatabaseProvisioningOptions) {
			options.CleanupTimeout = 5 * time.Millisecond
			options.LeaseDuration = 100 * time.Millisecond
		},
	)

	_, err := provisioning.Provision(
		context.Background(),
		ProvisionDatabaseAccountRequest{
			InstanceID: "instance-1", AdminAccountID: "admin-1",
			Host: "10.0.0.8", Grants: []DBGrant{{Database: "orders", Privilege: "read"}},
		},
	)
	if !errors.Is(err, ErrDatabaseProvisioningFailed) {
		t.Fatalf("provision error = %v, want fail closed", err)
	}
	if provisioner.createCalls != 0 || provisioner.grantCalls != 0 || provisioner.dropCalls != 0 {
		t.Fatalf("expired renew window invoked upstream: create=%d grant=%d drop=%d",
			provisioner.createCalls, provisioner.grantCalls, provisioner.dropCalls)
	}
}

func TestDatabaseProvisioningAdministratorLocalClockSkewDoesNotChangeLeaseOutcome(t *testing.T) {
	for _, offset := range []time.Duration{6 * time.Hour, -6 * time.Hour} {
		t.Run(offset.String(), func(t *testing.T) {
			repository := newProvisioningRepositoryFake()
			provisioner := &databaseProvisionerFake{}
			provisioning := newProvisioningServiceForTest(
				t, repository, provisioner,
				func(options *DatabaseProvisioningOptions) {
					options.Now = func() time.Time { return time.Now().Add(offset) }
				},
			)
			_, err := provisioning.Provision(
				context.Background(),
				ProvisionDatabaseAccountRequest{
					InstanceID: "instance-1", AdminAccountID: "admin-1",
					Host: "10.0.0.8", Grants: []DBGrant{{Database: "orders", Privilege: "read"}},
				},
			)
			if err != nil {
				t.Fatalf("provision with local clock offset %s: %v", offset, err)
			}
			if provisioner.createCalls != 1 || provisioner.grantCalls != 1 || provisioner.dropCalls != 0 {
				t.Fatalf("local clock offset changed upstream outcome: create=%d grant=%d drop=%d",
					provisioner.createCalls, provisioner.grantCalls, provisioner.dropCalls)
			}
		})
	}
}

func TestDatabaseProvisioningExpiredLeasePreventsCreateAfterProcessPause(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	repository.expireOnRenew = true
	provisioner := &databaseProvisionerFake{}
	provisioning := newProvisioningServiceForTest(t, repository, provisioner, nil)

	_, err := provisioning.Provision(
		context.Background(),
		ProvisionDatabaseAccountRequest{
			InstanceID: "instance-1", AdminAccountID: "admin-1",
			Host:   "10.0.0.8",
			Grants: []DBGrant{{Database: "orders", Privilege: "read"}},
		},
	)
	if !errors.Is(err, ErrDatabaseProvisioningFailed) || provisioner.createCalls != 0 {
		t.Fatalf("expired worker invoked CREATE: error=%v calls=%d", err, provisioner.createCalls)
	}
}

func TestDatabaseProvisioningTimedOutWorkerCannotCommitOrCompensateAfterReclaim(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	timedOut := make(chan struct{}, 1)
	releaseGrant := make(chan struct{})
	provisioner := &databaseProvisionerFake{
		grant: func(ctx context.Context) error {
			<-ctx.Done()
			timedOut <- struct{}{}
			<-releaseGrant
			return ctx.Err()
		},
	}
	workerOne := newProvisioningServiceForTest(
		t, repository, provisioner,
		func(options *DatabaseProvisioningOptions) {
			options.CleanupTimeout = 20 * time.Millisecond
			options.LeaseDuration = 100 * time.Millisecond
			options.WorkerID = "worker-one"
		},
	)
	workerTwo := newProvisioningServiceForTest(
		t, repository, provisioner,
		func(options *DatabaseProvisioningOptions) {
			options.CleanupTimeout = 20 * time.Millisecond
			options.LeaseDuration = 100 * time.Millisecond
			options.WorkerID = "worker-two"
		},
	)
	result := make(chan error, 1)
	go func() {
		_, err := workerOne.Provision(
			context.Background(),
			ProvisionDatabaseAccountRequest{
				InstanceID: "instance-1", AdminAccountID: "admin-1",
				Host:   "10.0.0.8",
				Grants: []DBGrant{{Database: "orders", Privilege: "read"}},
			},
		)
		result <- err
	}()
	select {
	case <-timedOut:
	case <-time.After(time.Second):
		t.Fatal("GRANT did not stop at guarded deadline")
	}

	var reclaimed DatabaseProvisioningOperation
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		operations := repository.operationsSnapshot()
		if len(operations) == 1 {
			candidate, err := repository.DatabaseProvisioningOperation(context.Background(), operations[0].ID)
			if err == nil {
				claimed, ok, claimErr := workerTwo.claimForReconcile(context.Background(), candidate)
				if claimErr != nil {
					t.Fatalf("second worker claim: %v", claimErr)
				}
				if ok {
					reclaimed = claimed
					break
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if reclaimed.LeaseOwner != "worker-two" {
		t.Fatalf("second worker did not reclaim expired lease: %#v", reclaimed)
	}
	close(releaseGrant)
	select {
	case err := <-result:
		if !errors.Is(err, ErrDatabaseProvisioningCleanupRequired) {
			t.Fatalf("old worker result = %v, want fenced cleanup failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("old worker did not return after GRANT release")
	}
	if provisioner.dropCalls != 0 {
		t.Fatalf("stale worker compensated after new claim: drops=%d", provisioner.dropCalls)
	}
	operations := repository.operationsSnapshot()
	if len(operations) != 1 || operations[0].LeaseOwner != "worker-two" {
		t.Fatalf("stale worker committed after reclaim: %#v", operations)
	}
}
