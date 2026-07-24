package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/util"
)

func TestSQLiteDSNWithForeignKeysNormalizesMemoryDSNs(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
	}{
		{name: "bare memory", dsn: ":memory:"},
		{name: "shared anonymous memory", dsn: "file::memory:?cache=shared"},
		{name: "shared named memory", dsn: "file:name?mode=memory&cache=shared"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sqliteDSNWithForeignKeys(tt.dsn)
			if !strings.Contains(got, "_pragma=foreign_keys(1)") {
				t.Fatalf("sqlite dsn = %q, want foreign key pragma", got)
			}
			if strings.Count(got, "_pragma=foreign_keys(1)") != 1 {
				t.Fatalf("sqlite dsn = %q, want one foreign key pragma", got)
			}
			if tt.dsn == ":memory:" && got == tt.dsn {
				t.Fatalf("bare memory dsn was not normalized: %q", got)
			}
		})
	}

	for _, tc := range []struct {
		name string
		dsn  string
		want string
	}{
		{name: "one to zero", dsn: "file:existing?mode=memory&_pragma=foreign_keys(0)", want: "file:existing?mode=memory&_pragma=foreign_keys(1)"},
		{name: "zero to one", dsn: "file:existing?mode=memory&_pragma=foreign_keys(1)", want: "file:existing?mode=memory&_pragma=foreign_keys(1)"},
		{name: "duplicate and mixed case", dsn: "file:existing?_pragma=FOREIGN_KEYS(OFF)&x=1&_PRAGMA=foreign_keys(ON)&_pragma=foreign_keys(0)", want: "file:existing?x=1&_pragma=foreign_keys(1)"},
		{name: "encoded", dsn: "file:existing?keep=a%2Bb&_pragma=foreign_keys%28OFF%29&other=2", want: "file:existing?keep=a%2Bb&other=2&_pragma=foreign_keys(1)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sqliteDSNWithForeignKeys(tc.dsn); got != tc.want {
				t.Fatalf("sqlite dsn = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOpenSQLiteNormalizesForeignKeyPragmaPerConnection(t *testing.T) {
	for _, dsn := range []string{
		"file:pragma-0?mode=memory&cache=shared&_pragma=foreign_keys(0)",
		"file:pragma-1?mode=memory&cache=shared&_pragma=foreign_keys(1)",
		"file:pragma-duplicates?mode=memory&_pragma=FOREIGN_KEYS(OFF)&_pragma=foreign_keys%28ON%29",
		"file:pragma-encoded?mode=memory&_pragma=foreign_keys%28OFF%29",
	} {
		t.Run(dsn, func(t *testing.T) {
			db, err := Open(Config{Driver: DriverSQLite, DSN: dsn, MaxOpenConns: 2, MaxIdleConns: 2})
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatalf("get SQL database: %v", err)
			}
			defer sqlDB.Close()
			for i := 0; i < 2; i++ {
				conn, err := sqlDB.Conn(context.Background())
				if err != nil {
					t.Fatalf("acquire connection: %v", err)
				}
				var enabled int
				err = conn.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&enabled)
				_ = conn.Close()
				if err != nil {
					t.Fatalf("read foreign key pragma: %v", err)
				}
				if enabled != 1 {
					t.Fatalf("foreign_keys = %d, want 1", enabled)
				}
			}
		})
	}
}

