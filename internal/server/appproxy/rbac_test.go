package appproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

type fakeConnectionAuthorizer struct {
	decision service.AuthorizationDecision
	err      error
	calls    int
	ctx      context.Context
	request  service.AuthorizationRequest
}

func (f *fakeConnectionAuthorizer) Authorize(ctx context.Context, request service.AuthorizationRequest) (service.AuthorizationDecision, error) {
	f.calls++
	f.ctx = ctx
	f.request = request
	return f.decision, f.err
}

func TestAuthorizeAppUsesUnifiedAuthorizerForAllowAndDeny(t *testing.T) {
	tests := []struct {
		name    string
		allowed bool
	}{
		{name: "allow", allowed: true},
		{name: "deny", allowed: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authorizer := &fakeConnectionAuthorizer{
				decision: service.AuthorizationDecision{Allowed: tt.allowed, Reason: service.AuthorizationReasonAllowed},
			}
			s := &Server{authorizer: authorizer}

			err := s.authorizeApp(context.Background(), "u1", "app1")
			if (err == nil) != tt.allowed {
				t.Fatalf("authorizeApp error = %v, allowed = %v", err, tt.allowed)
			}
			if authorizer.calls != 1 {
				t.Fatalf("authorizer calls = %d, want 1", authorizer.calls)
			}
			assertApplicationAuthorizationRequest(t, authorizer.request)
		})
	}
}

func TestAuthorizeAppUsesUnifiedSuperAdminDecision(t *testing.T) {
	authorizer := &fakeConnectionAuthorizer{
		decision: service.AuthorizationDecision{
			Allowed: true,
			Reason:  service.AuthorizationReasonSuperAdmin,
		},
	}
	s := &Server{authorizer: authorizer}

	if err := s.authorizeApp(context.Background(), "admin", "app1"); err != nil {
		t.Fatalf("authorizeApp super admin: %v", err)
	}
	if authorizer.calls != 1 {
		t.Fatalf("authorizer calls = %d, want 1", authorizer.calls)
	}
	assertApplicationAuthorizationRequest(t, authorizer.request)
}

func TestAuthorizeAppPropagatesAuthorizerError(t *testing.T) {
	wantErr := errors.New("authorization backend unavailable")
	s := &Server{authorizer: &fakeConnectionAuthorizer{err: wantErr}}

	err := s.authorizeApp(context.Background(), "u1", "app1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("authorizeApp error = %v, want wrapped %v", err, wantErr)
	}
}

func TestAuthorizeAppPropagatesCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	authorizer := &fakeConnectionAuthorizer{
		decision: service.AuthorizationDecision{Allowed: true},
	}
	s := &Server{authorizer: authorizer}

	if err := s.authorizeApp(ctx, "u1", "app1"); err != nil {
		t.Fatalf("authorizeApp: %v", err)
	}
	if !errors.Is(authorizer.ctx.Err(), context.Canceled) {
		t.Fatalf("authorizer context error = %v, want context canceled", authorizer.ctx.Err())
	}
}

func TestRBACMiddlewarePassesRequestContextToAuthorizer(t *testing.T) {
	db := openAppProxyTestDB(t)
	token := "test-token"
	hash := sha256.Sum256([]byte(token))
	if err := db.Create(&model.User{
		ID:        "u1",
		Username:  "user",
		TokenHash: hex.EncodeToString(hash[:]),
		Status:    "active",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	authorizer := &fakeConnectionAuthorizer{decision: service.AuthorizationDecision{Allowed: true}}
	s := &Server{db: db, authorizer: authorizer}
	nextCalled := false
	handler := s.rbacMiddleware(model.Application{ID: "app1"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1/app", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent || !nextCalled {
		t.Fatalf("status = %d, nextCalled = %v", recorder.Code, nextCalled)
	}
	if !errors.Is(authorizer.ctx.Err(), context.Canceled) {
		t.Fatalf("authorizer context error = %v, want context canceled", authorizer.ctx.Err())
	}
}

func TestNewWithNilAuthorizerFailsClosed(t *testing.T) {
	s := New(config.ApplicationGatewayConfig{}, config.AdminConfig{}, nil, nil, slog.Default())
	if err := s.authorizeApp(context.Background(), "u1", "app1"); err == nil {
		t.Fatal("authorizeApp with nil authorizer succeeded")
	}
}

func assertApplicationAuthorizationRequest(t *testing.T, request service.AuthorizationRequest) {
	t.Helper()
	if request.UserID != "u1" && request.UserID != "admin" {
		t.Fatalf("user ID = %q", request.UserID)
	}
	if len(request.Actions) != 1 || request.Actions[0] != rbac.ActionAppConnect {
		t.Fatalf("actions = %#v, want [%q]", request.Actions, rbac.ActionAppConnect)
	}
	if request.ResourceType != model.ResourceTypeApplication || request.ResourceID != "app1" {
		t.Fatalf("resource = %q/%q", request.ResourceType, request.ResourceID)
	}
}

func openAppProxyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate user: %v", err)
	}
	return db
}
