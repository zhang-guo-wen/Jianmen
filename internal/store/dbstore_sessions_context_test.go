package store

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestUserSessionCreationQueriesHonorCanceledContext(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	repository := NewDBStore(db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := repository.FindActiveHostAccount(ctx, "host-account"); !errors.Is(err, context.Canceled) {
		t.Fatalf("find active host account error = %v, want context canceled", err)
	}
	if _, _, err := repository.FindActivePermanentUserSession(ctx, "user"); !errors.Is(err, context.Canceled) {
		t.Fatalf("find permanent session error = %v, want context canceled", err)
	}
	if _, err := repository.CreateUserSessionWithContext(ctx, model.UserSession{UserID: "user", Type: "permanent", Status: "active"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("create session error = %v, want context canceled", err)
	}
}
