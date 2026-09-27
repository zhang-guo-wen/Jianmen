package storage

import (
	"strings"
	"testing"

	"gorm.io/gorm"
)

// legacyHostAccountsDDL 模拟尚未迁移的旧结构：id 是单列主键。
const legacyHostAccountsDDL = `CREATE TABLE "host_accounts" (
	"id" text,
	"host_id" text NOT NULL,
	"name" text NOT NULL DEFAULT "",
	"username" text NOT NULL,
	"auth_type" text,
	"password" text,
	"private_key_pem" text,
	"passphrase" text,
	"insecure_ignore_host_key" numeric NOT NULL DEFAULT false,
	"host_key_fingerprint" text,
	"known_hosts_path" text,
	"status" text NOT NULL DEFAULT "active",
	"resource_seq" integer NOT NULL DEFAULT 0,
	"resource_id" text,
	"group_name" text,
	"remark" text,
	"expires_at" datetime,
	"created_at" datetime,
	"updated_at" datetime,
	"domain" text,
	"rdp_security" text NOT NULL DEFAULT "any",
	"rdp_ignore_certificate" numeric NOT NULL DEFAULT false,
	"rdp_cert_fingerprints" text,
	"rdp_clipboard_read" numeric NOT NULL DEFAULT false,
	"rdp_clipboard_write" numeric NOT NULL DEFAULT false,
	"rdp_file_upload" numeric NOT NULL DEFAULT false,
	"rdp_file_download" numeric NOT NULL DEFAULT false,
	"rdp_drive_mapping" numeric NOT NULL DEFAULT false,
	"created_by" text NOT NULL DEFAULT "",
	"updated_by" text NOT NULL DEFAULT "",
	"active_marker" integer DEFAULT 1,
	PRIMARY KEY ("id"),
	CONSTRAINT "fk_host_accounts_host" FOREIGN KEY ("host_id") REFERENCES "hosts"("id") ON DELETE CASCADE ON UPDATE CASCADE
)`

func TestMigrateHostAccountIDActiveReplacesPrimaryKeyWithCompositeUnique(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`CREATE TABLE "hosts" (
		"id" text PRIMARY KEY,
		"name" text NOT NULL,
		"address" text NOT NULL,
		"port" integer NOT NULL,
		"protocol" text NOT NULL DEFAULT "ssh",
		"status" text NOT NULL DEFAULT "active",
		"active_marker" integer DEFAULT 1
	)`).Error; err != nil {
		t.Fatalf("create hosts table: %v", err)
	}
	if err := db.Exec(legacyHostAccountsDDL).Error; err != nil {
		t.Fatalf("create legacy host_accounts table: %v", err)
	}
	if err := db.Exec(`CREATE INDEX idx_host_accounts_host_username ON host_accounts (host_id, username)`).Error; err != nil {
		t.Fatalf("create extra index: %v", err)
	}
	if err := db.Exec(`INSERT INTO hosts (id, name, address, port, status) VALUES ('web-01', 'web-01', '127.0.0.1', 22, 'active')`).Error; err != nil {
		t.Fatalf("seed host: %v", err)
	}
	// 一条活跃 + 一条软删（active_marker NULL），软删行必须原样保留
	for _, stmt := range []string{
		`INSERT INTO host_accounts (id, host_id, name, username, status, active_marker) VALUES ('web-01-admin', 'web-01', 'admin', 'admin', 'active', 1)`,
		`INSERT INTO host_accounts (id, host_id, name, username, status, active_marker) VALUES ('web-01-deleted', 'web-01', 'old', 'old', 'disabled', NULL)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("seed host account: %v", err)
		}
	}

	if err := MigrateHostAccountIDActiveIndex(db); err != nil {
		t.Fatalf("migrate host account id active index: %v", err)
	}
	if err := MigrateAuditUniqueIndexes(db); err != nil {
		t.Fatalf("migrate audit unique indexes: %v", err)
	}

	var ddl string
	if err := db.Raw(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'host_accounts'`).Scan(&ddl).Error; err != nil {
		t.Fatalf("read host_accounts ddl: %v", err)
	}
	if strings.Contains(strings.ToUpper(ddl), "PRIMARY KEY") {
		t.Fatalf("host_accounts still declares a primary key: %s", ddl)
	}

	indexes, err := db.Migrator().GetIndexes("host_accounts")
	if err != nil {
		t.Fatalf("get indexes: %v", err)
	}
	var compositeUnique *gorm.Index
	for _, idx := range indexes {
		if idx.Name() == "idx_host_accounts_id_active" {
			compositeUnique = &idx
			break
		}
	}
	if compositeUnique == nil {
		t.Fatal("idx_host_accounts_id_active not found after migration")
	}
	unique, _ := (*compositeUnique).Unique()
	if !unique {
		t.Fatal("idx_host_accounts_id_active is not unique")
	}
	if len((*compositeUnique).Columns()) != 2 ||
		!strings.EqualFold((*compositeUnique).Columns()[0], "id") ||
		!strings.EqualFold((*compositeUnique).Columns()[1], "active_marker") {
		t.Fatalf("idx_host_accounts_id_active columns = %v, want [id active_marker]", (*compositeUnique).Columns())
	}

	var total, tombstones int64
	if err := db.Table("host_accounts").Count(&total).Error; err != nil {
		t.Fatalf("count host accounts: %v", err)
	}
	if total != 2 {
		t.Fatalf("host_accounts count = %d, want 2", total)
	}
	if err := db.Table("host_accounts").Where("active_marker IS NULL").Count(&tombstones).Error; err != nil {
		t.Fatalf("count tombstones: %v", err)
	}
	if tombstones != 1 {
		t.Fatalf("tombstone count = %d, want 1", tombstones)
	}

	var extraIndexCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_host_accounts_host_username'`).Scan(&extraIndexCount).Error; err != nil {
		t.Fatalf("check extra index: %v", err)
	}
	if extraIndexCount != 1 {
		t.Fatal("idx_host_accounts_host_username was not preserved")
	}

	// 幂等：重复执行不报错
	if err := MigrateHostAccountIDActiveIndex(db); err != nil {
		t.Fatalf("second migrate call failed: %v", err)
	}
}

