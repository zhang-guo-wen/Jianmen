package admin

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/storage"
	"jianmen/internal/store"
)

func TestRecordOperationPersistsMutationMetadata(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	s := &Server{
		cfg:    &config.Config{},
		db:     db,
		audit:  store.NewDBStore(db),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	r := httptest.NewRequest("PUT", "/api/db/instances/instance-1", nil)
	r.RemoteAddr = "192.0.2.10:54321"
	ctx := context.WithValue(r.Context(), ctxKeyUserID, "user-1")
	ctx = context.WithValue(ctx, ctxKeyUsername, "alice")
	s.recordOperation(r.WithContext(ctx), 200)

	var event model.AuditEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load audit event: %v", err)
	}
	if event.ActorID != "user-1" || event.Action != "update" || event.ResourceType != "db/instances" || event.ResourceID != "instance-1" {
		t.Fatalf("event = %+v", event)
	}
	if event.ClientIP != "192.0.2.10" || event.Detail == "" {
		t.Fatalf("event metadata = %+v", event)
	}
}
