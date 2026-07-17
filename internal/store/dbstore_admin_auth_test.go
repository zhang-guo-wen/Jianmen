package store

import (
	"context"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestDBStoreAdminAuthLifecycle(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := db.Create(&model.User{ID: "u1", Username: "alice", TokenHash: "token-hash", Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	store := NewDBStore(db)

	user, found, err := store.FindActiveUserByTokenHash(context.Background(), "token-hash")
	if err != nil {
		t.Fatalf("find active user: %v", err)
	}
	if !found || user.ID != "u1" {
		t.Fatalf("user = %#v found=%t", user, found)
	}
	if err := store.DisableUser(context.Background(), user.ID); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	_, found, err = store.FindActiveUserByTokenHash(context.Background(), "token-hash")
	if err != nil {
		t.Fatalf("find disabled user: %v", err)
	}
	if found {
		t.Fatal("disabled user must not authenticate")
	}
}
