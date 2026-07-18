package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/storage"

	"gorm.io/gorm"
)

func TestDBStoreCreateTemporaryAccessRollsBackOnConnectionPasswordFailure(t *testing.T) {
	db := newTemporaryAccessTestDB(t)
	seedTemporaryAccessUserAndHostAccount(t, db)
	if err := db.Exec(`CREATE TRIGGER fail_temporary_connection_password BEFORE INSERT ON connection_passwords BEGIN SELECT RAISE(ABORT, 'injected connection password failure'); END;`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	store := NewDBStore(db)
	_, err := store.CreateTemporaryAccess(context.Background(), service.CreateTemporaryAccessInput{
		AuthorizedUserID: "user-1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "host-account-1",
		ExpiresAt: time.Now().UTC().Add(time.Hour), Now: time.Now().UTC(),
		ConnectionPassword: model.ConnectionPassword{SecretHash: "hash", MySQLNativeHash: "mysql-hash", ExpiresAt: time.Now().UTC().Add(time.Hour)},
	})
	if err == nil || !strings.Contains(err.Error(), "injected connection password failure") {
		t.Fatalf("error = %v, want injected connection password failure", err)
	}
	for _, table := range []string{"user_sessions", "temporary_accounts", "temporary_account_grants", "connection_passwords", "ai_access_tokens"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows after rollback = %d, want 0", table, count)
		}
	}
}

