package service

import (
	"context"
	"errors"
	"io"
	"jianmen/internal/model"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newProvisioningServiceForTest(
	t *testing.T,
	repository *provisioningRepositoryFake,
	provisioner *databaseProvisionerFake,
	configure func(*DatabaseProvisioningOptions),
) *DatabaseProvisioningService {
	t.Helper()
	options := DatabaseProvisioningOptions{
		Random:              strings.NewReader(strings.Repeat("Q", 4096)),
		CleanupTimeout:      time.Second,
		LeaseDuration:       2 * time.Second,
		ReconcileStaleAfter: time.Second,
		WorkerID:            "test-worker",
	}
	if configure != nil {
		configure(&options)
	}
	provisioning, err := NewDatabaseProvisioningService(repository, provisioner, options)
	if err != nil {
		t.Fatalf("new provisioning service: %v", err)
	}
	return provisioning
}

type provisioningRepositoryFake struct {
	mu                sync.Mutex
	instance          model.DatabaseInstance
	admin             model.DatabaseAccount
	operations        map[string]DatabaseProvisioningOperation
	activated         map[string]ProvisionedDatabaseAccount
	createInput       DatabaseProvisioningOperationInput
	createCalls       int
	listOrder         []string
	listErr           error
	listCalls         atomic.Int32
	claimDenied       map[string]bool
	transitionDenied  map[string]int
	renewDenied       bool
	renewDeniedAt     int
	renewErr          error
	renewDelay        time.Duration
	renewWindow       time.Duration
	renewCalls        int
	expireOnRenew     bool
	transitionHistory []string
	activate          func(DatabaseProvisioningOperation) (ProvisionedDatabaseAccount, bool, error)
	activateCalls     int
	audits            []DatabaseProvisioningAudit
	auditErr          error
	completeAuditErr  error
	events            []string
}

func newProvisioningRepositoryFake() *provisioningRepositoryFake {
	instance := model.DatabaseInstance{
		ID: "instance-1", Name: "orders", Protocol: "mysql",
		Address: "127.0.0.1", Port: 3306, Status: "active",
	}
	return &provisioningRepositoryFake{
		instance: instance,
		admin: model.DatabaseAccount{
			ID: "admin-1", InstanceID: instance.ID, Username: "root",
			Password: model.NewEncryptedField("admin-secret"), Status: "active",
			Instance: instance,
		},
		operations:       make(map[string]DatabaseProvisioningOperation),
		activated:        make(map[string]ProvisionedDatabaseAccount),
		claimDenied:      make(map[string]bool),
		transitionDenied: make(map[string]int),
	}
}

func (f *provisioningRepositoryFake) DatabaseProvisioningAdmin(
	context.Context,
	string,
	string,
) (model.DatabaseInstance, model.DatabaseAccount, error) {
	return f.instance, f.admin, nil
}

func (f *provisioningRepositoryFake) CreateDatabaseProvisioningOperation(
	_ context.Context,
	input DatabaseProvisioningOperationInput,
) (DatabaseProvisioningOperation, DatabaseProvisioningLeaseWindow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	f.createInput = input
	f.events = append(f.events, "operation_create")
	expires := time.Now().UTC().Add(input.Lease.Duration)
	operation := DatabaseProvisioningOperation{
		ID: input.ID, InstanceID: input.InstanceID, AdminAccountID: input.AdminAccountID,
		ActorID: input.ActorID, IdempotencyKey: input.IdempotencyKey, RequestHash: input.RequestHash,
		Username: input.Username, Password: input.Password, Host: input.Host,
		GrantsJSON: input.GrantsJSON, Group: input.Group, Remark: input.Remark,
		ExpiresAt: input.ExpiresAt, Stage: ProvisioningStageReserved,
		CleanupStatus: ProvisioningCleanupNone, Revision: 1,
		LeaseOwner: input.Lease.Owner, LeaseToken: input.Lease.Token,
		LeaseExpiresAt: &expires, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	f.operations[operation.ID] = operation
	return operation, DatabaseProvisioningLeaseWindow{Remaining: input.Lease.Duration}, nil
}

func (f *provisioningRepositoryFake) CreateOrGetDatabaseProvisioningOperation(
	_ context.Context,
	input DatabaseProvisioningOperationInput,
) (DatabaseProvisioningOperation, DatabaseProvisioningLeaseWindow, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, operation := range f.operations {
		if operation.ActorID != input.ActorID || operation.IdempotencyKey != input.IdempotencyKey {
			continue
		}
		if operation.RequestHash != input.RequestHash {
			return DatabaseProvisioningOperation{}, DatabaseProvisioningLeaseWindow{}, false, ErrDatabaseProvisioningIdempotencyConflict
		}
		return operation, DatabaseProvisioningLeaseWindow{}, false, nil
	}
	f.createCalls++
	f.createInput = input
	f.events = append(f.events, "operation_create")
	expires := time.Now().UTC().Add(input.Lease.Duration)
	operation := DatabaseProvisioningOperation{
		ID: input.ID, InstanceID: input.InstanceID, AdminAccountID: input.AdminAccountID,
		ActorID: input.ActorID, IdempotencyKey: input.IdempotencyKey, RequestHash: input.RequestHash,
		Username: input.Username, Password: input.Password, Host: input.Host, GrantsJSON: input.GrantsJSON,
		Group: input.Group, Remark: input.Remark, ExpiresAt: input.ExpiresAt,
		Stage: ProvisioningStageReserved, CleanupStatus: ProvisioningCleanupNone, Revision: 1,
		LeaseOwner: input.Lease.Owner, LeaseToken: input.Lease.Token, LeaseExpiresAt: &expires,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	f.operations[operation.ID] = operation
	return operation, DatabaseProvisioningLeaseWindow{Remaining: input.Lease.Duration}, true, nil
}

func (f *provisioningRepositoryFake) DatabaseProvisioningOperationByIdempotency(
	_ context.Context,
	actorID, key string,
) (DatabaseProvisioningOperation, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, operation := range f.operations {
		if operation.ActorID == actorID && operation.IdempotencyKey == key {
			return operation, true, nil
		}
	}
	return DatabaseProvisioningOperation{}, false, nil
}

func (f *provisioningRepositoryFake) ProvisionedDatabaseAccountByOperation(
	_ context.Context,
	id string,
) (ProvisionedDatabaseAccount, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	account, ok := f.activated[id]
	return account, ok, nil
}

func (f *provisioningRepositoryFake) DatabaseProvisioningOperation(
	_ context.Context,
	id string,
) (DatabaseProvisioningOperation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	operation, ok := f.operations[id]
	if !ok {
		return DatabaseProvisioningOperation{}, errors.New("operation not found")
	}
	return operation, nil
}

func (f *provisioningRepositoryFake) TransitionDatabaseProvisioningOperation(
	_ context.Context,
	expected DatabaseProvisioningFence,
	transition DatabaseProvisioningTransition,
) (DatabaseProvisioningOperation, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.transitionDenied[transition.Stage] > 0 {
		f.transitionDenied[transition.Stage]--
		return DatabaseProvisioningOperation{}, false, errors.New("transition unavailable")
	}
	operation, ok := f.operations[expected.ID]
	if !ok || !fakeFenceMatches(operation, expected) ||
		operation.LeaseExpiresAt == nil || !operation.LeaseExpiresAt.After(time.Now().UTC()) {
		return DatabaseProvisioningOperation{}, false, nil
	}
	operation.Stage = transition.Stage
	f.transitionHistory = append(f.transitionHistory, transition.Stage)
	operation.CleanupStatus = transition.CleanupStatus
	operation.LastError = transition.LastError
	if transition.IncrementAttempt {
		attempted := time.Now().UTC()
		operation.LastAttemptAt = &attempted
	}
	if transition.IncrementAttempt {
		operation.AttemptCount++
	}
	if transition.ReleaseLease {
		operation.LeaseOwner = ""
		operation.LeaseToken = ""
		operation.LeaseExpiresAt = nil
	}
	operation.Revision++
	operation.UpdatedAt = time.Now().UTC()
	f.operations[operation.ID] = operation
	return operation, true, nil
}

func (f *provisioningRepositoryFake) ListExecutableDatabaseProvisioningOperations(
	context.Context,
	time.Duration,
	int,
) ([]DatabaseProvisioningOperation, error) {
	f.listCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	var operations []DatabaseProvisioningOperation
	if len(f.listOrder) > 0 {
		for _, id := range f.listOrder {
			if operation, ok := f.operations[id]; ok {
				operations = append(operations, operation)
			}
		}
		return operations, nil
	}
	for _, operation := range f.operations {
		operations = append(operations, operation)
	}
	return operations, nil
}

func (f *provisioningRepositoryFake) ClaimDatabaseProvisioningOperation(
	_ context.Context,
	expected DatabaseProvisioningFence,
	lease DatabaseProvisioningLease,
) (DatabaseProvisioningOperation, DatabaseProvisioningLeaseWindow, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	operation, ok := f.operations[expected.ID]
	if !ok || f.claimDenied[expected.ID] || !fakeFenceMatches(operation, expected) {
		return DatabaseProvisioningOperation{}, DatabaseProvisioningLeaseWindow{}, false, nil
	}
	now := time.Now().UTC()
	if operation.LeaseExpiresAt != nil && operation.LeaseExpiresAt.After(now) {
		return DatabaseProvisioningOperation{}, DatabaseProvisioningLeaseWindow{}, false, nil
	}
	expires := now.Add(lease.Duration)
	operation.LeaseOwner = lease.Owner
	operation.LeaseToken = lease.Token
	operation.LeaseExpiresAt = &expires
	operation.Revision++
	f.operations[operation.ID] = operation
	return operation, DatabaseProvisioningLeaseWindow{Remaining: lease.Duration}, true, nil
}

func (f *provisioningRepositoryFake) RenewDatabaseProvisioningOperation(
	_ context.Context,
	expected DatabaseProvisioningFence,
	lease DatabaseProvisioningLease,
) (DatabaseProvisioningOperation, DatabaseProvisioningLeaseWindow, bool, error) {
	f.mu.Lock()
	operation, ok := f.operations[expected.ID]
	f.renewCalls++
	delay := f.renewDelay
	window := f.renewWindow
	renewErr := f.renewErr
	f.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	operation, ok = f.operations[expected.ID]
	if f.expireOnRenew && operation.LeaseExpiresAt != nil {
		expired := time.Now().UTC().Add(-time.Nanosecond)
		operation.LeaseExpiresAt = &expired
		f.operations[operation.ID] = operation
	}
	if renewErr != nil {
		return DatabaseProvisioningOperation{}, DatabaseProvisioningLeaseWindow{}, false, renewErr
	}
	if f.renewDenied || (f.renewDeniedAt > 0 && f.renewCalls == f.renewDeniedAt) ||
		!ok || !fakeFenceMatches(operation, expected) ||
		lease.Owner != expected.LeaseOwner || lease.Token != expected.LeaseToken ||
		operation.LeaseExpiresAt == nil || !operation.LeaseExpiresAt.After(time.Now().UTC()) {
		return DatabaseProvisioningOperation{}, DatabaseProvisioningLeaseWindow{}, false, nil
	}
	expires := time.Now().UTC().Add(lease.Duration)
	operation.LeaseExpiresAt = &expires
	operation.Revision++
	f.operations[operation.ID] = operation
	if window <= 0 {
		window = lease.Duration
	}
	return operation, DatabaseProvisioningLeaseWindow{Remaining: window}, true, nil
}

func (f *provisioningRepositoryFake) ActivateDatabaseProvisioningOperation(
	_ context.Context,
	expected DatabaseProvisioningFence,
) (ProvisionedDatabaseAccount, bool, error) {
	f.mu.Lock()
	f.activateCalls++
	if account, ok := f.activated[expected.ID]; ok {
		f.mu.Unlock()
		return account, true, nil
	}
	operation, ok := f.operations[expected.ID]
	if !ok || !fakeFenceMatches(operation, expected) {
		f.mu.Unlock()
		return ProvisionedDatabaseAccount{}, false, nil
	}
	callback := f.activate
	f.mu.Unlock()
	if callback != nil {
		return callback(operation)
	}
	account := fakeProvisionedAccount(operation)
	f.recordActivated(operation.ID, account)
	return account, true, nil
}

func (f *provisioningRepositoryFake) DeleteDatabaseProvisioningOperation(
	_ context.Context,
	expected DatabaseProvisioningFence,
) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	operation, ok := f.operations[expected.ID]
	if !ok || !fakeFenceMatches(operation, expected) ||
		operation.LeaseExpiresAt == nil || !operation.LeaseExpiresAt.After(time.Now().UTC()) {
		return false, nil
	}
	delete(f.operations, expected.ID)
	return true, nil
}

func (f *provisioningRepositoryFake) BeginDatabaseProvisioningAudit(
	_ context.Context,
	audit DatabaseProvisioningAudit,
) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "audit_begin")
	f.audits = append(f.audits, audit)
	return "audit-1", f.auditErr
}

