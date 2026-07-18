package appproxy

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

type fakeTokenAuthenticator struct {
	subject service.IdentitySubject
	found   bool
	err     error
	calls   int
	ctx     context.Context
	userID  string
}

func (f *fakeTokenAuthenticator) FindIdentitySubject(
	ctx context.Context,
	userID string,
) (service.IdentitySubject, bool, error) {
	f.calls++
	f.ctx = ctx
	f.userID = userID
	return f.subject, f.found, f.err
}

type fakeBrowserSessions struct {
	subject service.BrowserSessionSubject
	found   bool
	err     error
}

func (f *fakeBrowserSessions) Authenticate(_ context.Context, _ string) (service.BrowserSessionSubject, bool, error) {
	return f.subject, f.found, f.err
}

func activeFakeBrowserSession() *fakeBrowserSessions {
	return &fakeBrowserSessions{subject: service.BrowserSessionSubject{SessionID: "s1", UserID: "canonical-user"}, found: true}
}

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

func TestApplicationMiddlewareAuthenticatesOnceAndAuthorizesCanonicalUser(t *testing.T) {
	authenticator := &fakeTokenAuthenticator{
		subject: service.IdentitySubject{ID: "canonical-user", Status: "active"},
		found:   true,
	}
	authorizer := &fakeConnectionAuthorizer{
		decision: service.AuthorizationDecision{Allowed: true},
	}
	s := &Server{authenticator: authenticator, sessions: activeFakeBrowserSession(), authorizer: authorizer}
	app := model.Application{ID: "app1"}
	nextCalled := false
	handler := applicationMiddleware(s, app, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/app", nil)
	req.AddCookie(&http.Cookie{Name: "jianmen_session", Value: "session"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent || !nextCalled {
		t.Fatalf("status = %d, nextCalled = %v", recorder.Code, nextCalled)
	}
	if authenticator.calls != 1 {
		t.Fatalf("authenticator calls = %d, want 1", authenticator.calls)
	}
	if authenticator.userID != "canonical-user" {
		t.Fatalf("identity lookup user id = %q", authenticator.userID)
	}
	if authorizer.calls != 1 || authorizer.request.UserID != "canonical-user" {
		t.Fatalf("authorizer calls = %d, request = %#v", authorizer.calls, authorizer.request)
	}
	assertApplicationAuthorizationRequest(t, authorizer.request, "canonical-user")
}

func TestApplicationMiddlewareRejectsAuthenticationFailures(t *testing.T) {
	authenticationErr := errors.New("identity store unavailable")
	tests := []struct {
		name          string
		token         string
		authenticator tokenAuthenticator
	}{
		{name: "missing token"},
		{name: "nil authenticator", token: "token"},
		{
			name:          "unknown token",
			token:         "token",
			authenticator: &fakeTokenAuthenticator{},
		},
		{
			name:  "authentication error",
			token: "token",
			authenticator: &fakeTokenAuthenticator{
				err: authenticationErr,
			},
		},
		{
			name:  "empty canonical identity",
			token: "token",
			authenticator: &fakeTokenAuthenticator{
				subject: service.IdentitySubject{Status: "active"},
				found:   true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authorizer := &fakeConnectionAuthorizer{
				decision: service.AuthorizationDecision{Allowed: true},
			}
			s := &Server{authenticator: tt.authenticator, sessions: activeFakeBrowserSession(), authorizer: authorizer}
			nextCalled := false
			handler := applicationMiddleware(s, model.Application{ID: "app1"}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				nextCalled = true
			}))
			req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/app", nil)
			if tt.token != "" {
				req.AddCookie(&http.Cookie{Name: "jianmen_session", Value: tt.token})
			}
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
			if nextCalled {
				t.Fatal("downstream handler was called")
			}
			if authorizer.calls != 0 {
				t.Fatalf("authorizer calls = %d, want 0", authorizer.calls)
			}
		})
	}
}

func TestApplicationMiddlewareRejectsAuthorizationFailures(t *testing.T) {
	authorizationErr := errors.New("authorization backend unavailable")
	tests := []struct {
		name       string
		authorizer connectionAuthorizer
		cancel     bool
	}{
		{
			name: "deny",
			authorizer: &fakeConnectionAuthorizer{
				decision: service.AuthorizationDecision{Reason: service.AuthorizationReasonActionDenied},
			},
		},
		{
			name:       "error",
			authorizer: &fakeConnectionAuthorizer{err: authorizationErr},
		},
		{
			name: "nil authorizer",
		},
		{
			name:       "cancelled request",
			authorizer: &fakeConnectionAuthorizer{err: context.Canceled},
			cancel:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authenticator := &fakeTokenAuthenticator{
				subject: service.IdentitySubject{ID: "u1", Status: "active"},
				found:   true,
			}
			s := &Server{authenticator: authenticator, sessions: activeFakeBrowserSession(), authorizer: tt.authorizer}
			nextCalled := false
			handler := applicationMiddleware(s, model.Application{ID: "app1"}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				nextCalled = true
			}))
			ctx := context.Background()
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1/app", nil)
			req.AddCookie(&http.Cookie{Name: "jianmen_session", Value: "session"})
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
			}
			if nextCalled {
				t.Fatal("downstream handler was called")
			}
			if authenticator.calls != 1 {
				t.Fatalf("authenticator calls = %d, want 1", authenticator.calls)
			}
			if tt.authorizer != nil {
				fake := tt.authorizer.(*fakeConnectionAuthorizer)
				if fake.calls != 1 {
					t.Fatalf("authorizer calls = %d, want 1", fake.calls)
				}
			}
			if tt.cancel {
				fake := tt.authorizer.(*fakeConnectionAuthorizer)
				if !errors.Is(fake.ctx.Err(), context.Canceled) {
					t.Fatalf("authorizer context error = %v, want context canceled", fake.ctx.Err())
				}
			}
		})
	}
}