func TestDBStoreCreateTemporaryAccessAllocatesUniqueSessionSequence(t *testing.T) {
	db := newTemporaryAccessTestDB(t)
	seedTemporaryAccessUserAndHostAccount(t, db)
	store := NewDBStore(db)
	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 2; i++ {
		_, err := store.CreateTemporaryAccess(context.Background(), service.CreateTemporaryAccessInput{
			AuthorizedUserID: "user-1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "host-account-1",
			ExpiresAt: now.Add(time.Hour), Now: now,
			ConnectionPassword: model.ConnectionPassword{UserID: "user-1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "host-account-1", SecretHash: "hash", ExpiresAt: now.Add(time.Hour)},
		})
		if err != nil {
			t.Fatalf("create temporary access %d: %v", i, err)
		}
	}
	var sessions []model.UserSession
	if err := db.Order("session_seq").Find(&sessions).Error; err != nil {
		t.Fatalf("load sessions: %v", err)
	}
	if len(sessions) != 2 || sessions[0].SessionSeq != 1 || sessions[1].SessionSeq != 2 || sessions[0].SessionID == sessions[1].SessionID {
		t.Fatalf("unexpected temporary sessions: %#v", sessions)
	}
}

func TestDBStoreDisableTemporaryAccessRevokesItsConnectionPassword(t *testing.T) {
	db := newTemporaryAccessTestDB(t)
	seedTemporaryAccessUserAndHostAccount(t, db)
	repository := NewDBStore(db)
	temporaryAccess, err := service.NewTemporaryAccessService(repository)
	if err != nil {
		t.Fatalf("new temporary access service: %v", err)
	}
	now := time.Now().UTC()
	result, err := temporaryAccess.Create(context.Background(), service.CreateTemporaryAccessInput{
		AuthorizedUserID: "user-1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "host-account-1",
		ExpiresAt: now.Add(time.Hour), Now: now,
	})
	if err != nil {
		t.Fatalf("create temporary access: %v", err)
	}
	if err := repository.AuthenticateConnectionPassword(context.Background(), "user-1", model.ResourceTypeHostAccount, "host-account-1", result.ConnectionPassword); err != nil {
		t.Fatalf("authenticate before disable: %v", err)
	}
	if err := repository.DisableTemporaryAccess(context.Background(), result.Account.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("disable temporary access: %v", err)
	}
	if err := repository.AuthenticateConnectionPassword(context.Background(), "user-1", model.ResourceTypeHostAccount, "host-account-1", result.ConnectionPassword); err == nil {
		t.Fatal("disabled temporary access connection password still authenticated")
	}
}

func TestDBStoreExtendDisabledTemporaryAccessIsRejected(t *testing.T) {
	db := newTemporaryAccessTestDB(t)
	seedTemporaryAccessUserAndHostAccount(t, db)
	repository := NewDBStore(db)
	temporaryAccess, err := service.NewTemporaryAccessService(repository)
	if err != nil {
		t.Fatalf("new temporary access service: %v", err)
	}
	now := time.Now().UTC()
	result, err := temporaryAccess.Create(context.Background(), service.CreateTemporaryAccessInput{
		AuthorizedUserID: "user-1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "host-account-1",
		ExpiresAt: now.Add(time.Hour), Now: now,
	})
	if err != nil {
		t.Fatalf("create temporary access: %v", err)
	}
	if err := repository.DisableTemporaryAccess(context.Background(), result.Account.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("disable temporary access: %v", err)
	}
	if err := repository.ExtendTemporaryAccess(context.Background(), result.Account.ID, now.Add(2*time.Hour)); err == nil {
		t.Fatal("disabled temporary access was extended and reactivated")
	}
}

func TestDBStoreExtendActiveTemporaryAccessExtendsBoundPassword(t *testing.T) {
	db := newTemporaryAccessTestDB(t)
	seedTemporaryAccessUserAndHostAccount(t, db)
	repository := NewDBStore(db)
	temporaryAccess, err := service.NewTemporaryAccessService(repository)
	if err != nil {
		t.Fatalf("new temporary access service: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	result, err := temporaryAccess.Create(context.Background(), service.CreateTemporaryAccessInput{
		AuthorizedUserID: "user-1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "host-account-1",
		ExpiresAt: now.Add(time.Hour), Now: now,
	})
	if err != nil {
		t.Fatalf("create temporary access: %v", err)
	}
	extendedExpiry := now.Add(2 * time.Hour)
	if err := repository.ExtendTemporaryAccess(context.Background(), result.Account.ID, extendedExpiry); err != nil {
		t.Fatalf("extend temporary access: %v", err)
	}
	var passwordExpiry time.Time
	if err := db.Raw("SELECT expires_at FROM connection_passwords WHERE temporary_account_id = ?", result.Account.ID).Scan(&passwordExpiry).Error; err != nil {
		t.Fatalf("load bound connection password: %v", err)
	}
	if !passwordExpiry.Equal(extendedExpiry) {
		t.Fatalf("connection password expiry = %v, want %v", passwordExpiry, extendedExpiry)
	}
	if err := repository.AuthenticateConnectionPassword(context.Background(), "user-1", model.ResourceTypeHostAccount, "host-account-1", result.ConnectionPassword); err != nil {
		t.Fatalf("extended connection password no longer authenticates: %v", err)
	}
}

func TestDBStoreCreateTemporaryAccessConcurrentSessionsAreUnique(t *testing.T) {
	db := newConcurrentTemporaryAccessTestDB(t)
	seedTemporaryAccessUserAndHostAccount(t, db)
	repository := NewDBStore(db)
	const workers = 12
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	now := time.Now().UTC()
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			temporaryAccess, err := service.NewTemporaryAccessService(repository)
			if err != nil {
				errs <- err
				return
			}
			_, err = temporaryAccess.Create(context.Background(), service.CreateTemporaryAccessInput{
				AuthorizedUserID: "user-1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "host-account-1",
				ExpiresAt: now.Add(time.Hour), Now: now, Remark: fmt.Sprintf("worker-%d", index),
			})
			errs <- err
		}(index)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if errors.Is(err, service.ErrTemporaryAccessNotFound) {
			t.Fatalf("sequence contention was misclassified as not found: %v", err)
		}
		if err != nil {
			t.Fatalf("concurrent create failed: %v", err)
		}
	}
	var sessions []model.UserSession
	if err := db.Order("session_seq").Find(&sessions).Error; err != nil {
		t.Fatalf("load concurrent sessions: %v", err)
	}
	if len(sessions) != workers {
		t.Fatalf("session count = %d, want %d", len(sessions), workers)
	}
	seenSequences := make(map[int]bool, workers)
	seenIDs := make(map[string]bool, workers)
	for _, session := range sessions {
		if seenSequences[session.SessionSeq] || seenIDs[session.SessionID] {
			t.Fatalf("duplicate session identity: seq=%d id=%q", session.SessionSeq, session.SessionID)
		}
		seenSequences[session.SessionSeq] = true
		seenIDs[session.SessionID] = true
	}
}

