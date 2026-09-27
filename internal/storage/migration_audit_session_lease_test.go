package storage

import (
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestAuditSessionLeaseMigrationIsVersionedAndDoesNotGuessLegacyActivity(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&SchemaMigration{}, &auditSessionWebRDPSchema{}); err != nil {
		t.Fatalf("create pre-lease schema: %v", err)
	}
	startedAt := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	if err := db.Create(&auditSessionWebRDPSchema{
		ID: "legacy-started", State: "started", StartedAt: startedAt,
		CleanupStatus: "ready",
	}).Error; err != nil {
		t.Fatalf("seed legacy audit session: %v", err)
	}
	if db.Migrator().HasColumn(&model.AuditSession{}, "lease_owner") {
		t.Fatal("pre-lease schema unexpectedly contains lease columns")
	}

	if err := migrateAuditSessionLease(db); err != nil {
		t.Fatalf("migrate audit session leases: %v", err)
	}
	for _, column := range []string{"lease_owner", "heartbeat_at", "lease_expires_at"} {
		if !db.Migrator().HasColumn(&model.AuditSession{}, column) {
			t.Fatalf("audit lease column %s is missing", column)
		}
	}
	for _, index := range []string{
		"idx_audit_sessions_lease_owner_state",
		"idx_audit_sessions_lease_expiry",
	} {
		if !db.Migrator().HasIndex(&model.AuditSession{}, index) {
			t.Fatalf("audit lease index %s is missing", index)
		}
	}
	var legacy model.AuditSession
	if err := db.First(&legacy, "id = ?", "legacy-started").Error; err != nil {
		t.Fatalf("load legacy audit session: %v", err)
	}
	if legacy.LeaseOwner != "" || legacy.HeartbeatAt != nil || legacy.LeaseExpiresAt != nil {
		t.Fatalf("migration guessed legacy activity: %#v", legacy)
	}
}
