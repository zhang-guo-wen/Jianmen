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

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/store"
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
	var got store.TargetView
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal get response: %v", err)
	}
	if got.ID != "runtime-a" {
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
	var updated store.TargetView
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

func TestHandleHostsPaginationAndLazyAccounts(t *testing.T) {
	server := newTargetTestServer(t)
	for _, body := range []string{
		`{
			"id": "prod-a",
			"name": "Production A",
			"group": "prod",
			"address": "10.0.0.10",
			"port": 2201,
			"remark": "primary host"
		}`,
		`{
			"id": "prod-b",
			"name": "Production B",
			"group": "prod",
			"address": "10.0.0.11",
			"port": 2202
		}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/hosts", bytes.NewBufferString(body))
		rec := httptest.NewRecorder()
		server.handleHosts(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create host status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
		}
	}

	createAccountReq := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewBufferString(`{
		"id": "prod-root",
		"host_id": "prod-a",
		"name": "Root account",
		"group": "ops",
		"remark": "break glass",
		"username": "root",
		"password": "secret",
		"insecure_ignore_host_key": true
	}`))
	createAccountRec := httptest.NewRecorder()
	server.handleTargets(createAccountRec, createAccountReq)
	if createAccountRec.Code != http.StatusCreated {
		t.Fatalf("create account status = %d, want %d; body=%s", createAccountRec.Code, http.StatusCreated, createAccountRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/hosts?page=1&page_size=1&q=prod", nil)
	listRec := httptest.NewRecorder()
	server.handleHosts(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list hosts status = %d, want %d; body=%s", listRec.Code, http.StatusOK, listRec.Body.String())
	}
	var page pagedHostList
	if err := json.Unmarshal(listRec.Body.Bytes(), &page); err != nil {
		t.Fatalf("unmarshal hosts page: %v; body=%s", err, listRec.Body.String())
	}
	if page.Total != 2 || page.Page != 1 || page.PageSize != 1 || len(page.Data) != 1 {
		t.Fatalf("unexpected hosts page: %#v", page)
	}
	if page.Data[0].ID != "prod-a" || page.Data[0].AccountCount != 1 || page.Data[0].Group != "prod" || page.Data[0].Remark != "primary host" {
		t.Fatalf("unexpected first host page item: %#v", page.Data[0])
	}

	accountsReq := httptest.NewRequest(http.MethodGet, "/api/hosts/prod-a/accounts", nil)
	accountsRec := httptest.NewRecorder()
	server.handleHost(accountsRec, accountsReq)
	if accountsRec.Code != http.StatusOK {
		t.Fatalf("host accounts status = %d, want %d; body=%s", accountsRec.Code, http.StatusOK, accountsRec.Body.String())
	}
	var accounts []store.TargetView
	if err := json.Unmarshal(accountsRec.Body.Bytes(), &accounts); err != nil {
		t.Fatalf("unmarshal host accounts: %v; body=%s", err, accountsRec.Body.String())
	}
	if len(accounts) != 1 {
		t.Fatalf("account count = %d, want 1: %#v", len(accounts), accounts)
	}
	account := accounts[0]
	if account.ID != "prod-root" || account.HostID != "prod-a" || account.Host != "10.0.0.10" || account.Port != 2201 {
		t.Fatalf("unexpected account host identity: %#v", account)
	}
	if account.Group != "ops" || account.Remark != "break glass" || account.ResourceType != model.ResourceTypeHostAccount {
		t.Fatalf("unexpected account metadata: %#v", account)
	}
	if strings.Contains(accountsRec.Body.String(), "secret") {
		t.Fatalf("host accounts response leaked secret: %s", accountsRec.Body.String())
	}
}

func newTargetTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{
		TargetsFile: t.TempDir() + "/targets.json",
		Admin: config.AdminConfig{},
		Users: []config.User{
			{
				ID:       "u-admin",
				Username: "admin",
				Password: "admin",
			},
		},
	}
	adapter, err := store.NewStaticAdapter(cfg, nil)
	if err != nil {
		t.Fatalf("NewStaticStore returned error: %v", err)
	}
	return &Server{
		cfg:    cfg,
		store:  adapter,
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
