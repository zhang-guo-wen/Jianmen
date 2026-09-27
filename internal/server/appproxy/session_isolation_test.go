package appproxy

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func TestApplicationProxyDoesNotExposeManagementSessionToUpstream(t *testing.T) {
	var upstreamCookie string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCookie = r.Header.Get("Cookie")
		http.SetCookie(w, &http.Cookie{Name: browserSessionCookieName, Value: "attacker-session", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "application_preference", Value: "dark", Path: "/"})
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	targetURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	protectManagementSession(proxy)

	server := &Server{
		sessions: activeFakeBrowserSession(),
		authenticator: &fakeTokenAuthenticator{
			subject: service.IdentitySubject{ID: "canonical-user", Status: "active"},
			found:   true,
		},
		authorizer: &fakeConnectionAuthorizer{
			decision: service.AuthorizationDecision{Allowed: true},
		},
	}
	app := model.Application{ID: "app-1"}
	handler := server.authMiddleware(app, server.rbacMiddleware(app, proxy))
	request := httptest.NewRequest(http.MethodGet, "http://proxy.example.test/", nil)
	request.AddCookie(&http.Cookie{Name: browserSessionCookieName, Value: "raw-management-secret"})
	request.AddCookie(&http.Cookie{Name: "application_preference", Value: "light"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if strings.Contains(upstreamCookie, browserSessionCookieName) || strings.Contains(upstreamCookie, "raw-management-secret") {
		t.Fatalf("upstream received management session cookie: %q", upstreamCookie)
	}
	if !strings.Contains(upstreamCookie, "application_preference=light") {
		t.Fatalf("application cookie was not preserved: %q", upstreamCookie)
	}
	for _, value := range response.Header().Values("Set-Cookie") {
		if setCookieName(value) == browserSessionCookieName {
			t.Fatalf("upstream overwrote management session cookie: %q", value)
		}
	}
	if got := response.Header().Values("Set-Cookie"); len(got) != 1 || setCookieName(got[0]) != "application_preference" {
		t.Fatalf("application response cookies = %#v, want only application_preference", got)
	}
}
