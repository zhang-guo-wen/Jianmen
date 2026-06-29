package storage

import (
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
)

func TestOpenAndAutoMigrateSQLite(t *testing.T) {
	db, err := Open(Config{
		Driver: DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	for _, m := range model.AllModels() {
		if !db.Migrator().HasTable(m) {
			t.Fatalf("expected table for %T", m)
		}
	}
}

func TestMigrateAppliesVersionedMigrations(t *testing.T) {
	db, err := Open(Config{
		Driver: DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, m := range model.AllModels() {
		if !db.Migrator().HasTable(m) {
			t.Fatalf("expected table for %T", m)
		}
	}

	var count int64
	if err := db.Model(&SchemaMigration{}).Count(&count).Error; err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != int64(len(migrations)) {
		t.Fatalf("migration count = %d, want %d", count, len(migrations))
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if err := db.Model(&SchemaMigration{}).Count(&count).Error; err != nil {
		t.Fatalf("count migrations after rerun: %v", err)
	}
	if count != int64(len(migrations)) {
		t.Fatalf("migration count after rerun = %d, want %d", count, len(migrations))
	}
	for _, tc := range []struct {
		name  string
		model any
		index string
	}{
		{name: "global session id", model: &model.UserSession{}, index: "idx_user_sessions_session_id"},
		{name: "session user timeline", model: &model.Session{}, index: "idx_sessions_user_started"},
		{name: "host account lookup", model: &model.HostAccount{}, index: "idx_host_accounts_host_username"},
		{name: "database account lookup", model: &model.DatabaseAccount{}, index: "idx_database_accounts_instance_username"},
		{name: "rbac user expiry", model: &model.UserRole{}, index: "idx_user_roles_user_expiry"},
	} {
		if !db.Migrator().HasIndex(tc.model, tc.index) {
			t.Fatalf("expected %s index %q", tc.name, tc.index)
		}
	}
}

func TestMigrateRepairsDuplicateUserSessionsBeforeIndexes(t *testing.T) {
	db, err := Open(Config{
		Driver: DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`CREATE TABLE user_sessions (
		id text primary key,
		user_id text,
		session_seq integer,
		session_id text,
		type text,
		status text,
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create legacy sessions table: %v", err)
	}
	for _, stmt := range []string{
		`INSERT INTO user_sessions (id, user_id, session_seq, session_id, type, status) VALUES ('s1', 'u1', 1, '00001', 'permanent', 'active')`,
		`INSERT INTO user_sessions (id, user_id, session_seq, session_id, type, status) VALUES ('s2', 'u1', 1, '00001', 'temporary', 'active')`,
		`INSERT INTO user_sessions (id, user_id, session_seq, session_id, type, status) VALUES ('s3', 'u1', 0, '', 'temporary', 'active')`,
		`INSERT INTO user_sessions (id, user_id, session_seq, session_id, type, status) VALUES ('s4', 'u2', 1, '00001', 'permanent', 'active')`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("insert legacy session: %v", err)
		}
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var sessions []model.UserSession
	if err := db.Order("session_seq").Find(&sessions).Error; err != nil {
		t.Fatalf("load sessions: %v", err)
	}
	if len(sessions) != 4 {
		t.Fatalf("sessions = %d, want 4", len(sessions))
	}
	seenSeq := map[int]bool{}
	seenID := map[string]bool{}
	for _, sess := range sessions {
		if sess.SessionSeq <= 0 || sess.SessionID == "" {
			t.Fatalf("unrepaired session: %#v", sess)
		}
		if seenSeq[sess.SessionSeq] {
			t.Fatalf("duplicate repaired session seq: %#v", sessions)
		}
		if seenID[sess.SessionID] {
			t.Fatalf("duplicate repaired session id: %#v", sessions)
		}
		seenSeq[sess.SessionSeq] = true
		seenID[sess.SessionID] = true
	}
	if !db.Migrator().HasIndex(&model.UserSession{}, "idx_user_sessions_session_id") {
		t.Fatal("expected global user session id unique index")
	}
}

func TestOpenRejectsMissingNetworkDSN(t *testing.T) {
	if _, err := Open(Config{Driver: DriverMySQL}); err == nil {
		t.Fatal("expected mysql without dsn to fail")
	}
	if _, err := Open(Config{Driver: DriverPostgres}); err == nil {
		t.Fatal("expected postgres without dsn to fail")
	}
}

func TestBootstrapMetadataSeedsUsersOnly(t *testing.T) {
	db, err := Open(Config{
		Driver: DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	cfg := &config.Config{
		Users: []config.User{
			{ID: "u-admin", Username: "admin"},
			{Username: "operator"},
		},
	}
	if err := BootstrapMetadata(db, cfg); err != nil {
		t.Fatalf("bootstrap metadata: %v", err)
	}

	var users []model.User
	if err := db.Order("username").Find(&users).Error; err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("users = %d, want 2: %#v", len(users), users)
	}

	// 去掉预置角色后，bootstrap 不应创建任何角色
	var roles []model.Role
	if err := db.Find(&roles).Error; err != nil {
		t.Fatalf("list roles: %v", err)
	}
	if len(roles) != 0 {
		t.Fatalf("roles = %d, want 0 — no builtin roles should exist", len(roles))
	}

	// 去掉预置权限后，bootstrap 不应创建任何权限
	var permissions []model.Permission
	if err := db.Find(&permissions).Error; err != nil {
		t.Fatalf("list permissions: %v", err)
	}
	if len(permissions) != 0 {
		t.Fatalf("permissions = %d, want 0 — no builtin permissions should exist", len(permissions))
	}
}