func (f *provisioningRepositoryFake) CompleteDatabaseProvisioningAudit(
	_ context.Context,
	_ string,
	result string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.audits) > 0 {
		f.audits[len(f.audits)-1].Result = result
	}
	return f.completeAuditErr
}

func (f *provisioningRepositoryFake) recordActivated(
	id string,
	account ProvisionedDatabaseAccount,
) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.activated[id] = account
	if operation, ok := f.operations[id]; ok {
		operation.Stage = "active_managed"
		operation.CleanupStatus = ProvisioningCleanupNone
		operation.LeaseOwner = ""
		operation.LeaseToken = ""
		operation.LeaseExpiresAt = nil
		operation.Revision++
		f.operations[id] = operation
	}
}

func (f *provisioningRepositoryFake) putOperation(operation DatabaseProvisioningOperation) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.operations[operation.ID] = operation
}

func (f *provisioningRepositoryFake) operationsSnapshot() []DatabaseProvisioningOperation {
	f.mu.Lock()
	defer f.mu.Unlock()
	operations := make([]DatabaseProvisioningOperation, 0, len(f.operations))
	for _, operation := range f.operations {
		operations = append(operations, operation)
	}
	return operations
}

func (f *provisioningRepositoryFake) recordEvent(event string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
}

func (f *provisioningRepositoryFake) eventsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events...)
}