func TestOpenSQLiteMemoryForeignKeysWorkAcrossConnections(t *testing.T) {
	for _, dsn := range []string{
		":memory:",
		"file::memory:?cache=shared",
		"file:name?mode=memory&cache=shared",
	} {
		t.Run(dsn, func(t *testing.T) {
			db, err := Open(Config{
				Driver:       DriverSQLite,
				DSN:          dsn,
				MaxOpenConns: 2,
				MaxIdleConns: 2,
			})
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatalf("get SQL database: %v", err)
			}
			defer sqlDB.Close()

			ctx := context.Background()
			connections := make([]*sql.Conn, 0, 2)
			for index := 0; index < 2; index++ {
				conn, err := sqlDB.Conn(ctx)
				if err != nil {
					t.Fatalf("acquire sqlite connection %d: %v", index, err)
				}
				connections = append(connections, conn)
			}
			defer func() {
				for _, conn := range connections {
					_ = conn.Close()
				}
			}()

			for index, conn := range connections {
				var enabled int
				if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled); err != nil {
					t.Fatalf("read foreign key pragma on connection %d: %v", index, err)
				}
				if enabled != 1 {
					t.Fatalf("foreign_keys on connection %d = %d, want 1", index, enabled)
				}
			}

			conn := connections[0]
			if _, err := conn.ExecContext(ctx, "CREATE TABLE fk_parent (id integer primary key)"); err != nil {
				t.Fatalf("create parent table: %v", err)
			}
			if _, err := conn.ExecContext(ctx, "CREATE TABLE fk_child (parent_id integer references fk_parent(id) ON DELETE RESTRICT)"); err != nil {
				t.Fatalf("create child table: %v", err)
			}
			if _, err := conn.ExecContext(ctx, "INSERT INTO fk_child (parent_id) VALUES (999)"); err == nil {
				t.Fatal("foreign key violation succeeded")
			}
			if _, err := conn.ExecContext(ctx, "INSERT INTO fk_parent (id) VALUES (1)"); err != nil {
				t.Fatalf("insert parent row: %v", err)
			}
			if _, err := conn.ExecContext(ctx, "INSERT INTO fk_child (parent_id) VALUES (1)"); err != nil {
				t.Fatalf("insert child row: %v", err)
			}
			if _, err := conn.ExecContext(ctx, "DELETE FROM fk_parent WHERE id = 1"); err == nil {
				t.Fatal("restricted parent delete succeeded")
			}
		})
	}
}

func TestOpenSQLiteEnablesForeignKeysForEveryConnection(t *testing.T) {
	db, err := Open(Config{
		Driver:       DriverSQLite,
		DSN:          filepath.Join(t.TempDir(), "foreign-keys.db"),
		MaxOpenConns: 2,
		MaxIdleConns: 2,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get SQL database: %v", err)
	}
	defer sqlDB.Close()
	ctx := context.Background()
	connections := make([]*sql.Conn, 0, 2)
	for index := 0; index < 2; index++ {
		conn, err := sqlDB.Conn(ctx)
		if err != nil {
			t.Fatalf("acquire sqlite connection %d: %v", index, err)
		}
		connections = append(connections, conn)
	}
	for index, conn := range connections {
		var enabled int
		err = conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled)
		if err != nil {
			t.Fatalf("read foreign key pragma on connection %d: %v", index, err)
		}
		if enabled != 1 {
			t.Fatalf("foreign_keys on connection %d = %d, want 1", index, enabled)
		}
	}
	for _, conn := range connections {
		_ = conn.Close()
	}
	if err := db.Exec("CREATE TABLE fk_parent (id integer primary key)").Error; err != nil {
		t.Fatalf("create parent table: %v", err)
	}
	if err := db.Exec("CREATE TABLE fk_child (parent_id integer references fk_parent(id) ON DELETE RESTRICT)").Error; err != nil {
		t.Fatalf("create child table: %v", err)
	}
	if err := db.Exec("INSERT INTO fk_child (parent_id) VALUES (999)").Error; err == nil {
		t.Fatal("foreign key violation succeeded")
	}
	if err := db.Exec("INSERT INTO fk_parent (id) VALUES (1)").Error; err != nil {
		t.Fatalf("insert parent row: %v", err)
	}
	if err := db.Exec("INSERT INTO fk_child (parent_id) VALUES (1)").Error; err != nil {
		t.Fatalf("insert child row: %v", err)
	}
	if err := db.Exec("DELETE FROM fk_parent WHERE id = 1").Error; err == nil {
		t.Fatal("restricted parent delete succeeded")
	}
}

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
		{name: "global session id", model: &model.UserSession{}, index: "idx_user_sessions_session_id_deleted"},
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
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("create legacy session parent table: %v", err)
	}
	for _, user := range []model.User{
		{ID: "u1", Username: "legacy-user-1", Status: "active"},
		{ID: "u2", Username: "legacy-user-2", Status: "active"},
	} {
		if err := db.Create(&user).Error; err != nil {
			t.Fatalf("insert legacy session parent user %s: %v", user.ID, err)
		}
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
	if !db.Migrator().HasIndex(&model.UserSession{}, "idx_user_sessions_session_id_deleted") {
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
			{ID: "u-admin", Username: "admin", Password: "admin-password", SuperAdmin: true},
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
	for _, user := range users {
		if user.Username == "admin" && user.MySQLNativeHash != util.MySQLNativePasswordHash("admin-password") {
			t.Fatal("bootstrap did not store the MySQL password verifier")
		}
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
