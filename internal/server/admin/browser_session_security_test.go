package admin

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/service"

	"gorm.io/gorm"
)

type failCreateBrowserSessionRepository struct {
	service.BrowserSessionRepository
}

func (failCreateBrowserSessionRepository) CreateAdminSession(context.Context, model.AdminSession) error {
	return errors.New("injected browser session persistence failure")
}

func TestSecureRequestUsesTLSOrConfiguredPublicURL(t *testing.T) {
	plain := httptest.NewRequest(http.MethodGet, "http://admin.example.com", nil)
	plain.Header.Set("X-Forwarded-Proto", "https")
	if secureRequest(plain, "http://admin.example.com") {
		t.Fatal("untrusted X-Forwarded-Proto enabled a secure cookie")
	}
	if !secureRequest(plain, "https://admin.example.com") {
		t.Fatal("HTTPS public URL did not enable secure cookie")
	}
	plain.TLS = &tls.ConnectionState{}
	if !secureRequest(plain, "http://admin.example.com") {
		t.Fatal("TLS request did not enable secure cookie")
	}
}

func TestBrowserSessionCookiesUseConfiguredSecureFlag(t *testing.T) {
	server := &Server{cfg: &config.Config{Admin: config.AdminConfig{PublicURL: "https://admin.example.com"}}}
	req := httptest.NewRequest(http.MethodPost, "http://internal-admin/login", nil)
	set := httptest.NewRecorder()
	setBrowserSessionCookie(set, req, "secret", time.Now().Add(time.Hour), server.cfg.Admin.PublicURL)
	if !strings.Contains(set.Header().Get("Set-Cookie"), "; Secure") {
		t.Fatal("set cookie did not use Secure for HTTPS public URL")
	}
	clear := httptest.NewRecorder()
	clearBrowserSessionCookie(clear, req, server.cfg.Admin.PublicURL)
	if !strings.Contains(clear.Header().Get("Set-Cookie"), "; Secure") {
		t.Fatal("clear cookie did not preserve Secure flag")
	}
}

func TestBrowserSessionMiddlewareEnforcesCSRFAndLogoutRevokesSession(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	user := model.User{ID: "browser-user", Username: "browser-user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create browser user: %v", err)
	}
	session, err := server.browserSessions.Create(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("create browser session: %v", err)
	}

	called := 0
	protected := server.withAuthAndUser(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	})
	for _, test := range []struct {
		name       string
		method     string
		csrf       string
		wantStatus int
		wantCalled int
	}{
		{name: "GET needs no CSRF", method: http.MethodGet, wantStatus: http.StatusNoContent, wantCalled: 1},
		{name: "POST rejects missing CSRF", method: http.MethodPost, wantStatus: http.StatusForbidden, wantCalled: 1},
		{name: "POST rejects wrong CSRF", method: http.MethodPost, csrf: "wrong", wantStatus: http.StatusForbidden, wantCalled: 1},
		{name: "POST accepts matching CSRF", method: http.MethodPost, csrf: session.CSRFToken, wantStatus: http.StatusNoContent, wantCalled: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "/api/protected", nil)
			request.AddCookie(&http.Cookie{Name: "jianmen_session", Value: session.Secret})
			if test.csrf != "" {
				request.Header.Set("X-CSRF-Token", test.csrf)
			}
			response := httptest.NewRecorder()
			protected(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if called != test.wantCalled {
				t.Fatalf("protected handler calls = %d, want %d", called, test.wantCalled)
			}
		})
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	logoutRequest.AddCookie(&http.Cookie{Name: "jianmen_session", Value: session.Secret})
	logoutRequest.Header.Set("X-CSRF-Token", session.CSRFToken)
	logoutResponse := httptest.NewRecorder()
	server.withAuthAndUser(server.handleLogout)(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d; body=%s", logoutResponse.Code, http.StatusNoContent, logoutResponse.Body.String())
	}
	if _, found, err := server.browserSessions.Authenticate(context.Background(), session.Secret); err != nil || found {
		t.Fatalf("revoked session authenticate = found=%v err=%v", found, err)
	}
	cookies := logoutResponse.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "jianmen_session" || cookies[0].MaxAge >= 0 {
		t.Fatalf("logout cookies = %#v, want expired jianmen_session", cookies)
	}
}

func TestSetupSessionInsertFailureRollsBackAndAllowsRetry(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	callbackName := "test:fail_initial_admin_session_create"
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "AdminSession" {
			tx.AddError(errors.New("injected setup session persistence failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}

	setupRequest := httptest.NewRequest(http.MethodPost, "/api/init/setup", strings.NewReader(
		`{"username":"recovery-admin","password":"Recovery-Password-123!","email":"admin@example.com"}`,
	))
	setupResponse := httptest.NewRecorder()
	server.handleInitSetup(setupResponse, setupRequest)
	if setupResponse.Code != http.StatusInternalServerError {
		t.Fatalf("setup status = %d, want %d; body=%s", setupResponse.Code, http.StatusInternalServerError, setupResponse.Body.String())
	}

	for _, check := range []struct {
		name       string
		modelValue any
	}{
		{name: "users", modelValue: &model.User{}},
		{name: "setup guards", modelValue: &model.SystemInitialization{}},
		{name: "sessions", modelValue: &model.AdminSession{}},
	} {
		var count int64
		if err := db.Model(check.modelValue).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s after failed setup = %d, want 0", check.name, count)
		}
	}

	if err := db.Callback().Create().Remove(callbackName); err != nil {
		t.Fatal(err)
	}
	retryRequest := httptest.NewRequest(http.MethodPost, "/api/init/setup", strings.NewReader(
		`{"username":"recovery-admin","password":"Recovery-Password-123!","email":"admin@example.com"}`,
	))
	retryResponse := httptest.NewRecorder()
	server.handleInitSetup(retryResponse, retryRequest)
	if retryResponse.Code != http.StatusCreated {
		t.Fatalf("retry setup status = %d, want %d; body=%s", retryResponse.Code, http.StatusCreated, retryResponse.Body.String())
	}
	if len(retryResponse.Result().Cookies()) != 1 || retryResponse.Result().Cookies()[0].Name != "jianmen_session" {
		t.Fatalf("retry setup cookies = %#v", retryResponse.Result().Cookies())
	}
	if _, found, err := server.browserSessions.Authenticate(
		context.Background(),
		retryResponse.Result().Cookies()[0].Value,
	); err != nil || !found {
		t.Fatalf("atomic setup session authenticate = found=%v err=%v", found, err)
	}
	for _, check := range []struct {
		name       string
		modelValue any
	}{
		{name: "users", modelValue: &model.User{}},
		{name: "setup guards", modelValue: &model.SystemInitialization{}},
		{name: "sessions", modelValue: &model.AdminSession{}},
	} {
		var count int64
		if err := db.Model(check.modelValue).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s after retry = %d, want 1", check.name, count)
		}
	}
}
