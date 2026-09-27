package storage

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

func TestMigrateAuditEventPhaseBackfill(t *testing.T) {
	db := auditPhaseBackfillTestDB(t)

	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	seed := func(event *model.AuditEvent) {
		t.Helper()
		if err := db.Create(event).Error; err != nil {
			t.Fatalf("create audit event: %v", err)
		}
	}
	// Legacy intent: phase empty but detail JSON encodes it.
	seed(&model.AuditEvent{ID: "intent-1", ActorID: "u1", ActorUsername: "alice",
		Action: "update", ResourceType: "hosts", Detail: `{"phase":"intent","result":"pending"}`,
		CreatedAt: base})
	// Legacy result: phase empty, intent_id in detail.
	seed(&model.AuditEvent{ID: "result-1", ActorID: "u1", ActorUsername: "alice",
		Action: "update", ResourceType: "hosts", Detail: `{"phase":"result","result":"success","intent_id":"intent-1"}`,
		CreatedAt: base.Add(time.Second)})
	// Unclassifiable legacy row: phase empty, no phase in detail -> untouched.
	seed(&model.AuditEvent{ID: "opaque-1", ActorID: "u1", ActorUsername: "alice",
		Action: "delete", ResourceType: "hosts", Detail: `{"something":"else"}`,
		CreatedAt: base.Add(2 * time.Second)})
	// Modern row already carries phase -> untouched.
	seed(&model.AuditEvent{ID: "modern-1", ActorID: "u1", ActorUsername: "alice",
		Action: "update", ResourceType: "hosts", Phase: "intent", IntentID: "",
		Detail:    `{"phase":"intent"}`,
		CreatedAt: base.Add(3 * time.Second)})

	if err := migrateAuditEventPhaseBackfill(db); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	assertPhase := func(id, wantPhase, wantIntentID string) {
		t.Helper()
		var event model.AuditEvent
		if err := db.First(&event, "id = ?", id).Error; err != nil {
			t.Fatalf("load %s: %v", id, err)
		}
		if event.Phase != wantPhase {
			t.Fatalf("%s phase = %q, want %q", id, event.Phase, wantPhase)
		}
		if event.IntentID != wantIntentID {
			t.Fatalf("%s intent_id = %q, want %q", id, event.IntentID, wantIntentID)
		}
	}
	assertPhase("intent-1", "intent", "")
	assertPhase("result-1", "result", "intent-1")
	assertPhase("opaque-1", "", "")
	assertPhase("modern-1", "intent", "")
}

func TestMigrateAuditEventPhaseBackfillIsIdempotent(t *testing.T) {
	db := auditPhaseBackfillTestDB(t)

	if err := db.Create(&model.AuditEvent{ID: "intent-1", ActorID: "u1", ActorUsername: "alice",
		Action: "update", ResourceType: "hosts", Detail: `{"phase":"intent","result":"pending"}`,
		CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AuditEvent{ID: "result-1", ActorID: "u1", ActorUsername: "alice",
		Action: "update", ResourceType: "hosts", Detail: `{"phase":"result","result":"success","intent_id":"intent-1"}`,
		CreatedAt: time.Now().Add(time.Second)}).Error; err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err := migrateAuditEventPhaseBackfill(db); err != nil {
			t.Fatalf("backfill round %d: %v", i, err)
		}
	}

	var result model.AuditEvent
	if err := db.First(&result, "id = ?", "result-1").Error; err != nil {
		t.Fatal(err)
	}
	if result.Phase != "result" || result.IntentID != "intent-1" {
		t.Fatalf("result-1 = phase %q intent_id %q", result.Phase, result.IntentID)
	}
}

// auditPhaseBackfillTestDB opens an in-memory SQLite database with the schema
// migrations applied so the phase/intent_id columns exist before the backfill.
func auditPhaseBackfillTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}
