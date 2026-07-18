package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func TestDatabaseProvisioningOperationClaimIsAtomicAndFenced(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	migrateProvisioningOperationsForStoreTest(t, db)
	now := time.Now().UTC()
	operation := seedStoreProvisioningOperation(t, repository, db, now.Add(-time.Minute))
	setStoreProvisioningOperationState(
		t, db, operation.ID, service.ProvisioningStageCleanupRequired,
		service.ProvisioningCleanupRequired, now.Add(-time.Minute),
	)
	candidates, err := repository.ListExecutableDatabaseProvisioningOperations(
		context.Background(), 30*time.Second, 10,
	)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("list executable operations = %#v, %v", candidates, err)
	}

	type claimResult struct {
		operation service.DatabaseProvisioningOperation
		claimed   bool
		err       error
	}
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	var workers sync.WaitGroup
	for index := 0; index < 2; index++ {
		index := index
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			claimed, _, ok, claimErr := repository.ClaimDatabaseProvisioningOperation(
				context.Background(),
				candidates[0].Fence(),
				service.DatabaseProvisioningLease{
					Owner:    fmt.Sprintf("worker-%d", index),
					Token:    fmt.Sprintf("lease-%d", index),
					Duration: time.Minute,
				},
			)
			results <- claimResult{operation: claimed, claimed: ok, err: claimErr}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	var winner service.DatabaseProvisioningOperation
	claimCount := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("claim operation: %v", result.err)
		}
		if result.claimed {
			claimCount++
			winner = result.operation
		}
	}
	if claimCount != 1 {
		t.Fatalf("atomic claim winners = %d, want 1", claimCount)
	}
	if winner.Revision != candidates[0].Revision+1 || winner.LeaseToken == "" {
		t.Fatalf("claim did not advance fencing revision: %#v", winner)
	}
	if _, updated, err := repository.TransitionDatabaseProvisioningOperation(
		context.Background(),
		candidates[0].Fence(),
		service.DatabaseProvisioningTransition{
			Stage:         service.ProvisioningStageCleanupInProgress,
			CleanupStatus: service.ProvisioningCleanupInProgress,
		},
	); err != nil || updated {
		t.Fatalf("stale fence transition = %t, %v; want rejected", updated, err)
	}
}

func TestDatabaseProvisioningOperationLeaseCanExpireWithoutRevivingOldWorker(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	migrateProvisioningOperationsForStoreTest(t, db)
	now := time.Now().UTC()
	operation := seedStoreProvisioningOperation(t, repository, db, now.Add(-time.Second))
	candidate, err := repository.DatabaseProvisioningOperation(context.Background(), operation.ID)
	if err != nil {
		t.Fatalf("load operation: %v", err)
	}
	first, _, claimed, err := repository.ClaimDatabaseProvisioningOperation(
		context.Background(),
		candidate.Fence(),
		service.DatabaseProvisioningLease{
			Owner: "worker-a", Token: "lease-a", Duration: time.Minute,
		},
	)
	if err != nil || !claimed {
		t.Fatalf("first claim = %t, %v", claimed, err)
	}
	if _, _, claimed, err := repository.ClaimDatabaseProvisioningOperation(
		context.Background(),
		first.Fence(),
		service.DatabaseProvisioningLease{
			Owner: "worker-b", Token: "lease-b", Duration: time.Minute,
		},
	); err != nil || claimed {
		t.Fatalf("unexpired lease was stolen = %t, %v", claimed, err)
	}
	if err := db.Model(&model.DatabaseProvisioningOperation{}).
		Where("id = ?", first.ID).
		Update("lease_expires_at", now.Add(-time.Minute)).Error; err != nil {
		t.Fatalf("expire first lease: %v", err)
	}
	second, _, claimed, err := repository.ClaimDatabaseProvisioningOperation(
		context.Background(),
		first.Fence(),
		service.DatabaseProvisioningLease{
			Owner: "worker-b", Token: "lease-b", Duration: time.Minute,
		},
	)
	if err != nil || !claimed {
		t.Fatalf("expired lease was not reclaimed = %t, %v", claimed, err)
	}
	if _, updated, err := repository.TransitionDatabaseProvisioningOperation(
		context.Background(),
		first.Fence(),
		service.DatabaseProvisioningTransition{
			Stage:         service.ProvisioningStageNotCreated,
			CleanupStatus: service.ProvisioningCleanupNone,
		},
	); err != nil || updated {
		t.Fatalf("expired worker mutated reclaimed operation = %t, %v", updated, err)
	}
	if second.Revision <= first.Revision {
		t.Fatalf("reclaim revision did not advance: first=%d second=%d", first.Revision, second.Revision)
	}
}

