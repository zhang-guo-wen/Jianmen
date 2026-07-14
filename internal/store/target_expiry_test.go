package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestTargetConfigCarriesHostAccountExpiry(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	now := time.Now().UTC()
	expiresAt := now.Add(time.Hour).Truncate(time.Second)
	host := model.Host{ID: "expiry-host", Name: "expiry-host", Address: "127.0.0.1", Port: 22, Status: "active"}
	account := model.HostAccount{ID: "expiry-account", HostID: host.ID, Username: "root", Status: "active", ResourceID: "H101", ExpiresAt: &expiresAt}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	target, err := NewDBStore(db).TargetConfig(account.ID)
	if err != nil {
		t.Fatalf("target config: %v", err)
	}
	if target.ExpiresAt != expiresAt.Format(time.RFC3339Nano) {
		t.Fatalf("expires_at = %q, want %q", target.ExpiresAt, expiresAt.Format(time.RFC3339Nano))
	}
	if target.Expired(now) {
		t.Fatal("target was expired before its expiry time")
	}
	if !target.Expired(expiresAt) {
		t.Fatal("target was not expired at its expiry time")
	}
}

func TestDefaultTargetRejectsExpiredHostAccounts(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	future := time.Now().UTC().Add(time.Hour)
	host := model.Host{ID: "default-expiry-host", Name: "default-expiry-host", Address: "127.0.0.1", Port: 22, Status: "active"}
	accounts := []model.HostAccount{
		{ID: "expired-account", HostID: host.ID, Username: "expired", Status: "active", ResourceID: "H102", ExpiresAt: &past},
		{ID: "valid-account", HostID: host.ID, Username: "valid", Status: "active", ResourceID: "H103", ExpiresAt: &future},
	}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := db.Create(&accounts).Error; err != nil {
		t.Fatalf("create accounts: %v", err)
	}
	st := NewDBStore(db)

	_, err = st.DefaultTarget(context.Background(), model.User{RequestedTargetID: "expired-account"})
	if !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("expired explicit target error = %v, want ErrTargetUnavailable", err)
	}
	target, err := st.DefaultTarget(context.Background(), model.User{})
	if err != nil {
		t.Fatalf("default target: %v", err)
	}
	if target.ID != "valid-account" {
		t.Fatalf("default target = %q, want valid-account", target.ID)
	}
}
