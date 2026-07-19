package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/storage"
)

func TestDBStoreBrowserSessionsPersistOnlyHashesAndBindTarget(t *testing.T) {
	store, db := newBrowserSessionDBStore(t)
	sessions, err := service.NewBrowserSessionService(store)
	if err != nil {
		t.Fatal(err)
	}
	created, err := sessions.Create(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	var persisted model.AdminSession
	if err := db.First(&persisted, "id = ?", created.SessionID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.SecretHash == created.Secret || persisted.CSRFHash == created.CSRFToken {
		t.Fatal("raw browser session credential was persisted")
	}
	subject, found, err := sessions.Authenticate(context.Background(), created.Secret)
	if err != nil || !found {
		t.Fatalf("authenticate = found=%v err=%v", found, err)
	}
	ticket, err := sessions.CreateWebSocketTicket(context.Background(), subject, "target-1")
	if err != nil {
		t.Fatal(err)
	}
	var persistedTicket model.WebSocketTicket
	if err := db.First(&persistedTicket, "session_id = ?", created.SessionID).Error; err != nil {
		t.Fatal(err)
	}
	if persistedTicket.SecretHash == ticket {
		t.Fatal("raw websocket ticket was persisted")
	}
	if _, found, err := sessions.ConsumeWebSocketTicket(context.Background(), ticket, "target-2"); err != nil || found {
		t.Fatalf("target binding failed: found=%v err=%v", found, err)
	}
	if _, found, err := sessions.ConsumeWebSocketTicket(context.Background(), ticket, "target-1"); err != nil || !found {
		t.Fatalf("bound ticket consume = found=%v err=%v", found, err)
	}
}

func TestDBStoreBrowserSessionTicketConcurrentConsumeExactlyOne(t *testing.T) {
	store, _ := newBrowserSessionDBStore(t)
	sessions, err := service.NewBrowserSessionService(store)
	if err != nil {
		t.Fatal(err)
	}
	created, err := sessions.Create(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	subject, found, err := sessions.Authenticate(context.Background(), created.Secret)
	if err != nil || !found {
		t.Fatalf("authenticate = found=%v err=%v", found, err)
	}
	ticket, err := sessions.CreateWebSocketTicket(context.Background(), subject, "target-1")
	if err != nil {
		t.Fatal(err)
	}

	const workers = 16
	results := make(chan bool, workers)
	errs := make(chan error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, found, err := sessions.ConsumeWebSocketTicket(context.Background(), ticket, "target-1")
			if err != nil {
				errs <- err
				return
			}
			results <- found
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("consume returned error: %v", err)
	}
	successes := 0
	for found := range results {
		if found {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful consumes = %d, want exactly one", successes)
	}
}

func TestDBStoreBrowserSessionTicketDoesNotBurnForRevokedOrExpiredSession(t *testing.T) {
	store, db := newBrowserSessionDBStore(t)
	now := time.Now().UTC()
	for _, test := range []struct {
		name    string
		revoked bool
		expired bool
	}{
		{name: "revoked", revoked: true},
		{name: "expired", expired: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			secret := "ticket-" + test.name
			session := model.AdminSession{ID: "session-" + test.name, UserID: "user-1", SecretHash: hashBrowserSessionTestValue("session-" + test.name), CSRFHash: hashBrowserSessionTestValue("csrf-" + test.name), ExpiresAt: now.Add(time.Hour)}
			if test.revoked {
				session.RevokedAt = &now
			}
			if test.expired {
				session.ExpiresAt = now.Add(-time.Second)
			}
			ticket := model.WebSocketTicket{ID: "ticket-row-" + test.name, SessionID: session.ID, TargetID: "target-1", SecretHash: hashBrowserSessionTestValue(secret), ExpiresAt: now.Add(time.Minute)}
			if err := db.Create(&session).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&ticket).Error; err != nil {
				t.Fatal(err)
			}
			if _, found, err := store.ConsumeWebSocketTicket(context.Background(), hashBrowserSessionTestValue(secret), service.WebSocketPurposeTerminal, "target-1", now); err != nil || found {
				t.Fatalf("invalid session ticket consume = found=%v err=%v", found, err)
			}
			var persisted model.WebSocketTicket
			if err := db.First(&persisted, "id = ?", ticket.ID).Error; err != nil {
				t.Fatal(err)
			}
			if persisted.ConsumedAt != nil {
				t.Fatal("ticket was consumed before session validity was established")
			}
		})
	}
}

func newBrowserSessionDBStore(t *testing.T) (*DBStore, *gorm.DB) {
	t.Helper()
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: t.TempDir() + "/browser-sessions.db", MaxOpenConns: 8, MaxIdleConns: 8})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return NewDBStore(db), db
}

func hashBrowserSessionTestValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