type databaseProvisionerFake struct {
	createResult    DatabaseAccountCreateResult
	createErr       error
	grant           func(context.Context) error
	drop            func(context.Context, string) error
	onCreate        func()
	createdPassword string
	createCalls     int
	grantCalls      int
	dropCalls       int
	listCalls       int
	listErr         error
}

func (f *databaseProvisionerFake) ListDatabases(
	context.Context,
	model.DatabaseInstance,
	model.DatabaseAccount,
) ([]string, error) {
	f.listCalls++
	return []string{"orders"}, f.listErr
}

func (f *databaseProvisionerFake) CreateAccount(
	_ context.Context,
	_ model.DatabaseInstance,
	_ model.DatabaseAccount,
	_ string,
	password string,
	_ string,
) (DatabaseAccountCreateResult, error) {
	f.createCalls++
	f.createdPassword = password
	if f.onCreate != nil {
		f.onCreate()
	}
	result := f.createResult
	if result.Disposition == "" {
		result.Disposition = DatabaseAccountCreateApplied
	}
	return result, f.createErr
}

func (f *databaseProvisionerFake) GrantAccount(
	ctx context.Context,
	_ model.DatabaseInstance,
	_ model.DatabaseAccount,
	_ string,
	_ string,
	_ []DBGrant,
) error {
	f.grantCalls++
	if f.grant != nil {
		return f.grant(ctx)
	}
	return nil
}