func TestDatabaseProvisioningActivationMakesOldFenceUnableToDelete(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	migrateProvisioningOperationsForStoreTest(t, db)
	now := time.Now().UTC()
	operation := seedStoreProvisioningOperation(t, repository, db, now.Add(-time.Second))
	setStoreProvisioningOperationState(
		t, db, operation.ID, service.ProvisioningStageActivationPending,
		service.ProvisioningCleanupNone, now.Add(-time.Minute),
	)
	candidate, err := repository.DatabaseProvisioningOperation(context.Background(), operation.ID)
	if err != nil {
		t.Fatalf("load activation operation: %v", err)
	}
	claimed, _, ok, err := repository.ClaimDatabaseProvisioningOperation(
		context.Background(),
		candidate.Fence(),
		service.DatabaseProvisioningLease{
			Owner: "activation-worker", Token: "activation-lease", Duration: time.Minute,
		},
	)
	if err != nil || !ok {
		t.Fatalf("claim activation = %t, %v", ok, err)
	}
	beforeActivation, err := databaseNow(context.Background(), db)
	if err != nil {
		t.Fatalf("read metadata clock before activation: %v", err)
	}
	account, activated, err := repository.ActivateDatabaseProvisioningOperation(
		context.Background(), claimed.Fence(),
	)
	if err != nil || !activated || account.Status != "active" {
		t.Fatalf("activate operation = %#v, %t, %v", account, activated, err)
	}
	var retainedOperation model.DatabaseProvisioningOperation
	if err := db.First(&retainedOperation, "id = ?", operation.ID).Error; err != nil {
		t.Fatalf("load retained operation: %v", err)
	}
	if retainedOperation.Stage != "active_managed" ||
		retainedOperation.ActiveRetainedAt == nil ||
		retainedOperation.TerminalAt != nil ||
		retainedOperation.LeaseOwner != "" ||
		retainedOperation.LeaseToken != "" ||
		retainedOperation.LeaseExpiresAt != nil {
		t.Fatalf("activated operation is not retained safely: %#v", retainedOperation)
	}
	afterActivation, err := databaseNow(context.Background(), db)
	if err != nil {
		t.Fatalf("read metadata clock after activation: %v", err)
	}
	if retainedOperation.ActiveRetainedAt.Before(beforeActivation.Add(-time.Second)) ||
		retainedOperation.ActiveRetainedAt.After(afterActivation.Add(time.Second)) {
		t.Fatalf("activation timestamp did not use metadata clock: %#v", retainedOperation.ActiveRetainedAt)
	}
	var managed model.DatabaseAccount
	if err := db.First(&managed, "id = ?", account.ID).Error; err != nil {
		t.Fatalf("load managed account: %v", err)
	}
	if !managed.Managed || managed.UpstreamHost != operation.Host ||
		managed.ProvisioningOperationID == nil || *managed.ProvisioningOperationID != operation.ID {
		t.Fatalf("activated account lost managed lifecycle metadata: %#v", managed)
	}
	confirmed, active, err := repository.ActivateDatabaseProvisioningOperation(
		context.Background(), candidate.Fence(),
	)
	if err != nil || active || confirmed.ID != "" {
		t.Fatalf("old fence confirmed another worker activation = %#v, %t, %v", confirmed, active, err)
	}
	if deleted, err := repository.DeleteDatabaseProvisioningOperation(
		context.Background(), candidate.Fence(),
	); err != nil || deleted {
		t.Fatalf("old fence deleted activated identity = %t, %v", deleted, err)
	}
	view, err := repository.DatabaseAccount(account.ID)
	if err != nil || view.Status != "active" {
		t.Fatalf("activated account was lost: %#v, %v", view, err)
	}
	var operationCount int64
	if err := db.Model(&model.DatabaseProvisioningOperation{}).
		Where("id = ?", operation.ID).Count(&operationCount).Error; err != nil {
		t.Fatalf("count retained operations: %v", err)
	}
	if operationCount != 1 {
		t.Fatalf("active managed operation count = %d, want 1", operationCount)
	}
}

