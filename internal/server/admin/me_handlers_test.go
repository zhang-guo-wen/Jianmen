package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"jianmen/internal/rbac"
)

func TestMeAccessContextDefaultsToNoAccess(t *testing.T) {
	server := &Server{superAdminIDs: map[string]bool{}}
	req := httptest.NewRequest(http.MethodGet, "/api/me/access-context", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUserID, "regular-user"))
	rec := httptest.NewRecorder()

	server.handleMeAccessContext(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response meAccessContextResponse
	if err := decodeTestData(t, rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Actions) != 0 || len(response.Pages) != 0 {
		t.Fatalf("access = %#v, want empty access", response)
	}
}

func TestMeAccessContextReturnsAllPagesForSuperAdmin(t *testing.T) {
	server := &Server{superAdminIDs: map[string]bool{"u-admin": true}}
	req := asTestSuperAdmin(httptest.NewRequest(http.MethodGet, "/api/me/access-context", nil))
	rec := httptest.NewRecorder()

	server.handleMeAccessContext(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response meAccessContextResponse
	if err := decodeTestData(t, rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !reflect.DeepEqual(response.Actions, []string{"*"}) {
		t.Fatalf("actions = %#v, want wildcard", response.Actions)
	}
	if !reflect.DeepEqual(response.Pages, rbac.AccessiblePages([]string{"*"})) {
		t.Fatalf("pages = %#v, want complete page catalog", response.Pages)
	}
}
