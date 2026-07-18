package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDatabaseProvisioningServiceOwnsUpstreamIdentityAndSecret(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	provisioner := &databaseProvisionerFake{
		onCreate: func() { repository.recordEvent("upstream_create") },
	}
	provisioning := newProvisioningServiceForTest(t, repository, provisioner, nil)

	result, err := provisioning.Provision(
		context.Background(),
		ProvisionDatabaseAccountRequest{
			InstanceID: "instance-1", AdminAccountID: "admin-1",
			Host:   "10.0.0.8",
			Grants: []DBGrant{{Database: "orders", Privilege: "read"}},
			Actor:  DatabaseProvisioningActor{UserID: "operator-1", Username: "operator"},
		},
	)
	if err != nil {
		t.Fatalf("provision account: %v", err)
	}
	input := repository.createInput
	token := strings.TrimPrefix(input.ID, "jmo_")
	if len(token) != 20 || input.Username != "jm_"+token {
		t.Fatalf("upstream identity is not operation-owned: %#v", input)
	}
	if input.Password == "" || input.Password != provisioner.createdPassword {
		t.Fatal("service did not generate one consistent upstream secret")
	}
	if input.Host != "10.0.0.8" ||
		input.GrantsJSON != `[{"database":"orders","privilege":"read"}]` {
		t.Fatalf("saga intent was not persisted: %#v", input)
	}
	if result.Account.Status != "active" || result.Account.Username != input.Username {
		t.Fatalf("unexpected local account result: %#v", result.Account)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if bytes.Contains(encoded, []byte(input.Password)) ||
		bytes.Contains(encoded, []byte("generated_password")) {
		t.Fatalf("provisioning result exposed upstream credentials: %s", encoded)
	}
	if got := repository.eventsSnapshot(); len(got) < 3 ||
		got[0] != "audit_begin" || got[1] != "operation_create" ||
		got[2] != "upstream_create" {
		t.Fatalf("audit intent was not persisted before side effects: %#v", got)
	}
	if len(repository.audits) != 1 || repository.audits[0].Result != "success" {
		t.Fatalf("credential use audit was not completed: %#v", repository.audits)
	}
}

func TestDatabaseProvisioningServiceFailsClosedWhenAuditIntentFails(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	repository.auditErr = errors.New("audit unavailable password=top-secret")
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
		t.Fatalf("provision error = %v, want fail-closed error", err)
	}
	if repository.createCalls != 0 || provisioner.createCalls != 0 {
		t.Fatalf(
			"audit failure allowed side effects: operations=%d upstream=%d",
			repository.createCalls,
			provisioner.createCalls,
		)
	}
}

func TestDatabaseProvisioningServiceListFailsClosedWhenAuditIntentFails(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	repository.auditErr = errors.New("audit unavailable")
	provisioner := &databaseProvisionerFake{}
	provisioning := newProvisioningServiceForTest(t, repository, provisioner, nil)
	_, err := provisioning.ListDatabases(
		context.Background(),
		ListProvisioningDatabasesRequest{
			InstanceID: "instance-1", AdminAccountID: "admin-1",
		},
	)
	if !errors.Is(err, ErrDatabaseProvisioningFailed) {
		t.Fatalf("list error = %v, want fail-closed error", err)
	}
	if provisioner.listCalls != 0 {
		t.Fatalf("audit failure used administrator credential: %d", provisioner.listCalls)
	}
}

func TestDatabaseProvisioningServicePropagatesRandomSourceFailure(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	provisioner := &databaseProvisionerFake{}
	provisioning := newProvisioningServiceForTest(
		t,
		repository,
		provisioner,
		func(options *DatabaseProvisioningOptions) {
			options.Random = failingProvisioningRandomReader{}
		},
	)
	_, err := provisioning.Provision(
		context.Background(),
		ProvisionDatabaseAccountRequest{
			InstanceID: "instance-1", AdminAccountID: "admin-1",
			Host:   "10.0.0.8",
			Grants: []DBGrant{{Database: "orders", Privilege: "read"}},
		},
	)
	if err == nil || !errors.Is(err, errProvisioningRandomSource) {
		t.Fatalf("provision error = %v, want random source failure", err)
	}
	if repository.createCalls != 0 || provisioner.createCalls != 0 {
		t.Fatalf(
			"random failure performed side effects: operations=%d upstream=%d",
			repository.createCalls,
			provisioner.createCalls,
		)
	}
}

