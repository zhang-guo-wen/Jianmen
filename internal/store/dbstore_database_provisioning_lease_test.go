package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func TestDatabaseProvisioningOwnershipWritesUseStatementTimeAfterPause(t *testing.T) {
	t.Run("renew", func(t *testing.T) {
		repository, db := newPausedProvisioningStoreOperation(t)
		operation := loadPausedProvisioningOperation(t, repository)
		release := pauseProvisioningStatement(t, db, "update")
		result := make(chan struct {
			ok  bool
			err error
		}, 1)
		go func() {
			_, _, ok, err := repository.RenewDatabaseProvisioningOperation(
				context.Background(), operation.Fence(),
				service.DatabaseProvisioningLease{
					Owner: operation.LeaseOwner, Token: operation.LeaseToken,
					Duration: time.Minute,
				},
			)
			result <- struct {
				ok  bool
				err error
			}{ok: ok, err: err}
		}()
		release()
		got := <-result
		if got.err != nil || got.ok {
			t.Fatalf("renewed after paused lease expired = %t, %v", got.ok, got.err)
		}
	})

	t.Run("transition", func(t *testing.T) {
		repository, db := newPausedProvisioningStoreOperation(t)
		operation := loadPausedProvisioningOperation(t, repository)
		release := pauseProvisioningStatement(t, db, "update")
		result := make(chan struct {
			ok  bool
			err error
		}, 1)
		go func() {
			_, ok, err := repository.TransitionDatabaseProvisioningOperation(
				context.Background(), operation.Fence(),
				service.DatabaseProvisioningTransition{
					Stage:         service.ProvisioningStageNotCreated,
					CleanupStatus: service.ProvisioningCleanupNone,
				},
			)
			result <- struct {
				ok  bool
				err error
			}{ok: ok, err: err}
		}()
		release()
		got := <-result
		if got.err != nil || got.ok {
			t.Fatalf("transitioned after paused lease expired = %t, %v", got.ok, got.err)
		}
	})

	t.Run("delete", func(t *testing.T) {
		repository, db := newPausedProvisioningStoreOperation(t)
		operation := loadPausedProvisioningOperation(t, repository)
		release := pauseProvisioningStatement(t, db, "update")
		result := make(chan struct {
			ok  bool
			err error
		}, 1)
		go func() {
			ok, err := repository.DeleteDatabaseProvisioningOperation(
				context.Background(), operation.Fence(),
			)
			result <- struct {
				ok  bool
				err error
			}{ok: ok, err: err}
		}()
		release()
		got := <-result
		if got.err != nil || got.ok {
			t.Fatalf("deleted after paused lease expired = %t, %v", got.ok, got.err)
		}
	})

	t.Run("activate", func(t *testing.T) {
		repository, db := newPausedProvisioningStoreOperation(t)
		if err := db.Model(&model.DatabaseProvisioningOperation{}).
			Where("id = ?", "jmo_operation00000000001").
			Updates(map[string]any{
				"stage":          service.ProvisioningStageActivationPending,
				"cleanup_status": service.ProvisioningCleanupNone,
			}).Error; err != nil {
			t.Fatalf("prepare activation: %v", err)
		}
		operation := loadPausedProvisioningOperation(t, repository)
		release := pauseProvisioningStatement(t, db, "query")
		result := make(chan struct {
			ok  bool
			err error
		}, 1)
		go func() {
			_, ok, err := repository.ActivateDatabaseProvisioningOperation(
				context.Background(), operation.Fence(),
			)
			result <- struct {
				ok  bool
				err error
			}{ok: ok, err: err}
		}()
		release()
		got := <-result
		if got.err != nil || got.ok {
			t.Fatalf("activated after paused lease expired = %t, %v", got.ok, got.err)
		}
		var accounts int64
		if err := db.Model(&model.DatabaseAccount{}).
			Where("provisioning_operation_id = ?", operation.ID).
			Count(&accounts).Error; err != nil {
			t.Fatalf("count rolled back activation accounts: %v", err)
		}
		if accounts != 0 {
			t.Fatalf("expired activation committed an account: %d", accounts)
		}
	})
}