func TestDatabaseProvisioningActivationExistingAccountRequiresCurrentFence(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *gorm.DB, *service.DatabaseProvisioningFence)
		wantOK bool
	}{
		{
			name:   "correct current fence completes activation",
			wantOK: true,
		},
		{
			name: "wrong token is rejected",
			mutate: func(_ *testing.T, _ *gorm.DB, fence *service.DatabaseProvisioningFence) {
				fence.LeaseToken = "wrong-token"
			},
		},
		{
			name: "old revision is rejected",
			mutate: func(_ *testing.T, _ *gorm.DB, fence *service.DatabaseProvisioningFence) {
				fence.Revision--
			},
		},
		{
			name: "expired lease is rejected",
			mutate: func(t *testing.T, db *gorm.DB, fence *service.DatabaseProvisioningFence) {
				t.Helper()
				if err := db.Model(&model.DatabaseProvisioningOperation{}).
					Where("id = ?", fence.ID).
					Update("lease_expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
					t.Fatalf("expire activation lease: %v", err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, db := newDatabaseProvisioningStoreTest(t)
			migrateProvisioningOperationsForStoreTest(t, db)
			operation := seedStoreProvisioningOperation(t, repository, db, time.Now().UTC().Add(time.Minute))
			setStoreProvisioningOperationState(
				t, db, operation.ID, service.ProvisioningStageActivationPending,
				service.ProvisioningCleanupNone, time.Now().UTC(),
			)
			current, err := repository.DatabaseProvisioningOperation(context.Background(), operation.ID)
			if err != nil {
				t.Fatalf("load activation operation: %v", err)
			}
			if err := db.Create(&model.DatabaseAccount{
				InstanceID: operation.InstanceID, UniqueName: "existing-" + operation.ID,
				Username: operation.Username, Password: model.NewEncryptedField("generated-secret"),
				Status: "active", Managed: true, UpstreamHost: operation.Host,
				ResourceSeq: 99, ResourceID: "D099", ProvisioningOperationID: &operation.ID,
			}).Error; err != nil {
				t.Fatalf("create existing managed account: %v", err)
			}
			fence := current.Fence()
			if test.mutate != nil {
				test.mutate(t, db, &fence)
			}

			account, activated, err := repository.ActivateDatabaseProvisioningOperation(context.Background(), fence)
			if err != nil {
				t.Fatalf("activate existing account: %v", err)
			}
			if activated != test.wantOK {
				t.Fatalf("activate existing account = %#v, %t, want %t", account, activated, test.wantOK)
			}
			if !test.wantOK {
				if account.ID != "" {
					t.Fatalf("stale fence exposed existing account: %#v", account)
				}
				return
			}
			if account.Username != operation.Username || account.Status != "active" {
				t.Fatalf("current fence did not return managed account: %#v", account)
			}
			var completed model.DatabaseProvisioningOperation
			if err := db.First(&completed, "id = ?", operation.ID).Error; err != nil {
				t.Fatalf("load completed activation: %v", err)
			}
			if completed.Stage != provisioningStageActiveManaged || completed.LeaseToken != "" {
				t.Fatalf("existing account activation did not complete operation: %#v", completed)
			}
		})
	}
}

func TestDatabaseProvisioningDeleteUsesMetadataClock(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	migrateProvisioningOperationsForStoreTest(t, db)
	operation := seedStoreProvisioningOperation(t, repository, db, time.Now().Add(time.Minute))
	deleted, err := repository.DeleteDatabaseProvisioningOperation(context.Background(), operation.Fence())
	if err != nil || !deleted {
		t.Fatalf("delete operation = %t, %v", deleted, err)
	}
	if _, err := repository.DatabaseProvisioningOperation(context.Background(), operation.ID); err == nil {
		t.Fatal("deleted operation remains visible")
	}
}

func TestPendingProvisioningOperationIsInvisibleAndBlocksInstanceDelete(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	migrateProvisioningOperationsForStoreTest(t, db)
	now := time.Now().UTC()
	operation := seedStoreProvisioningOperation(t, repository, db, now.Add(time.Minute))

	accounts, err := repository.DatabaseAccounts()
	if err != nil {
		t.Fatalf("list database accounts: %v", err)
	}
	for _, account := range accounts {
		if account.ID == operation.ID || account.Username == operation.Username {
			t.Fatalf("pending operation appeared as ordinary account: %#v", account)
		}
	}
	if _, err := repository.DatabaseAccount(operation.ID); !errors.Is(err, ErrDBAccountNotFound) {
		t.Fatalf("pending operation detail error = %v, want not found", err)
	}
	if _, err := repository.UpdateDatabaseAccount(
		operation.ID, "changed", "", "", "", nil, "active",
	); !errors.Is(err, ErrDBAccountNotFound) {
		t.Fatalf("pending operation update error = %v, want not found", err)
	}
	if err := repository.DeleteDatabaseAccount(operation.ID); !errors.Is(err, ErrDBAccountNotFound) {
		t.Fatalf("pending operation delete error = %v, want not found", err)
	}
	if err := repository.DeleteDatabaseInstance(operation.InstanceID); err == nil {
		t.Fatal("instance with pending provisioning operation was deleted")
	}
	if _, err := repository.DatabaseProvisioningOperation(
		context.Background(), operation.ID,
	); err != nil {
		t.Fatalf("instance deletion erased cleanup clue: %v", err)
	}
}

func TestExecutableOperationQueryIsNotHiddenByFreshReservations(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	migrateProvisioningOperationsForStoreTest(t, db)
	now := time.Now().UTC()
	for index := 0; index < 101; index++ {
		seedStoreProvisioningOperationWithToken(
			t, repository, db, now.Add(time.Minute), fmt.Sprintf("fresh%015d", index),
		)
	}
	executable := seedStoreProvisioningOperationWithToken(
		t, repository, db, now.Add(-time.Minute), "cleanup0000000000001",
	)
	setStoreProvisioningOperationState(
		t, db, executable.ID, service.ProvisioningStageCleanupRequired,
		service.ProvisioningCleanupRequired, now.Add(-time.Minute),
	)

	operations, err := repository.ListExecutableDatabaseProvisioningOperations(
		context.Background(), 30*time.Second, 100,
	)
	if err != nil {
		t.Fatalf("list executable operations: %v", err)
	}
	if len(operations) != 1 || operations[0].ID != executable.ID {
		t.Fatalf("executable operation was hidden by reservations: %#v", operations)
	}
}

func migrateProvisioningOperationsForStoreTest(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(
		&model.DatabaseAccount{},
		&model.DatabaseProvisioningOperation{},
	); err != nil {
		t.Fatalf("migrate provisioning operations: %v", err)
	}
}

func seedStoreProvisioningOperation(
	t *testing.T,
	repository *DBStore,
	db *gorm.DB,
	leaseExpiresAt time.Time,
) service.DatabaseProvisioningOperation {
	t.Helper()
	return seedStoreProvisioningOperationWithToken(
		t, repository, db, leaseExpiresAt, "operation00000000001",
	)
}

func seedStoreProvisioningOperationWithToken(
	t *testing.T,
	repository *DBStore,
	db *gorm.DB,
	leaseExpiresAt time.Time,
	token string,
) service.DatabaseProvisioningOperation {
	t.Helper()
	var instance model.DatabaseInstance
	if err := db.First(&instance).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		instance = model.DatabaseInstance{
			Name: "orders", Protocol: "mysql", Address: "127.0.0.1",
			Port: 3306, Status: "active",
		}
		if err := db.Create(&instance).Error; err != nil {
			t.Fatalf("create instance: %v", err)
		}
	} else if err != nil {
		t.Fatalf("load instance: %v", err)
	}
	var admin model.DatabaseAccount
	if err := db.First(&admin).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		admin = model.DatabaseAccount{
			InstanceID: instance.ID, UniqueName: "admin", Username: "root",
			Password: model.NewEncryptedField("admin-secret"), Status: "active",
			ResourceID: "D001",
		}
		if err := db.Create(&admin).Error; err != nil {
			t.Fatalf("create administrator: %v", err)
		}
	} else if err != nil {
		t.Fatalf("load administrator: %v", err)
	}
	operation, _, err := repository.CreateDatabaseProvisioningOperation(
		context.Background(),
		service.DatabaseProvisioningOperationInput{
			ID: "jmo_" + token, InstanceID: instance.ID, AdminAccountID: admin.ID,
			Username: "jm_" + token, Password: "generated-secret",
			Host:       "10.0.0.8",
			GrantsJSON: `[{"database":"orders","privilege":"read"}]`,
			Lease: service.DatabaseProvisioningLease{
				Owner: "request", Token: "request-lease", Duration: time.Minute,
			},
		},
	)
	if err != nil {
		t.Fatalf("create provisioning operation: %v", err)
	}
	if err := db.Model(&model.DatabaseProvisioningOperation{}).
		Where("id = ?", operation.ID).
		Update("lease_expires_at", leaseExpiresAt.UTC()).Error; err != nil {
		t.Fatalf("set provisioning operation lease: %v", err)
	}
	operation.LeaseExpiresAt = timePointer(leaseExpiresAt.UTC())
	return operation
}

func setStoreProvisioningOperationState(
	t *testing.T,
	db *gorm.DB,
	id, stage, cleanup string,
	lastAttempt time.Time,
) {
	t.Helper()
	if err := db.Model(&model.DatabaseProvisioningOperation{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"stage": stage, "cleanup_status": cleanup,
			"last_attempt_at": lastAttempt,
		}).Error; err != nil {
		t.Fatalf("set provisioning operation state: %v", err)
	}
}
