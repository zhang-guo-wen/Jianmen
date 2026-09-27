package admin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jianmen/internal/store"
)

func TestReferencedDatabaseProvisioningConflictIsSafe(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("wrapped operation password=secret: %w", store.ErrReferencedDatabaseAdministrator),
		fmt.Errorf("wrapped SQL=DELETE: %w", store.ErrReferencedDatabaseInstance),
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodDelete, "/api/db/instances/one", nil)
		writeDBStoreError(recorder, request, err)
		if recorder.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
		}
		if strings.Contains(recorder.Body.String(), "secret") || strings.Contains(recorder.Body.String(), "DELETE") ||
			!strings.Contains(recorder.Body.String(), "referenced by provisioning operation") {
			t.Fatalf("unsafe conflict response: %s", recorder.Body.String())
		}
	}
}
