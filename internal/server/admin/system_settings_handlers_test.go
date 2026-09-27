package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSystemSettingsRequireSuperAdmin(t *testing.T) {
	server := &Server{}
	called := false
	handler := server.withSuperAdmin(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/api/system-settings", nil)
	request = request.WithContext(context.WithValue(request.Context(), ctxKeyUserID, "regular-user"))
	recorder := httptest.NewRecorder()

	handler(recorder, request)

	if recorder.Code != http.StatusForbidden || called {
		t.Fatalf("status = %d, called = %v", recorder.Code, called)
	}
}

func TestSystemSettingsAllowSuperAdmin(t *testing.T) {
	server := &Server{}
	handler := server.withSuperAdmin(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	request := asTestSuperAdmin(httptest.NewRequest(http.MethodGet, "/api/system-settings", nil))
	recorder := httptest.NewRecorder()

	handler(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}
