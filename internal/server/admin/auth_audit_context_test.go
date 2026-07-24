package admin

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/storage"
	"jianmen/internal/store"
)

func TestAuthMiddlewarePropagatesAuditUserToModelHooks(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	repository := store.NewDBStore(db)
	actor := model.User{
		ID: "middleware-actor", Username: "middleware-actor",
		Status: "active", IsSuperAdmin: true,
	}
	if err := db.Create(&actor).Error; err != nil {
		t.Fatalf("create actor: %v", err)
	}
	identity, err := service.NewIdentityService(repository)
	if err != nil {
		t.Fatalf("create identity service: %v", err)
	}
	browserSessions, err := service.NewBrowserSessionService(repository)
	if err != nil {
		t.Fatalf("create browser session service: %v", err)
	}
	session, err := browserSessions.Create(context.Background(), actor.ID)
	if err != nil {
		t.Fatalf("create browser session: %v", err)
	}
	server := &Server{
		identity:        identity,
		browserSessions: browserSessions,
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	var handlerErr error
	handler := server.withAuthAndUser(func(w http.ResponseWriter, r *http.Request) {
		if got := model.AuditUserIDFromContext(r.Context()); got != actor.ID {
			handlerErr = &auditContextMismatchError{got: got, want: actor.ID}
			http.Error(w, "missing audit identity", http.StatusInternalServerError)
			return
		}
		group := model.UserGroup{ID: "middleware-created", Name: "middleware-created"}
		if err := db.WithContext(r.Context()).Create(&group).Error; err != nil {
			handlerErr = err
			http.Error(w, "create failed", http.StatusInternalServerError)
			return
		}
		group.Description = "updated through authenticated request"
		if err := db.WithContext(r.Context()).Save(&group).Error; err != nil {
			handlerErr = err
			http.Error(w, "update failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/audit-context", nil)
	request.AddCookie(&http.Cookie{Name: "jianmen_session", Value: session.Secret})
	response := httptest.NewRecorder()
	handler(response, request)
	if handlerErr != nil {
		t.Fatalf("authenticated handler: %v", handlerErr)
	}
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
	}

	var stored model.UserGroup
	if err := db.First(&stored, "id = ?", "middleware-created").Error; err != nil {
		t.Fatalf("load audited model: %v", err)
	}
	if stored.CreatedBy != actor.ID || stored.UpdatedBy != actor.ID {
		t.Fatalf("audit fields = created_by:%q updated_by:%q, want %q", stored.CreatedBy, stored.UpdatedBy, actor.ID)
	}
}

type auditContextMismatchError struct {
	got  string
	want string
}

func (e *auditContextMismatchError) Error() string {
	return "audit context user mismatch: got " + e.got + ", want " + e.want
}
