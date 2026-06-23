package admin

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jianmen/internal/access"
	"jianmen/internal/config"
)

func TestHandleIndexReturnsAPIOnlyInfo(t *testing.T) {
	server := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	server.handleIndex(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "api-only") || !strings.Contains(body, "http://127.0.0.1:47101") {
		t.Fatalf("body missing API-only frontend info: %s", body)
	}
	if strings.Contains(body, "<html") {
		t.Fatalf("body still contains HTML: %s", body)
	}
}

func TestHandleTargetCRUD(t *testing.T) {
	server := newTargetTestServer(t)

	createBody := `{
		"id": "runtime-a",
		"name": "runtime-a",
		"host": "127.0.0.2",
		"port": 22,
		"username": "root",
		"password": "secret",
		"private_key_pem": "hidden",
		"passphrase": "hidden-passphrase",
		"insecure_ignore_host_key": true
	}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewBufferString(createBody))
	createRec := httptest.NewRecorder()
	server.handleTargets(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body=%s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}
	assertTargetResponseHasNoSecrets(t, createRec.Body.Bytes())

	getReq := httptest.NewRequest(http.MethodGet, "/api/targets/runtime-a", nil)
	getRec := httptest.NewRecorder()
	server.handleTarget(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body=%s", getRec.Code, http.StatusOK, getRec.Body.String())
	}
	var got access.TargetView
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal get response: %v", err)
	}
	if got.ID != "runtime-a" || got.Static {
		t.Fatalf("unexpected get target view: %#v", got)
	}
	assertTargetResponseHasNoSecrets(t, getRec.Body.Bytes())

	updateBody := `{
		"id": "runtime-a",
		"name": "updated runtime",
		"host": "10.0.0.2",
		"port": 2200,
		"username": "ubuntu",
		"insecure_ignore_host_key": true
	}`
	updateReq := httptest.NewRequest(http.MethodPut, "/api/targets/runtime-a", bytes.NewBufferString(updateBody))
	updateRec := httptest.NewRecorder()
	server.handleTarget(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d; body=%s", updateRec.Code, http.StatusOK, updateRec.Body.String())
	}
	var updated access.TargetView
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("unmarshal update response: %v", err)
	}
	if updated.Name != "updated runtime" || updated.Host != "10.0.0.2" || updated.Port != 2200 || updated.Username != "ubuntu" {
		t.Fatalf("unexpected updated target view: %#v", updated)
	}
	assertTargetResponseHasNoSecrets(t, updateRec.Body.Bytes())

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/targets/runtime-a", nil)
	deleteRec := httptest.NewRecorder()
	server.handleTarget(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d; body=%s", deleteRec.Code, http.StatusNoContent, deleteRec.Body.String())
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/api/targets/runtime-a", nil)
	missingRec := httptest.NewRecorder()
	server.handleTarget(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want %d; body=%s", missingRec.Code, http.StatusNotFound, missingRec.Body.String())
	}
}

func TestHandleDeleteStaticTargetRejected(t *testing.T) {
	server := newTargetTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/targets/target-a", nil)
	rec := httptest.NewRecorder()

	server.handleTarget(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func newTargetTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{
		TargetsFile: t.TempDir() + "/targets.json",
		Admin: config.AdminConfig{
			Token: "",
		},
		Users: []config.User{
			{
				ID:       "u-admin",
				Username: "admin",
				Password: "admin",
			},
		},
		Targets: []config.Target{
			{
				ID:       "target-a",
				Name:     "target-a",
				Host:     "127.0.0.1",
				Port:     22,
				Username: "root",
				Password: "password",
			},
		},
		DefaultTarget: "target-a",
	}
	store, err := access.NewStaticStore(cfg)
	if err != nil {
		t.Fatalf("NewStaticStore returned error: %v", err)
	}
	return &Server{
		cfg:    cfg,
		store:  store,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func assertTargetResponseHasNoSecrets(t *testing.T, raw []byte) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	for _, key := range []string{"password", "private_key_pem", "passphrase"} {
		if _, ok := body[key]; ok {
			t.Fatalf("response leaked %q: %s", key, string(raw))
		}
	}
}