func TestDBStoreExtendTemporaryAccessRejectsMissingAggregateRecords(t *testing.T) {
	db := newTemporaryAccessTestDB(t)
	now := time.Now().UTC()
	if err := db.Create(&model.TemporaryAccount{ID: "temporary-1", SessionID: "ABCDE", Type: model.TemporaryAccountTypeUser, Username: "tmp_ABCDE", Status: "active", StartsAt: now}).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	err := NewDBStore(db).ExtendTemporaryAccess(context.Background(), "temporary-1", now.Add(time.Hour))
	if err == nil {
		t.Fatal("extend succeeded without its session and grant")
	}
	var account model.TemporaryAccount
	if err := db.First(&account, "id = ?", "temporary-1").Error; err != nil {
		t.Fatalf("load account: %v", err)
	}
	if account.Status != "active" || account.ExpiresAt != nil {
		t.Fatalf("account was partially updated: %#v", account)
	}
}

func TestDBStoreDisableTemporaryAccessRollsBackWhenSessionUpdateFails(t *testing.T) {
	db := newTemporaryAccessTestDB(t)
	now := time.Now().UTC()
	account := model.TemporaryAccount{ID: "temporary-1", SessionID: "ABCDE", Type: model.TemporaryAccountTypeUser, Username: "tmp_ABCDE", AuthorizedUserID: "user-1", Status: "active", StartsAt: now}
	grant := model.TemporaryAccountGrant{ID: "grant-1", TemporaryAccountID: account.ID, UserID: "user-1", ResourceType: model.ResourceTypeHostAccount, ResourceID: "host-account-1"}
	session := model.UserSession{ID: "session-1", UserID: "user-1", SessionSeq: 1, SessionID: account.SessionID, Type: "temporary", Status: "active"}
	user := model.User{ID: "user-1", Username: "user-1", Status: "active"}
	for _, value := range []any{&user, &account, &grant, &session} {
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("seed temporary aggregate: %v", err)
		}
	}
	if err := db.Exec(`CREATE TRIGGER fail_temporary_session_update BEFORE UPDATE ON user_sessions BEGIN SELECT RAISE(ABORT, 'injected session update failure'); END;`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	err := NewDBStore(db).DisableTemporaryAccess(context.Background(), account.ID, now)
	if err == nil || !strings.Contains(err.Error(), "injected session update failure") {
		t.Fatalf("error = %v, want injected session update failure", err)
	}
	if err := db.First(&account, "id = ?", account.ID).Error; err != nil {
		t.Fatalf("load account: %v", err)
	}
	if account.Status != "active" {
		t.Fatalf("account status = %q, want active after rollback", account.Status)
	}
	if err := db.First(&grant, "id = ?", grant.ID).Error; err != nil {
		t.Fatalf("load grant: %v", err)
	}
	if grant.RevokedAt != nil {
		t.Fatalf("grant was revoked despite rollback: %#v", grant.RevokedAt)
	}
}

func TestDBStoreDisableOrphanAIAccessConverges(t *testing.T) {
	db := newTemporaryAccessTestDB(t)
	now := time.Now().UTC()
	account := model.TemporaryAccount{
		ID: "orphan-ai", SessionID: "AIA01", Type: model.TemporaryAccountTypeAI,
		Username: "tmp_AIA01", Status: "active", StartsAt: now,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create orphan AI account: %v", err)
	}
	if err := NewDBStore(db).DisableTemporaryAccess(context.Background(), account.ID, now); err != nil {
		t.Fatalf("disable orphan AI account: %v", err)
	}
	if err := db.First(&account, "id = ?", account.ID).Error; err != nil {
		t.Fatalf("reload orphan AI account: %v", err)
	}
	if account.Status != "disabled" {
		t.Fatalf("orphan AI account status = %q, want disabled", account.Status)
	}
}

func newTemporaryAccessTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func newConcurrentTemporaryAccessTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "temporary-access.db"))
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite, DSN: dsn, MaxOpenConns: 16, MaxIdleConns: 16,
	})
	if err != nil {
		t.Fatalf("open concurrent sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate concurrent sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get concurrent sql db: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close concurrent sqlite: %v", err)
		}
	})
	return db
}

func seedTemporaryAccessUserAndHostAccount(t *testing.T, db *gorm.DB) {
	t.Helper()
	values := []any{
		&model.User{ID: "user-1", Username: "user-1", Status: "active"},
		&model.Host{ID: "host-1", Name: "host-1", Address: "127.0.0.1", Port: 22, Status: "active"},
		&model.HostAccount{ID: "host-account-1", HostID: "host-1", Name: "root", Username: "root", Status: "active", ResourceID: "A001"},
	}
	for _, value := range values {
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("seed temporary access: %v", err)
		}
	}
}
