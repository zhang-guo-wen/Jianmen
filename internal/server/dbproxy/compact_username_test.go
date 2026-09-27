package dbproxy

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/storage"
	"jianmen/internal/util"
)

func TestResolveAccountRejectsHostCompactPrefix(t *testing.T) {
	gateway := &Gateway{}
	_, err := gateway.resolveAccount(context.Background(), util.FullUsername(util.PrefixHost, 1, 1))
	if err == nil {
		t.Fatal("expected host compact prefix to be rejected")
	}
	if !strings.Contains(err.Error(), "expected D") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveAccountRejectsDisabledDatabaseAssets(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := db.Create(&model.DatabaseInstance{
		ID:       "db-disabled-account",
		Name:     "db-disabled-account",
		Protocol: "mysql",
		Address:  "127.0.0.1",
		Port:     3306,
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("create instance: %v", err)
	}
	if err := db.Create(&model.DatabaseAccount{
		ID:          "dbacct-disabled",
		InstanceID:  "db-disabled-account",
		UniqueName:  "dbacct-disabled",
		Username:    "app",
		Status:      "disabled",
		ResourceSeq: 1,
		ResourceID:  util.ResourceIDFromSeq(util.PrefixDatabase, 1),
	}).Error; err != nil {
		t.Fatalf("create disabled account: %v", err)
	}

	gateway := &Gateway{db: db}
	if _, err := gateway.resolveAccount(context.Background(), util.FullUsername(util.PrefixDatabase, 1, 1)); err == nil {
		t.Fatal("disabled database account resolved successfully")
	}

	if err := db.Create(&model.DatabaseInstance{
		ID:       "db-disabled-instance",
		Name:     "db-disabled-instance",
		Protocol: "mysql",
		Address:  "127.0.0.1",
		Port:     3307,
		Status:   "disabled",
	}).Error; err != nil {
		t.Fatalf("create disabled instance: %v", err)
	}
	if err := db.Create(&model.DatabaseAccount{
		ID:          "dbacct-active-on-disabled",
		InstanceID:  "db-disabled-instance",
		UniqueName:  "dbacct-active-on-disabled",
		Username:    "app",
		Status:      "active",
		ResourceSeq: 2,
		ResourceID:  util.ResourceIDFromSeq(util.PrefixDatabase, 2),
	}).Error; err != nil {
		t.Fatalf("create active account on disabled instance: %v", err)
	}
	_, err = gateway.resolveAccount(context.Background(), util.FullUsername(util.PrefixDatabase, 2, 1))
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled database instance error = %v, want disabled", err)
	}
}

type resolveContextKey struct{}

func TestResolveAccountPropagatesConnectionContextToEveryQuery(t *testing.T) {
	db := newResolvableDatabaseAccount(t, nil)
	var queryContexts []context.Context
	if err := db.Callback().Query().Before("gorm:query").Register("test:capture_query_context", func(tx *gorm.DB) {
		queryContexts = append(queryContexts, tx.Statement.Context)
	}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}

	ctx := context.WithValue(context.Background(), resolveContextKey{}, "connection")
	gateway := &Gateway{db: db}
	if _, err := gateway.resolveAccount(ctx, util.FullUsername(util.PrefixDatabase, 1, 1)); err != nil {
		t.Fatalf("resolveAccount returned error: %v", err)
	}
	if len(queryContexts) < 3 {
		t.Fatalf("query callbacks = %d, want account, session, and user queries", len(queryContexts))
	}
	for index, queryCtx := range queryContexts {
		if queryCtx.Value(resolveContextKey{}) != "connection" {
			t.Fatalf("query %d did not receive the connection context", index)
		}
	}
}

func TestResolveAccountPropagatesConnectionContextToExpiryUpdate(t *testing.T) {
	expiredAt := time.Now().UTC().Add(-time.Hour)
	db := newResolvableDatabaseAccount(t, &expiredAt)
	var updateContexts []context.Context
	if err := db.Callback().Update().Before("gorm:update").Register("test:capture_update_context", func(tx *gorm.DB) {
		updateContexts = append(updateContexts, tx.Statement.Context)
	}); err != nil {
		t.Fatalf("register update callback: %v", err)
	}

	ctx := context.WithValue(context.Background(), resolveContextKey{}, "connection")
	gateway := &Gateway{db: db}
	_, err := gateway.resolveAccount(ctx, util.FullUsername(util.PrefixDatabase, 1, 1))
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("resolveAccount error = %v, want expired session", err)
	}
	if len(updateContexts) != 1 {
		t.Fatalf("update callbacks = %d, want 1", len(updateContexts))
	}
	if updateContexts[0].Value(resolveContextKey{}) != "connection" {
		t.Fatal("session expiry update did not receive the connection context")
	}
}

func newResolvableDatabaseAccount(t *testing.T, sessionExpiresAt *time.Time) *gorm.DB {
	t.Helper()
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	instance := model.DatabaseInstance{
		ID:       "db-instance-1",
		Name:     "db-instance-1",
		Protocol: "mysql",
		Address:  "127.0.0.1",
		Port:     3306,
		Status:   "active",
	}
	account := model.DatabaseAccount{
		ID:          "db-account-1",
		InstanceID:  instance.ID,
		UniqueName:  "db-account-1",
		Username:    "app",
		Status:      "active",
		ResourceSeq: 1,
		ResourceID:  util.ResourceIDFromSeq(util.PrefixDatabase, 1),
	}
	user := model.User{ID: "user-1", Username: "user-1", Status: "active"}
	session := model.UserSession{
		ID:         "user-session-1",
		UserID:     user.ID,
		SessionSeq: 1,
		SessionID:  util.EncodeBase62Padded(1, 5),
		Type:       "permanent",
		Status:     "active",
		ExpiresAt:  sessionExpiresAt,
	}
	for _, value := range []any{&instance, &account, &user, &session} {
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("create %T: %v", value, err)
		}
	}
	return db
}
