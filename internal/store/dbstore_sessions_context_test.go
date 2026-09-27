package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestUserSessionCreationQueriesHonorCanceledContext(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	defer sqlDB.Close()
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	repository := NewDBStore(db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := repository.FindActiveHostAccount(ctx, "host-account"); !errors.Is(err, context.Canceled) {
		t.Fatalf("find active host account error = %v, want context canceled", err)
	}
	if _, _, err := repository.FindActiveHost(ctx, "host"); !errors.Is(err, context.Canceled) {
		t.Fatalf("find active host error = %v, want context canceled", err)
	}
	if _, _, err := repository.FindActiveDatabaseAccount(ctx, "database-account"); !errors.Is(err, context.Canceled) {
		t.Fatalf("find active database account error = %v, want context canceled", err)
	}
	if err := repository.CreateConnectionPassword(ctx, model.ConnectionPassword{
		UserID:       "user",
		ResourceType: model.ResourceTypeHostAccount,
		ResourceID:   "host-account",
		SecretHash:   "hash",
		ExpiresAt:    time.Now().UTC().Add(time.Minute),
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("create connection password error = %v, want context canceled", err)
	}
	if _, _, err := repository.FindActivePermanentUserSession(ctx, "user"); !errors.Is(err, context.Canceled) {
		t.Fatalf("find permanent session error = %v, want context canceled", err)
	}
	if _, err := repository.CreateUserSessionWithContext(ctx, model.UserSession{UserID: "user", Type: "permanent", Status: "active"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("create session error = %v, want context canceled", err)
	}
}

func TestGetOrCreateActivePermanentUserSessionSerializesConcurrentCreation(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: t.TempDir() + "/sessions.db", MaxOpenConns: 8, MaxIdleConns: 8})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	defer sqlDB.Close()
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	if err := db.Create(&model.User{ID: "user-1", Username: "user-1", Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	repository := NewDBStore(db)

	const callers = 16
	results := make(chan model.UserSession, callers)
	errs := make(chan error, callers)
	var group sync.WaitGroup
	for i := 0; i < callers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			session, err := repository.GetOrCreateActivePermanentUserSession(context.Background(), "user-1")
			if err != nil {
				errs <- err
				return
			}
			results <- session
		}()
	}
	group.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("get or create permanent session: %v", err)
	}
	var sessionID string
	for session := range results {
		if sessionID == "" {
			sessionID = session.SessionID
		}
		if session.SessionID != sessionID {
			t.Fatalf("session id = %q, want %q", session.SessionID, sessionID)
		}
	}
	var count int64
	if err := db.Model(&model.UserSession{}).Where("user_id = ? AND type = ? AND status = ?", "user-1", "permanent", "active").Count(&count).Error; err != nil {
		t.Fatalf("count active permanent sessions: %v", err)
	}
	if count != 1 {
		t.Fatalf("active permanent session count = %d, want 1", count)
	}
}