func TestRBACMiddlewareRejectsMissingAuthenticatedIdentity(t *testing.T) {
	s := &Server{
		authorizer: &fakeConnectionAuthorizer{
			decision: service.AuthorizationDecision{Allowed: true},
		},
	}
	nextCalled := false
	handler := s.rbacMiddleware(model.Application{ID: "app1"}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/app", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if nextCalled {
		t.Fatal("downstream handler was called")
	}
}

func TestAuthorizeAppPropagatesAuthorizerError(t *testing.T) {
	wantErr := errors.New("authorization backend unavailable")
	s := &Server{authorizer: &fakeConnectionAuthorizer{err: wantErr}}

	err := s.authorizeApp(context.Background(), "u1", "app1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("authorizeApp error = %v, want wrapped %v", err, wantErr)
	}
}

func TestNewWithNilDependenciesFailsClosed(t *testing.T) {
	s := New(config.ApplicationGatewayConfig{}, config.AdminConfig{}, nil, nil, nil, nil, slog.Default())
	nextCalled := false
	handler := applicationMiddleware(s, model.Application{ID: "app1"}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/app", nil)
	req.AddCookie(&http.Cookie{Name: "jianmen_session", Value: "session"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized || nextCalled {
		t.Fatalf("status = %d, nextCalled = %v", recorder.Code, nextCalled)
	}
}

func applicationMiddleware(s *Server, app model.Application, next http.Handler) http.Handler {
	return s.authMiddleware(app, s.rbacMiddleware(app, next))
}

func assertApplicationAuthorizationRequest(t *testing.T, request service.AuthorizationRequest, wantUserID string) {
	t.Helper()
	if request.UserID != wantUserID {
		t.Fatalf("user ID = %q, want %q", request.UserID, wantUserID)
	}
	if len(request.Actions) != 1 || request.Actions[0] != rbac.ActionAppConnect {
		t.Fatalf("actions = %#v, want [%q]", request.Actions, rbac.ActionAppConnect)
	}
	if request.ResourceType != model.ResourceTypeApplication || request.ResourceID != "app1" {
		t.Fatalf("resource = %q/%q", request.ResourceType, request.ResourceID)
	}
}
