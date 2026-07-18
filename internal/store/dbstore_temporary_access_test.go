package store

import (
	"context"
	"strings"
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

func TestDBStoreExtendTemporaryAccessRejectsMissingAggregateRecords(t *testing.T) {
	db := newTemporaryAccessTestDB(t)
	now := time.Now().UTC()
	if err := db.Create(&model.TemporaryAccount{ID: "temporary-1", SessionID: "ABCDE", Type: model.TemporaryAccountTypeUser, Username: "tmp_ABCDE", Status: "disabled", StartsAt: now}).Error; err != nil {
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
	if account.Status != "disabled" || account.ExpiresAt != nil {
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

func TestDBStoreCreateTemporaryAccountAllocatesUniqueUsername(t *testing.T) {
	db := newTemporaryAccessTestDB(t)
	store := NewDBStore(db)
	first, err := store.CreateTemporaryAccount(context.Background(), service.CreateTemporaryAccountInput{AccountType: model.TemporaryAccountTypeAI, Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("create first temporary account: %v", err)
	}
	second, err := store.CreateTemporaryAccount(context.Background(), service.CreateTemporaryAccountInput{AccountType: model.TemporaryAccountTypeAI, Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("create second temporary account: %v", err)
	}
	if first.Username == second.Username || first.SessionID == second.SessionID {
		t.Fatalf("temporary accounts are not unique: first=%#v second=%#v", first, second)
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