func (f *databaseProvisionerFake) DropAccount(
	ctx context.Context,
	_ model.DatabaseInstance,
	_ model.DatabaseAccount,
	username string,
	_ string,
) error {
	f.dropCalls++
	if f.drop != nil {
		return f.drop(ctx, username)
	}
	return nil
}

func fakeFenceMatches(
	operation DatabaseProvisioningOperation,
	expected DatabaseProvisioningFence,
) bool {
	return operation.ID == expected.ID &&
		operation.Stage == expected.Stage &&
		operation.CleanupStatus == expected.CleanupStatus &&
		operation.Revision == expected.Revision &&
		operation.LeaseOwner == expected.LeaseOwner &&
		operation.LeaseToken == expected.LeaseToken
}

func containsProvisioningStage(stages []string, wanted string) bool {
	for _, stage := range stages {
		if stage == wanted {
			return true
		}
	}
	return false
}

func fakeProvisionedAccount(
	operation DatabaseProvisioningOperation,
) ProvisionedDatabaseAccount {
	return ProvisionedDatabaseAccount{
		ID: "account-" + operation.ID, InstanceID: operation.InstanceID,
		UniqueName: "db-generated", Username: operation.Username,
		Status: "active", ResourceID: "D001",
	}
}

func fakeReconcileOperation(
	token, stage, cleanup string,
	now time.Time,
) DatabaseProvisioningOperation {
	expired := now.Add(-time.Minute)
	return DatabaseProvisioningOperation{
		ID: "jmo_" + token, InstanceID: "instance-1", AdminAccountID: "admin-1",
		Username: "jm_" + token, Password: "generated-secret", Host: "10.0.0.8",
		GrantsJSON: `[{"database":"orders","privilege":"read"}]`,
		Stage:      stage, CleanupStatus: cleanup, Revision: 1,
		LeaseOwner: "", LeaseToken: "", LeaseExpiresAt: &expired,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}
}

type failingProvisioningRandomReader struct{}

func (failingProvisioningRandomReader) Read([]byte) (int, error) {
	return 0, errProvisioningRandomSource
}

var errProvisioningRandomSource = errors.New("random source failed")

var _ io.Reader = failingProvisioningRandomReader{}