func TestDatabaseProvisioningServiceRejectsWildcardHostBeforeOperation(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	provisioning := newProvisioningServiceForTest(
		t,
		repository,
		&databaseProvisionerFake{},
		nil,
	)
	_, err := provisioning.Provision(
		context.Background(),
		ProvisionDatabaseAccountRequest{
			InstanceID: "instance-1", AdminAccountID: "admin-1",
			Host:   "%",
			Grants: []DBGrant{{Database: "orders", Privilege: "read"}},
		},
	)
	if !errors.Is(err, ErrInvalidDatabaseProvisioningRequest) {
		t.Fatalf("wildcard host error = %v, want invalid request", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("wildcard host created operation: %d", repository.createCalls)
	}
}

func TestDatabaseProvisioningServiceDoesNotDropWhenCreateWasNotSent(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	provisioner := &databaseProvisionerFake{
		createResult: DatabaseAccountCreateResult{
			Disposition: DatabaseAccountCreateNotSent,
		},
		createErr: context.Canceled,
	}
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
		t.Fatalf("provision error = %v, want failure", err)
	}
	if provisioner.dropCalls != 0 {
		t.Fatalf("CREATE-not-sent result triggered DROP: %d", provisioner.dropCalls)
	}
	operations := repository.operationsSnapshot()
	if len(operations) != 1 ||
		operations[0].Stage != ProvisioningStageNotCreated ||
		operations[0].LeaseToken != "" {
		t.Fatalf("deterministically uncreated operation was not retained as a terminal tombstone: %#v", operations)
	}
}

func TestDatabaseProvisioningServiceDropsOnlyGeneratedIdentityAfterUncertainCreate(t *testing.T) {
	requestContext, cancelRequest := context.WithCancel(context.Background())
	repository := newProvisioningRepositoryFake()
	provisioner := &databaseProvisionerFake{
		createResult: DatabaseAccountCreateResult{
			Disposition: DatabaseAccountCreateMayBeApplied,
		},
		createErr: errors.New("CREATE response lost password=top-secret"),
		drop: func(ctx context.Context, username string) error {
			if ctx.Err() != nil {
				t.Fatalf("cleanup started with canceled guarded context: %v", ctx.Err())
			}
			cancelRequest()
			if !errors.Is(ctx.Err(), context.Canceled) {
				t.Fatalf("cleanup side effect ignored request cancellation: %v", ctx.Err())
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("cleanup has no independent timeout")
			}
			if !strings.HasPrefix(username, "jm_") {
				t.Fatalf("cleanup targeted caller identity: %q", username)
			}
			return errors.New("DROP failed password=top-secret")
		},
	}
	provisioning := newProvisioningServiceForTest(t, repository, provisioner, nil)
	_, err := provisioning.Provision(
		requestContext,
		ProvisionDatabaseAccountRequest{
			InstanceID: "instance-1", AdminAccountID: "admin-1",
			Host:   "10.0.0.8",
			Grants: []DBGrant{{Database: "orders", Privilege: "read"}},
		},
	)
	if !errors.Is(err, ErrDatabaseProvisioningCleanupRequired) {
		t.Fatalf("provision error = %v, want cleanup-required", err)
	}
	if provisioner.dropCalls != 1 {
		t.Fatalf("detached cleanup calls = %d, want 1", provisioner.dropCalls)
	}
	if !containsProvisioningStage(
		repository.transitionHistory,
		ProvisioningStageCreateUncertain,
	) {
		t.Fatalf(
			"uncertain CREATE outcome was not persisted explicitly: %#v",
			repository.transitionHistory,
		)
	}
	operations := repository.operationsSnapshot()
	if len(operations) != 1 ||
		operations[0].Stage != ProvisioningStageCleanupRequired ||
		operations[0].CleanupStatus != ProvisioningCleanupFailed ||
		operations[0].LastError != ProvisioningErrorCleanupFailed {
		t.Fatalf("cleanup clue was not retained safely: %#v", operations)
	}
}

