package store

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/storage"
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

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