func newPausedProvisioningStoreOperation(t *testing.T) (*DBStore, *gorm.DB) {
	t.Helper()
	repository, db := newDatabaseProvisioningStoreTest(t)
	migrateProvisioningOperationsForStoreTest(t, db)
	operation := seedStoreProvisioningOperation(
		t, repository, db, time.Now().UTC().Add(time.Minute),
	)
	if err := db.Exec(
		"UPDATE database_provisioning_operations "+
			"SET lease_expires_at = strftime('%Y-%m-%d %H:%M:%f', 'now', '+0.025 seconds') "+
			"WHERE id = ?",
		operation.ID,
	).Error; err != nil {
		t.Fatalf("set short statement-time lease: %v", err)
	}
	return repository, db
}

func loadPausedProvisioningOperation(
	t *testing.T,
	repository *DBStore,
) service.DatabaseProvisioningOperation {
	t.Helper()
	operation, err := repository.DatabaseProvisioningOperation(
		context.Background(), "jmo_operation00000000001",
	)
	if err != nil {
		t.Fatalf("load paused operation: %v", err)
	}
	return operation
}

func pauseProvisioningStatement(t *testing.T, db *gorm.DB, processor string) func() {
	t.Helper()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	name := "pause-provisioning-statement-" + processor
	pause := func(tx *gorm.DB) {
		if tx.Statement.Table != "database_provisioning_operations" {
			return
		}
		once.Do(func() {
			close(entered)
			<-release
		})
	}
	callbacks := db.Callback()
	switch processor {
	case "update":
		callbacks.Update().Before("gorm:update").Register(name, pause)
		t.Cleanup(func() { _ = callbacks.Update().Remove(name) })
	case "delete":
		callbacks.Delete().Before("gorm:delete").Register(name, pause)
		t.Cleanup(func() { _ = callbacks.Delete().Remove(name) })
	case "query":
		callbacks.Query().Before("gorm:query").Register(name, pause)
		t.Cleanup(func() { _ = callbacks.Query().Remove(name) })
	default:
		t.Fatalf("unsupported callback processor %q", processor)
	}
	return func() {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("ownership statement did not reach controlled pause")
		}
		time.Sleep(75 * time.Millisecond)
		close(release)
	}
}

func TestDatabaseProvisioningRenewRejectsStaleOwnerTokenAndRevision(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	migrateProvisioningOperationsForStoreTest(t, db)
	operation := seedStoreProvisioningOperation(t, repository, db, time.Now().Add(-time.Minute))
	candidate, err := repository.DatabaseProvisioningOperation(context.Background(), operation.ID)
	if err != nil {
		t.Fatalf("load operation: %v", err)
	}
	claimed, _, ok, err := repository.ClaimDatabaseProvisioningOperation(
		context.Background(),
		candidate.Fence(),
		service.DatabaseProvisioningLease{
			Owner: "worker-a", Token: "lease-a", Duration: time.Minute,
		},
	)
	if err != nil || !ok {
		t.Fatalf("claim operation = %t, %v", ok, err)
	}
	staleRevision := claimed.Fence()
	staleRevision.Revision--
	if _, _, ok, err := repository.RenewDatabaseProvisioningOperation(
		context.Background(), staleRevision,
		service.DatabaseProvisioningLease{Owner: "worker-a", Token: "lease-a", Duration: time.Minute},
	); err != nil || ok {
		t.Fatalf("stale revision renewed lease = %t, %v", ok, err)
	}
	staleToken := claimed.Fence()
	staleToken.LeaseToken = "other-token"
	if _, _, ok, err := repository.RenewDatabaseProvisioningOperation(
		context.Background(), staleToken,
		service.DatabaseProvisioningLease{Owner: "worker-a", Token: "other-token", Duration: time.Minute},
	); err != nil || ok {
		t.Fatalf("stale token renewed lease = %t, %v", ok, err)
	}
}

func TestDatabaseProvisioningLeaseOperationsDoNotAcceptCallerClock(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	migrateProvisioningOperationsForStoreTest(t, db)
	operation := seedStoreProvisioningOperation(t, repository, db, time.Now().Add(-time.Minute))
	candidate, err := repository.DatabaseProvisioningOperation(context.Background(), operation.ID)
	if err != nil {
		t.Fatalf("load operation: %v", err)
	}
	claimed, window, ok, err := repository.ClaimDatabaseProvisioningOperation(
		context.Background(), candidate.Fence(),
		service.DatabaseProvisioningLease{Owner: "worker-a", Token: "lease-a", Duration: time.Minute},
	)
	if err != nil || !ok {
		t.Fatalf("claim operation = %t, %v", ok, err)
	}
	if window.Remaining <= 0 || window.Remaining > time.Minute {
		t.Fatalf("claim returned invalid DB lease window: %#v", window)
	}
	if _, ok, err := repository.TransitionDatabaseProvisioningOperation(
		context.Background(), claimed.Fence(),
		service.DatabaseProvisioningTransition{
			Stage:         service.ProvisioningStageNotCreated,
			CleanupStatus: service.ProvisioningCleanupNone,
		},
	); err != nil || !ok {
		t.Fatalf("transition using DB time = %t, %v", ok, err)
	}
}

