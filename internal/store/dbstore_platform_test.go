package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"jianmen/internal/crypto"
	"jianmen/internal/model"
	"jianmen/internal/storage"

	"gorm.io/gorm"
)

func TestDBStorePlatformAccountsHonorsCancelledContext(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	store := NewDBStore(db)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := store.PlatformAccounts(ctx, PlatformAccountListParams{}); err == nil || !isContextError(err) {
		t.Fatalf("PlatformAccounts(context canceled) error = %v, want context cancellation", err)
	}
}

func TestDBStoreGetPlatformAccountPasswordHonorsCancelledContext(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	store := NewDBStore(db)

	owner := model.User{ID: "owner-1", Username: "owner", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}

	account := model.PlatformAccount{
		ID: "platform-1", Name: "gitlab", PlatformName: "GitLab", Username: "alice", OwnerID: owner.ID, Status: "active",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("seed platform account: %v", err)
	}
	var seeded model.PlatformAccount
	if err := db.First(&seeded, "id = ?", account.ID).Error; err != nil {
		t.Fatalf("load seeded platform account: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.GetPlatformAccountPassword(ctx, seeded.ID); err == nil {
		t.Fatalf("GetPlatformAccountPassword(context canceled) should fail")
	}
	var refreshed model.PlatformAccount
	if err := db.First(&refreshed, "id = ?", seeded.ID).Error; err != nil {
		t.Fatalf("reload platform account: %v", err)
	}
	if refreshed.Name != account.Name {
		t.Fatalf("platform account changed name to %q, want %q", refreshed.Name, account.Name)
	}
}

func TestDBStoreAddPlatformAccountSkipsWriteOnCancelledContext(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	store := NewDBStore(db)

	owner := model.User{ID: "owner-1", Username: "owner", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.AddPlatformAccount(ctx, model.PlatformAccount{
		Name: "gitlab", PlatformName: "GitLab", Username: "alice", OwnerID: owner.ID, Status: "active",
	}); err == nil || !isContextError(err) {
		t.Fatalf("AddPlatformAccount(context canceled) error = %v, want context cancellation", err)
	}

	var count int64
	if err := db.Model(&model.PlatformAccount{}).Count(&count).Error; err != nil {
		t.Fatalf("count platform accounts: %v", err)
	}
	if count != 0 {
		t.Fatalf("platform account count = %d, want 0", count)
	}
	var resourceCount int64
	if err := db.Model(&model.Resource{}).Where("type = ?", model.ResourceTypePlatformAccount).Count(&resourceCount).Error; err != nil {
		t.Fatalf("count platform account resources: %v", err)
	}
	if resourceCount != 0 {
		t.Fatalf("platform account resource count = %d, want 0", resourceCount)
	}
}

func TestDBStoreAddPlatformAccountRollsBackWhenContextCancelledDuringReload(t *testing.T) {
	store, db := newPlatformAtomicTestStore(t)
	owner := model.User{ID: "owner-add", Username: "owner-add", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancelPlatformAccountContextBeforeReload(t, db, "create", cancel)
	if _, err := store.AddPlatformAccount(ctx, model.PlatformAccount{
		ID: "platform-add", Name: "before", PlatformName: "GitLab", Username: "alice", OwnerID: owner.ID, Status: "active",
	}); err == nil || !isContextError(err) {
		t.Fatalf("AddPlatformAccount(reload canceled) error = %v, want context cancellation", err)
	}

	var accountCount int64
	if err := db.Model(&model.PlatformAccount{}).Where("id = ?", "platform-add").Count(&accountCount).Error; err != nil {
		t.Fatalf("count platform accounts: %v", err)
	}
	if accountCount != 0 {
		t.Fatalf("platform account count = %d, want transaction rollback", accountCount)
	}
	var resourceCount int64
	if err := db.Model(&model.Resource{}).
		Where("type = ? AND resource_id = ?", model.ResourceTypePlatformAccount, "platform-add").
		Count(&resourceCount).Error; err != nil {
		t.Fatalf("count platform account resources: %v", err)
	}
	if resourceCount != 0 {
		t.Fatalf("platform account resource count = %d, want transaction rollback", resourceCount)
	}
}

func TestDBStoreUpdatePlatformAccountRollsBackWhenContextCancelledDuringReload(t *testing.T) {
	store, db := newPlatformAtomicTestStore(t)
	owner := model.User{ID: "owner-update", Username: "owner-update", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	account := model.PlatformAccount{
		ID: "platform-update", Name: "before", PlatformName: "GitLab", Username: "alice", OwnerID: owner.ID, Status: "active",
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create platform account: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancelPlatformAccountContextBeforeReload(t, db, "update", cancel)
	if _, err := store.UpdatePlatformAccount(ctx, account.ID, model.PlatformAccount{Name: "after"}); err == nil || !isContextError(err) {
		t.Fatalf("UpdatePlatformAccount(reload canceled) error = %v, want context cancellation", err)
	}

	var refreshed model.PlatformAccount
	if err := db.First(&refreshed, "id = ?", account.ID).Error; err != nil {
		t.Fatalf("reload platform account: %v", err)
	}
	if refreshed.Name != account.Name {
		t.Fatalf("platform account name = %q, want rolled back value %q", refreshed.Name, account.Name)
	}
	var resourceCount int64
	if err := db.Model(&model.Resource{}).
		Where("type = ? AND resource_id = ?", model.ResourceTypePlatformAccount, account.ID).
		Count(&resourceCount).Error; err != nil {
		t.Fatalf("count platform account resources: %v", err)
	}
	if resourceCount != 0 {
		t.Fatalf("platform account resource count = %d, want transaction rollback", resourceCount)
	}
}

func TestCreateManagedPlatformAccountRollsBackWhenCreatorGrantFails(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	repository := NewDBStore(db)
	if err := db.Create(&model.User{ID: "creator", Username: "creator", Status: "active"}).Error; err != nil {
		t.Fatalf("create creator: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_platform_creator_grant
		BEFORE INSERT ON resource_grants
		BEGIN SELECT RAISE(ABORT, 'injected resource grant failure'); END;`).Error; err != nil {
		t.Fatalf("create grant failure trigger: %v", err)
	}
	if _, err := repository.CreateManagedPlatformAccount(context.Background(), model.PlatformAccount{
		Name: "Git", PlatformName: "Git", Username: "alice", OwnerID: "creator", Status: "active",
	}, "creator"); err == nil {
		t.Fatal("CreateManagedPlatformAccount() succeeded despite creator grant failure")
	}
	var accountCount, resourceCount int64
	if err := db.Model(&model.PlatformAccount{}).Count(&accountCount).Error; err != nil {
		t.Fatalf("count platform accounts: %v", err)
	}
	if err := db.Model(&model.Resource{}).Where("type = ?", model.ResourceTypePlatformAccount).Count(&resourceCount).Error; err != nil {
		t.Fatalf("count platform resources: %v", err)
	}
	if accountCount != 0 || resourceCount != 0 {
		t.Fatalf("orphan platform rows after grant failure: accounts=%d resources=%d", accountCount, resourceCount)
	}
}

func TestCreateManagedPlatformAccountSkipsWriteOnCancelledContext(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	repository := NewDBStore(db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.CreateManagedPlatformAccount(ctx, model.PlatformAccount{
		Name: "Git", PlatformName: "Git", Username: "alice", OwnerID: "creator", Status: "active",
	}, "creator"); err == nil {
		t.Fatal("CreateManagedPlatformAccount() succeeded with cancelled context")
	}
	var accountCount, resourceCount, grantCount int64
	if err := db.Model(&model.PlatformAccount{}).Count(&accountCount).Error; err != nil {
		t.Fatalf("count platform accounts: %v", err)
	}
	if err := db.Model(&model.Resource{}).Where("type = ?", model.ResourceTypePlatformAccount).Count(&resourceCount).Error; err != nil {
		t.Fatalf("count platform resources: %v", err)
	}
	if err := db.Model(&model.ResourceGrant{}).Where("resource_type = ?", model.ResourceTypePlatformAccount).Count(&grantCount).Error; err != nil {
		t.Fatalf("count platform grants: %v", err)
	}
	if accountCount != 0 || resourceCount != 0 || grantCount != 0 {
		t.Fatalf("cancelled creation left rows: accounts=%d resources=%d grants=%d", accountCount, resourceCount, grantCount)
	}
}

func TestUpdatePlatformAccountPreservesConcurrentLifecycleAndPassword(t *testing.T) {
	store, db := newPlatformAtomicTestStore(t)
	owner := model.User{ID: "owner-concurrent", Username: "owner-concurrent", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	oldExpiry := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	created, err := store.AddPlatformAccount(context.Background(), model.PlatformAccount{
		ID: "platform-concurrent", Name: "before", PlatformName: "GitLab", Username: "alice",
		Password: model.NewEncryptedField("old-secret"), OwnerID: owner.ID, Status: "active", ExpiresAt: &oldExpiry,
	})
	if err != nil {
		t.Fatalf("create platform account: %v", err)
	}

	newExpiry := oldExpiry.Add(24 * time.Hour)
	callbackName := "test:platform_concurrent_security_update"
	callbackCalled := false
	if err := db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if callbackCalled || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "platform_accounts" {
			return
		}
		callbackCalled = true
		if err := tx.Exec(
			"UPDATE platform_accounts SET status = ?, password = ?, expires_at = ? WHERE id = ?",
			"disabled", model.NewEncryptedField("new-secret"), newExpiry, created.ID,
		).Error; err != nil {
			tx.AddError(err)
		}
	}); err != nil {
		t.Fatalf("register concurrent update callback: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Update().Remove(callbackName); err != nil {
			t.Errorf("remove concurrent update callback: %v", err)
		}
	})

	if _, err := store.UpdateManagedPlatformAccount(
		context.Background(),
		created.ID,
		model.PlatformAccount{Name: "renamed"},
	); err != nil {
		t.Fatalf("update platform account: %v", err)
	}
	if !callbackCalled {
		t.Fatal("concurrent security update callback was not called")
	}
	var refreshed model.PlatformAccount
	if err := db.First(&refreshed, "id = ?", created.ID).Error; err != nil {
		t.Fatalf("reload platform account: %v", err)
	}
	if refreshed.Name != "renamed" {
		t.Fatalf("name = %q, want renamed", refreshed.Name)
	}
	if refreshed.Status != "disabled" {
		t.Fatalf("status = %q, want concurrent disabled value", refreshed.Status)
	}
	if refreshed.Password.GetPlaintext() != "new-secret" {
		t.Fatalf("password = %q, want concurrent replacement preserved", refreshed.Password.GetPlaintext())
	}
	if refreshed.ExpiresAt == nil || !refreshed.ExpiresAt.Equal(newExpiry) {
		t.Fatalf("expires_at = %v, want %v", refreshed.ExpiresAt, newExpiry)
	}
}

func TestUpdatePlatformAccountDoesNotReviveDeletedAccount(t *testing.T) {
	store, db := newPlatformAtomicTestStore(t)
	owner := model.User{ID: "owner-delete-race", Username: "owner-delete-race", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	created, err := store.AddPlatformAccount(context.Background(), model.PlatformAccount{
		ID: "platform-delete-race", Name: "before", PlatformName: "GitLab", Username: "alice",
		OwnerID: owner.ID, Status: "active",
	})
	if err != nil {
		t.Fatalf("create platform account: %v", err)
	}

	callbackName := "test:platform_delete_before_update"
	callbackCalled := false
	if err := db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if callbackCalled || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "platform_accounts" {
			return
		}
		callbackCalled = true
		if err := tx.Exec(
			"DELETE FROM resources WHERE type = ? AND resource_id = ?",
			model.ResourceTypePlatformAccount, created.ID,
		).Error; err != nil {
			tx.AddError(err)
			return
		}
		if err := tx.Exec("DELETE FROM platform_accounts WHERE id = ?", created.ID).Error; err != nil {
			tx.AddError(err)
		}
	}); err != nil {
		t.Fatalf("register concurrent delete callback: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Update().Remove(callbackName); err != nil {
			t.Errorf("remove concurrent delete callback: %v", err)
		}
	})

	_, err = store.UpdateManagedPlatformAccount(
		context.Background(),
		created.ID,
		model.PlatformAccount{Name: "must-not-return"},
	)
	if !errors.Is(err, ErrPlatformAccountNotFound) {
		t.Fatalf("UpdateManagedPlatformAccount() error = %v, want %v", err, ErrPlatformAccountNotFound)
	}
	if !callbackCalled {
		t.Fatal("concurrent delete callback was not called")
	}
	var accountCount, resourceCount int64
	if err := db.Model(&model.PlatformAccount{}).Where("id = ?", created.ID).Count(&accountCount).Error; err != nil {
		t.Fatalf("count platform accounts: %v", err)
	}
	if err := db.Model(&model.Resource{}).
		Where("type = ? AND resource_id = ?", model.ResourceTypePlatformAccount, created.ID).
		Count(&resourceCount).Error; err != nil {
		t.Fatalf("count platform resources: %v", err)
	}
	if accountCount != 0 || resourceCount != 0 {
		t.Fatalf("update revived deleted state: accounts=%d resources=%d", accountCount, resourceCount)
	}
}

func newPlatformAtomicTestStore(t *testing.T) (*DBStore, *gorm.DB) {
	t.Helper()
	if _, err := crypto.Init(t.TempDir()); err != nil {
		t.Fatalf("initialize crypto: %v", err)
	}
	dsn := filepath.ToSlash(filepath.Join(t.TempDir(), "platform-atomic.db"))
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: dsn})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sql database: %v", err)
		}
	})
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	return NewDBStore(db), db
}

func cancelPlatformAccountContextBeforeReload(
	t *testing.T,
	db *gorm.DB,
	mutation string,
	cancel context.CancelFunc,
) {
	t.Helper()
	callbacks := db.Callback()
	armed := false
	armName := "test:arm_platform_account_" + mutation + "_reload_cancel"
	arm := func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "platform_accounts" {
			armed = true
		}
	}
	switch mutation {
	case "create":
		if err := callbacks.Create().After("gorm:create").Register(armName, arm); err != nil {
			t.Fatalf("register platform account create callback: %v", err)
		}
		t.Cleanup(func() {
			if err := callbacks.Create().Remove(armName); err != nil {
				t.Errorf("remove platform account create callback: %v", err)
			}
		})
	case "update":
		if err := callbacks.Update().After("gorm:update").Register(armName, arm); err != nil {
			t.Fatalf("register platform account update callback: %v", err)
		}
		t.Cleanup(func() {
			if err := callbacks.Update().Remove(armName); err != nil {
				t.Errorf("remove platform account update callback: %v", err)
			}
		})
	default:
		t.Fatalf("unsupported platform account mutation %q", mutation)
	}

	queryName := "test:cancel_platform_account_" + mutation + "_reload"
	if err := callbacks.Query().Before("gorm:query").Register(queryName, func(tx *gorm.DB) {
		if !armed || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "platform_accounts" {
			return
		}
		armed = false
		cancel()
	}); err != nil {
		t.Fatalf("register platform account reload callback: %v", err)
	}
	t.Cleanup(func() {
		if err := callbacks.Query().Remove(queryName); err != nil {
			t.Errorf("remove platform account reload callback: %v", err)
		}
	})
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