func TestDatabaseProvisioningServiceNeverCompensatesUnknownActivationCommit(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	activationCalls := 0
	repository.activate = func(
		operation DatabaseProvisioningOperation,
	) (ProvisionedDatabaseAccount, bool, error) {
		activationCalls++
		account := fakeProvisionedAccount(operation)
		if activationCalls == 1 {
			repository.recordActivated(operation.ID, account)
			return ProvisionedDatabaseAccount{}, false, errors.New("commit response lost")
		}
		return account, true, nil
	}
	provisioner := &databaseProvisionerFake{}
	provisioning := newProvisioningServiceForTest(t, repository, provisioner, nil)
	result, err := provisioning.Provision(
		context.Background(),
		ProvisionDatabaseAccountRequest{
			InstanceID: "instance-1", AdminAccountID: "admin-1",
			Host:   "10.0.0.8",
			Grants: []DBGrant{{Database: "orders", Privilege: "read"}},
		},
	)
	if err != nil || result.Account.Status != "active" {
		t.Fatalf("confirm activation = %#v, %v", result, err)
	}
	if repository.activateCalls != 2 || provisioner.dropCalls != 0 {
		t.Fatalf("unknown activation commit was compensated: activations=%d drops=%d",
			repository.activateCalls, provisioner.dropCalls)
	}
}

func TestDatabaseProvisioningServiceLeavesActivationPendingForReconcile(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	repository.activate = func(
		DatabaseProvisioningOperation,
	) (ProvisionedDatabaseAccount, bool, error) {
		return ProvisionedDatabaseAccount{}, false, errors.New("storage unavailable")
	}
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
		t.Fatalf("activation error = %v, want provisioning failure", err)
	}
	if provisioner.dropCalls != 0 {
		t.Fatalf("activation uncertainty triggered DROP: %d", provisioner.dropCalls)
	}
	operations := repository.operationsSnapshot()
	if len(operations) != 1 ||
		operations[0].Stage != ProvisioningStageActivationPending ||
		operations[0].LeaseToken != "" {
		t.Fatalf("activation was not released for retry: %#v", operations)
	}
}

func TestDatabaseProvisioningSideEffectsCannotOutliveLease(t *testing.T) {
	repository := newProvisioningRepositoryFake()
	provisioner := &databaseProvisionerFake{
		grant: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
				return errors.New("grant remained unbounded")
			}
		},
	}
	provisioning := newProvisioningServiceForTest(
		t,
		repository,
		provisioner,
		func(options *DatabaseProvisioningOptions) {
			options.CleanupTimeout = 20 * time.Millisecond
			options.LeaseDuration = 100 * time.Millisecond
		},
	)
	started := time.Now()
	_, err := provisioning.Provision(
		context.Background(),
		ProvisionDatabaseAccountRequest{
			InstanceID: "instance-1", AdminAccountID: "admin-1",
			Host:   "10.0.0.8",
			Grants: []DBGrant{{Database: "orders", Privilege: "read"}},
		},
	)
	if !errors.Is(err, ErrDatabaseProvisioningFailed) {
		t.Fatalf("bounded grant error = %v, want provisioning failure", err)
	}
	if elapsed := time.Since(started); elapsed >= 300*time.Millisecond {
		t.Fatalf("upstream side effect outlived lease safety window: %s", elapsed)
	}
	if provisioner.dropCalls != 1 {
		t.Fatalf("bounded grant failure cleanup calls = %d, want 1", provisioner.dropCalls)
	}
}