func TestDatabaseProvisioningInitialLeaseAndClaimUseMetadataClockDespiteProcessSkew(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	migrateProvisioningOperationsForStoreTest(t, db)
	instance, admin := seedProvisioningStoreAdministrator(t, db)
	before, err := databaseNow(context.Background(), db)
	if err != nil {
		t.Fatalf("read metadata clock before create: %v", err)
	}
	const leaseDuration = 2 * time.Minute
	operation, _, err := repository.CreateDatabaseProvisioningOperation(
		context.Background(),
		service.DatabaseProvisioningOperationInput{
			ID: "jmo_clockskew00000000000", InstanceID: instance.ID, AdminAccountID: admin.ID,
			Username: "jm_clockskew00000000000", Password: "generated-secret", Host: "10.0.0.8",
			GrantsJSON: `[{"database":"orders","privilege":"read"}]`,
			Lease: service.DatabaseProvisioningLease{
				Owner: "worker-a", Token: "lease-a", Duration: leaseDuration,
			},
			AdministratorUsername: admin.Username, AdministratorPassword: admin.Password.GetPlaintext(),
			InstanceProof: databaseProvisioningInstanceProof(instance),
		},
	)
	if err != nil {
		t.Fatalf("create operation: %v", err)
	}
	after, err := databaseNow(context.Background(), db)
	if err != nil {
		t.Fatalf("read metadata clock after create: %v", err)
	}
	if operation.LeaseExpiresAt == nil ||
		operation.LeaseExpiresAt.Before(before.Add(leaseDuration-time.Second)) ||
		operation.LeaseExpiresAt.After(after.Add(leaseDuration+time.Second)) {
		t.Fatalf("initial lease did not use metadata clock: %#v", operation.LeaseExpiresAt)
	}
	// The API has no caller-clock field: a worker six hours fast or slow still
	// obtains a lease window derived from the database samples above.
	if err := db.Model(&model.DatabaseProvisioningOperation{}).
		Where("id = ?", operation.ID).
		Update("lease_expires_at", before.Add(-time.Minute)).Error; err != nil {
		t.Fatalf("expire operation for claim: %v", err)
	}
	claimed, window, ok, err := repository.ClaimDatabaseProvisioningOperation(
		context.Background(), operation.Fence(),
		service.DatabaseProvisioningLease{Owner: "worker-b", Token: "lease-b", Duration: leaseDuration},
	)
	if err != nil || !ok || window.Remaining <= 0 || window.Remaining > leaseDuration {
		t.Fatalf("claim with metadata clock = %#v, %t, %v", window, ok, err)
	}
	if claimed.LeaseExpiresAt == nil || claimed.LeaseExpiresAt.Before(before.Add(time.Second)) {
		t.Fatalf("claim retained a caller-clock expiry: %#v", claimed.LeaseExpiresAt)
	}
}

func TestDatabaseProvisioningTransitionAttemptUsesMetadataClock(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	migrateProvisioningOperationsForStoreTest(t, db)
	operation := seedStoreProvisioningOperation(t, repository, db, time.Now().Add(time.Minute))
	before, err := databaseNow(context.Background(), db)
	if err != nil {
		t.Fatalf("read metadata clock before transition: %v", err)
	}
	updated, ok, err := repository.TransitionDatabaseProvisioningOperation(
		context.Background(), operation.Fence(),
		service.DatabaseProvisioningTransition{
			Stage: service.ProvisioningStageNotCreated, CleanupStatus: service.ProvisioningCleanupNone,
			IncrementAttempt: true,
		},
	)
	if err != nil || !ok {
		t.Fatalf("transition operation = %t, %v", ok, err)
	}
	after, err := databaseNow(context.Background(), db)
	if err != nil {
		t.Fatalf("read metadata clock after transition: %v", err)
	}
	if updated.LastAttemptAt == nil || updated.LastAttemptAt.Before(before.Add(-time.Second)) ||
		updated.LastAttemptAt.After(after.Add(time.Second)) {
		t.Fatalf("attempt timestamp did not use metadata clock: %#v", updated.LastAttemptAt)
	}
}