func TestMigrateHostAccountIDActiveAllowsReinsertingTombstoneID(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`CREATE TABLE "hosts" (
		"id" text PRIMARY KEY,
		"name" text NOT NULL,
		"address" text NOT NULL,
		"port" integer NOT NULL,
		"protocol" text NOT NULL DEFAULT "ssh",
		"status" text NOT NULL DEFAULT "active",
		"active_marker" integer DEFAULT 1
	)`).Error; err != nil {
		t.Fatalf("create hosts table: %v", err)
	}
	if err := db.Exec(legacyHostAccountsDDL).Error; err != nil {
		t.Fatalf("create legacy host_accounts table: %v", err)
	}
	if err := db.Exec(`INSERT INTO hosts (id, name, address, port, status) VALUES ('web-01', 'web-01', '127.0.0.1', 22, 'active')`).Error; err != nil {
		t.Fatalf("seed host: %v", err)
	}
	if err := db.Exec(`INSERT INTO host_accounts (id, host_id, name, username, status, active_marker) VALUES ('web-01-admin', 'web-01', 'admin', 'admin', 'active', 1)`).Error; err != nil {
		t.Fatalf("seed host account: %v", err)
	}
	// 模拟软删除：active_marker 置 NULL
	if err := db.Exec(`UPDATE host_accounts SET active_marker = NULL, status = 'disabled' WHERE id = 'web-01-admin'`).Error; err != nil {
		t.Fatalf("soft delete host account: %v", err)
	}

	if err := MigrateHostAccountIDActiveIndex(db); err != nil {
		t.Fatalf("migrate host account id active index: %v", err)
	}
	if err := db.Exec(`INSERT INTO host_accounts (id, host_id, name, username, status, active_marker) VALUES ('web-01-admin', 'web-01', 'admin', 'admin', 'active', 1)`).Error; err != nil {
		t.Fatalf("reinsert same id after migration: %v", err)
	}
	var activeCount int64
	if err := db.Table("host_accounts").Where("id = ? AND active_marker = 1", "web-01-admin").Count(&activeCount).Error; err != nil {
		t.Fatalf("count active row: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active count = %d, want 1", activeCount)
	}
}
