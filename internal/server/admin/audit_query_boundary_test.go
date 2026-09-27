package admin

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAuditQueryHandlersDoNotReadAuditRepositoryDirectly(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source location")
	}
	for _, name := range []string{"audit_handlers.go", "audit_management_handlers.go"} {
		contents, err := os.ReadFile(filepath.Join(filepath.Dir(currentFile), name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if bytes.Contains(contents, []byte("s.audit.")) {
			t.Fatalf("%s directly reads the audit repository", name)
		}
	}
}

func TestWriteAuditQueryErrorDoesNotExposeDependencyDetail(t *testing.T) {
	server := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	request := httptest.NewRequest(http.MethodGet, "/api/audit/ssh", nil)
	recorder := httptest.NewRecorder()
	server.writeAuditQueryError(recorder, request, errors.New("repository file C:\\private\\recording.cast unavailable"))
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "private") {
		t.Fatalf("unsafe error response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
