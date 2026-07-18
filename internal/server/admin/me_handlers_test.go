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
	server := &Server{}
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
	wantPages := []rbac.PageAccess{{Key: "settings", Path: "/settings", Order: 90}}
	if len(response.Actions) != 0 || !reflect.DeepEqual(response.Pages, wantPages) {
		t.Fatalf("access = %#v, want settings-only access", response)
	}
}

func TestMeAccessContextReturnsAllPagesForSuperAdmin(t *testing.T) {
	server := &Server{}
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
	if !reflect.DeepEqual(response.Pages, appendSettingsPage(rbac.AccessiblePages([]string{"*"}))) {
		t.Fatalf("pages = %#v, want complete page catalog", response.Pages)
	}
}
