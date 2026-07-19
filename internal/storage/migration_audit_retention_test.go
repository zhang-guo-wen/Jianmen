package storage

import (
	"testing"
	"time"

	"jianmen/internal/model"
)

const auditRetentionMigrationVersion = "202607190002"

func TestAuditRetentionMigrationAddsCleanupStateWithoutLosingSessions(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&SchemaMigration{}, &auditSessionBeforeRetention{}); err != nil {
		t.Fatalf("create previous audit schema: %v", err)
	}
	now := time.Now().UTC()
	if err := db.Create(&auditSessionBeforeRetention{
		ID: "legacy-session", State: "ended", StartedAt: now.Add(-time.Hour), EndedAt: &now,
		ReplayDir: "data/replay/ssh/legacy-session",
	}).Error; err != nil {
		t.Fatalf("seed legacy audit session: %v", err)
	}
	for _, migration := range migrations {
		if migration.Version == auditRetentionMigrationVersion {
			continue
		}
		if err := db.Create(&SchemaMigration{
			Version: migration.Version, Name: migration.Name, AppliedAt: now,
		}).Error; err != nil {
			t.Fatalf("record previous migration %s: %v", migration.Version, err)
		}
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate audit retention schema: %v", err)
	}
	for _, column := range []string{"cleanup_status", "cleanup_at", "cleanup_error"} {
		if !db.Migrator().HasColumn(&model.AuditSession{}, column) {
			t.Fatalf("audit retention column %s is missing", column)
		}
	}
	if !db.Migrator().HasIndex(&model.AuditSession{}, "idx_audit_sessions_cleanup") {
		t.Fatal("audit cleanup index is missing")
	}
	var session model.AuditSession
	if err := db.First(&session, "id = ?", "legacy-session").Error; err != nil {
		t.Fatalf("load migrated session: %v", err)
	}
	if session.CleanupStatus != "ready" || session.ReplayDir != "data/replay/ssh/legacy-session" {
		t.Fatalf("migrated session = %#v", session)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}
