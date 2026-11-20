package storage

import (
	"database/sql"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestAuditBatchMigrationPreservesOldRows(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	sqldb, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqldb.Close()
	if err := db.AutoMigrate(&SchemaMigration{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE audit_sessions (id varchar(64) PRIMARY KEY, started_at datetime, username varchar(128))").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO audit_sessions (id, started_at, username) VALUES (?, ?, ?)", "legacy", time.Now(), "原操作者").Error; err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.Version != "202611190001" {
			if err := db.Create(&SchemaMigration{Version: m.Version, Name: m.Name, AppliedAt: time.Now()}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := 0; i < 2; i++ {
		if err := Migrate(db); err != nil {
			t.Fatal(err)
		}
	}
	if !db.Migrator().HasColumn(&model.AuditSession{}, "batch_id") || !db.Migrator().HasIndex(&model.AuditSession{}, "idx_audit_sessions_batch_started") {
		t.Fatal("expected additive batch column and index")
	}
	var name string
	var batch sql.NullString
	if err := db.Raw("SELECT username, batch_id FROM audit_sessions WHERE id = ?", "legacy").Row().Scan(&name, &batch); err != nil {
		t.Fatal(err)
	}
	if name != "原操作者" || batch.Valid {
		t.Fatalf("old row was rewritten: %q, %+v", name, batch)
	}
}
