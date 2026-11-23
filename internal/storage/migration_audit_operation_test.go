package storage

import (
	"testing"

	"jianmen/internal/model"
)

// 新列必须是纯增量：历史行保持空值，重复执行不报错。
func TestAuditOperationMigrationAddsColumnsWithoutRewritingRows(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	sqldb, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqldb.Close()
	if err := db.Exec(`CREATE TABLE audit_events (
		id varchar(64) PRIMARY KEY,
		action varchar(64),
		resource_type varchar(64),
		username varchar(128)
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(
		`INSERT INTO audit_events (id, action, resource_type, username) VALUES (?, ?, ?, ?)`,
		"legacy", "create", "host_account", "原操作者",
	).Error; err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err := migrateAuditOperationFields(db); err != nil {
			t.Fatalf("migrate attempt %d: %v", i+1, err)
		}
	}

	event := &model.AuditEvent{}
	for _, column := range []string{"Operation", "ActorChannel", "TargetLabel"} {
		if !db.Migrator().HasColumn(event, column) {
			t.Fatalf("missing column %s", column)
		}
	}
	if !db.Migrator().HasIndex(event, "idx_audit_events_operation") {
		t.Fatal("missing operation index")
	}
	if !db.Migrator().HasIndex(event, "idx_audit_events_channel") {
		t.Fatal("missing actor channel index")
	}

	var row struct {
		Username     string
		Operation    string
		ActorChannel string
		TargetLabel  string
	}
	if err := db.Raw(
		`SELECT username,
			COALESCE(operation, '') AS operation,
			COALESCE(actor_channel, '') AS actor_channel,
			COALESCE(target_label, '') AS target_label
		 FROM audit_events WHERE id = ?`, "legacy",
	).Scan(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Username != "原操作者" {
		t.Fatalf("historical row rewritten: %+v", row)
	}
	if row.Operation != "" || row.ActorChannel != "" || row.TargetLabel != "" {
		t.Fatalf("historical row should keep empty new columns: %+v", row)
	}
}

// 表还不存在时迁移必须直接跳过，不能建一半。
func TestAuditOperationMigrationSkipsMissingTable(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	sqldb, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqldb.Close()

	if err := migrateAuditOperationFields(db); err != nil {
		t.Fatalf("migrate on empty database: %v", err)
	}
	if db.Migrator().HasTable(&model.AuditEvent{}) {
		t.Fatal("migration should not create the audit table")
	}
}
