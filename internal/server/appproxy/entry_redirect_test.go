package appproxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
)

func TestEntryRedirectMiddlewareRedirectsRootNavigation(t *testing.T) {
	s := &Server{}
	app := model.Application{EntryPath: "/nacos/#/login?namespace="}
	nextCalled := false
	handler := s.entryRedirectMiddleware(app, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:47110/", nil)
	req.Header.Set("Accept", "text/html")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusFound)
	}
	if got, want := recorder.Header().Get("Location"), "/nacos/#/login?namespace="; got != want {
		t.Fatalf("location = %q, want %q", got, want)
	}
	if nextCalled {
		t.Fatal("next handler was called")
	}
}

func TestEntryRedirectMiddlewareLeavesNonNavigationRequestUnchanged(t *testing.T) {
	s := &Server{}
	app := model.Application{EntryPath: "/nacos/"}
	nextCalled := false
	handler := s.entryRedirectMiddleware(app, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:47110/", nil)
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent || !nextCalled {
		t.Fatalf("status = %d, nextCalled = %v", recorder.Code, nextCalled)
	}
}

func TestLoginRedirectRestoresConfiguredEntryFragment(t *testing.T) {
	s := &Server{adminCfg: config.AdminConfig{
		ListenAddr: "127.0.0.1:47100",
		PublicURL:  "http://127.0.0.1:47101",
	}}
	app := model.Application{EntryPath: "/nacos/#/login?namespace=&pageSize="}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:47110/nacos/", nil)

	location, err := url.Parse(s.loginRedirectURLForApp(req, app))
	if err != nil {
		t.Fatalf("parse login redirect: %v", err)
	}
	if got, want := location.Query().Get("redirect"), "http://127.0.0.1:47110/nacos/#/login?namespace=&pageSize="; got != want {
		t.Fatalf("return URL = %q, want %q", got, want)
	}
}
