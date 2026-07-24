package admin

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
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

func TestOperationActionClassifiesDiagnosticsAsTest(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/system-settings/diagnostics/object-storage",
		nil,
	)
	if action := operationAction(request); action != "test" {
		t.Fatalf("operationAction() = %q, want test", action)
	}
}

func TestHostIdentityRefreshAuditRecordsOnlyFingerprintEvidence(t *testing.T) {
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
	handler := s.withOperationAudit(func(w http.ResponseWriter, r *http.Request) {
		setOperationAuditMetadata(r, map[string]string{
			"old_fingerprint":      "SHA256:old",
			"expected_fingerprint": "SHA256:expected",
			"actual_fingerprint":   "SHA256:actual",
		})
		w.WriteHeader(http.StatusConflict)
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/hosts/host-1/refresh-identity",
		nil,
	)
	ctx := context.WithValue(request.Context(), ctxKeyUserID, "operator")
	ctx = context.WithValue(ctx, ctxKeyUsername, "alice")
	response := httptest.NewRecorder()
	handler(response, request.WithContext(ctx))

	var events []model.AuditEvent
	if err := db.Order("created_at ASC").Find(&events).Error; err != nil {
		t.Fatalf("load audit events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("audit event count = %d, want 2", len(events))
	}
	result := events[1]
	if result.Action != "refresh" || result.ResourceType != "hosts" || result.ResourceID != "host-1" {
		t.Fatalf("refresh audit identity = %#v", result)
	}
	var detail map[string]any
	if err := json.Unmarshal([]byte(result.Detail), &detail); err != nil {
		t.Fatalf("decode audit detail: %v", err)
	}
	if detail["old_fingerprint"] != "SHA256:old" ||
		detail["expected_fingerprint"] != "SHA256:expected" ||
		detail["actual_fingerprint"] != "SHA256:actual" {
		t.Fatalf("refresh audit detail = %#v", detail)
	}
	if _, leaked := detail["known_hosts"]; leaked {
		t.Fatalf("refresh audit leaked known_hosts: %#v", detail)
	}
}
