package service

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func idempotentProvisioningRequest(actor, key string, grants []DBGrant) ProvisionDatabaseAccountRequest {
	return ProvisionDatabaseAccountRequest{
		InstanceID: "instance-1", AdminAccountID: "admin-1",
		Grants: grants, Actor: DatabaseProvisioningActor{UserID: actor}, IdempotencyKey: key,
	}
}

func TestDatabaseProvisioningIdempotencyReusesActiveOperation(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	provisioner := &databaseProvisionerFake{}
	provisioning := newProvisioningServiceForTest(t, repository, provisioner, nil)
	request := idempotentProvisioningRequest("operator-1", "provision-retry-key-001", []DBGrant{{Database: "orders", Privilege: "read"}})
	first, err := provisioning.Provision(context.Background(), request)
	if err != nil {
		t.Fatalf("first provisioning: %v", err)
	}
	provisioner.accountHost = "%"
	second, err := provisioning.Provision(context.Background(), request)
	if err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	if first.OperationID == "" || first.OperationID != second.OperationID || first.Account.ID != second.Account.ID {
		t.Fatalf("retry did not return stable result: %#v %#v", first, second)
	}
	if provisioner.resolveCalls != 1 || provisioner.createCalls != 1 || provisioner.grantCalls != 1 {
		t.Fatalf(
			"retry repeated host resolution or upstream effects: resolve=%d create=%d grant=%d",
			provisioner.resolveCalls,
			provisioner.createCalls,
			provisioner.grantCalls,
		)
	}
}

func TestDatabaseProvisioningIdempotencyRejectsRequestMismatch(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	provisioning := newProvisioningServiceForTest(t, repository, &databaseProvisionerFake{}, nil)
	request := idempotentProvisioningRequest("operator-1", "provision-mismatch-key-001", []DBGrant{{Database: "orders", Privilege: "read"}})
	if _, err := provisioning.Provision(context.Background(), request); err != nil {
		t.Fatalf("first provisioning: %v", err)
	}
	request.Remark = "changed request"
	if _, err := provisioning.Provision(context.Background(), request); !errors.Is(err, ErrDatabaseProvisioningIdempotencyConflict) {
		t.Fatalf("mismatch error = %v, want conflict", err)
	}
}

func TestDatabaseProvisioningIdempotencyNormalizesGrantOrderAndActorScope(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	provisioner := &databaseProvisionerFake{}
	provisioning := newProvisioningServiceForTest(t, repository, provisioner, nil)
	key := "provision-grant-order-key-001"
	first := idempotentProvisioningRequest("operator-1", key, []DBGrant{{Database: "zeta", Privilege: "read"}, {Database: "alpha", Privilege: "readwrite"}})
	if _, err := provisioning.Provision(context.Background(), first); err != nil {
		t.Fatalf("first provisioning: %v", err)
	}
	second := idempotentProvisioningRequest("operator-1", key, []DBGrant{{Database: "alpha", Privilege: "readwrite"}, {Database: "zeta", Privilege: "read"}})
	if _, err := provisioning.Provision(context.Background(), second); err != nil {
		t.Fatalf("grant reorder retry: %v", err)
	}
	otherActor := idempotentProvisioningRequest("operator-2", key, second.Grants)
	if _, err := provisioning.Provision(context.Background(), otherActor); err != nil {
		t.Fatalf("other actor provisioning: %v", err)
	}
	if provisioner.createCalls != 2 || provisioner.grantCalls != 2 {
		t.Fatalf("actor scope or grant normalization failed: create=%d grant=%d", provisioner.createCalls, provisioner.grantCalls)
	}
}

func TestDatabaseProvisioningIdempotencyDeduplicatesCanonicalGrants(t *testing.T) {
	request := idempotentProvisioningRequest(
		"operator-1",
		"provision-deduplicate-key-001",
		[]DBGrant{{Database: "orders", Privilege: "read"}},
	)
	firstHash, firstGrants, err := canonicalProvisioningRequestHash(request)
	if err != nil {
		t.Fatalf("canonical single grant request: %v", err)
	}
	request.Grants = append(request.Grants, DBGrant{Database: "orders", Privilege: "read"})
	secondHash, secondGrants, err := canonicalProvisioningRequestHash(request)
	if err != nil {
		t.Fatalf("canonical duplicate grant request: %v", err)
	}
	if firstHash != secondHash || len(firstGrants) != 1 || len(secondGrants) != 1 {
		t.Fatalf("duplicate grants changed canonical request: hash=%q/%q grants=%#v/%#v", firstHash, secondHash, firstGrants, secondGrants)
	}
}

func TestDatabaseProvisioningIdempotencyRetainsFailedCleanupTombstone(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	provisioner := &databaseProvisionerFake{
		grant: func(context.Context) error { return errors.New("grant denied") },
	}
	provisioning := newProvisioningServiceForTest(t, repository, provisioner, nil)
	request := idempotentProvisioningRequest(
		"operator-1",
		"provision-cleanup-tombstone-key-001",
		[]DBGrant{{Database: "orders", Privilege: "read"}},
	)
	if _, err := provisioning.Provision(context.Background(), request); !errors.Is(err, ErrDatabaseProvisioningFailed) {
		t.Fatalf("first provisioning error = %v, want terminal failure", err)
	}
	operations := repository.operationsSnapshot()
	if len(operations) != 1 || operations[0].Stage != ProvisioningStageNotCreated ||
		operations[0].IdempotencyKey != request.IdempotencyKey || operations[0].LeaseToken != "" {
		t.Fatalf("cleanup did not retain idempotency tombstone: %#v", operations)
	}
	if _, err := provisioning.Provision(context.Background(), request); !errors.Is(err, ErrDatabaseProvisioningFailed) {
		t.Fatalf("same key retry error = %v, want stable terminal failure", err)
	}
	if provisioner.resolveCalls != 1 || provisioner.createCalls != 1 ||
		provisioner.grantCalls != 1 || provisioner.dropCalls != 1 {
		t.Fatalf(
			"retry repeated host resolution or upstream side effects: resolve=%d create=%d grant=%d drop=%d",
			provisioner.resolveCalls,
			provisioner.createCalls,
			provisioner.grantCalls,
			provisioner.dropCalls,
		)
	}
}

func TestDatabaseProvisioningIdempotencyConcurrentRequestRunsUpstreamOnce(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	started := make(chan struct{})
	release := make(chan struct{})
	provisioner := &databaseProvisionerFake{onCreate: func() { close(started); <-release }}
	provisioning := newProvisioningServiceForTest(t, repository, provisioner, nil)
	request := idempotentProvisioningRequest("operator-1", "provision-concurrent-key-001", []DBGrant{{Database: "orders", Privilege: "read"}})
	var firstErr error
	var wait sync.WaitGroup
	wait.Add(1)
	go func() { defer wait.Done(); _, firstErr = provisioning.Provision(context.Background(), request) }()
	<-started
	if _, err := provisioning.Provision(context.Background(), request); !errors.Is(err, ErrDatabaseProvisioningInProgress) {
		t.Fatalf("parallel retry error = %v, want in progress", err)
	}
	close(release)
	wait.Wait()
	if firstErr != nil || provisioner.resolveCalls != 1 ||
		provisioner.createCalls != 1 || provisioner.grantCalls != 1 {
		t.Fatalf(
			"concurrent provisioning repeated host resolution or upstream work: err=%v resolve=%d create=%d grant=%d",
			firstErr,
			provisioner.resolveCalls,
			provisioner.createCalls,
			provisioner.grantCalls,
		)
	}
}
