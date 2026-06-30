package admin

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/crypto"
	"jianmen/internal/model"
	"jianmen/internal/storage"
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
	createReq = asTestSuperAdmin(createReq)
	createRec := httptest.NewRecorder()
	server.handleTargets(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body=%s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}
	var created store.TargetView
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v; body=%s", err, createRec.Body.String())
	}
	if !created.InsecureIgnoreHostKey {
		t.Fatalf("created target did not preserve explicit insecure host key mode: %#v", created)
	}
	assertTargetResponseHasNoSecrets(t, createRec.Body.Bytes())

	getReq := httptest.NewRequest(http.MethodGet, "/api/targets/runtime-a", nil)
	getReq = asTestSuperAdmin(getReq)
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
		"insecure_ignore_host_key": false,
		"host_key_fingerprint": "SHA256:test-fingerprint"
	}`
	updateReq := httptest.NewRequest(http.MethodPut, "/api/targets/runtime-a", bytes.NewBufferString(updateBody))
	updateReq = asTestSuperAdmin(updateReq)
	updateRec := httptest.NewRecorder()
	server.handleTarget(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d; body=%s", updateRec.Code, http.StatusOK, updateRec.Body.String())
	}
	var updated store.TargetView
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("unmarshal update response: %v", err)
	}
	if updated.Name != "ubuntu" || updated.Host != "10.0.0.2" || updated.Port != 2200 || updated.Username != "ubuntu" {
		t.Fatalf("unexpected updated target view: %#v", updated)
	}
	if updated.InsecureIgnoreHostKey || updated.HostKeyFingerprint != "SHA256:test-fingerprint" || updated.KnownHostsPath != "" {
		t.Fatalf("unexpected updated host key settings: %#v", updated)
	}
	assertTargetResponseHasNoSecrets(t, updateRec.Body.Bytes())

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/targets/runtime-a", nil)
	deleteReq = asTestSuperAdmin(deleteReq)
	deleteRec := httptest.NewRecorder()
	server.handleTarget(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d; body=%s", deleteRec.Code, http.StatusNoContent, deleteRec.Body.String())
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/api/targets/runtime-a", nil)
	missingReq = asTestSuperAdmin(missingReq)
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
		req = asTestSuperAdmin(req)
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
	createAccountReq = asTestSuperAdmin(createAccountReq)
	createAccountRec := httptest.NewRecorder()
	server.handleTargets(createAccountRec, createAccountReq)
	if createAccountRec.Code != http.StatusCreated {
		t.Fatalf("create account status = %d, want %d; body=%s", createAccountRec.Code, http.StatusCreated, createAccountRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/hosts?page=1&page_size=1&q=prod", nil)
	listReq = asTestSuperAdmin(listReq)
	listRec := httptest.NewRecorder()
	server.handleHosts(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list hosts status = %d, want %d; body=%s", listRec.Code, http.StatusOK, listRec.Body.String())
	}
	var page pageResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &page); err != nil {
		t.Fatalf("unmarshal hosts page: %v; body=%s", err, listRec.Body.String())
	}
	if page.Total != 2 || page.Page != 1 || page.PageSize != 1 {
		t.Fatalf("unexpected hosts page: %#v", page)
	}
	// page.Items is []interface{} after JSON unmarshal; re-marshal to get typed HostView
	itemsJSON, _ := json.Marshal(page.Items)
	var hostItems []store.HostView
	if err := json.Unmarshal(itemsJSON, &hostItems); err != nil {
		t.Fatalf("unmarshal host items: %v", err)
	}
	if len(hostItems) != 1 {
		t.Fatalf("unexpected host items count: %d", len(hostItems))
	}
	if (hostItems[0].ID != "prod-a" && hostItems[0].ID != "prod-b") || hostItems[0].Group != "prod" {
		t.Fatalf("unexpected first host page item: %#v", hostItems[0])
	}

	accountsReq := httptest.NewRequest(http.MethodGet, "/api/hosts/prod-a/accounts", nil)
	accountsReq = asTestSuperAdmin(accountsReq)
	accountsRec := httptest.NewRecorder()
	server.handleHost(accountsRec, accountsReq)
	if accountsRec.Code != http.StatusOK {
		t.Fatalf("host accounts status = %d, want %d; body=%s", accountsRec.Code, http.StatusOK, accountsRec.Body.String())
	}
	var accountsPage pageResponse
	if err := json.Unmarshal(accountsRec.Body.Bytes(), &accountsPage); err != nil {
		t.Fatalf("unmarshal host accounts page: %v; body=%s", err, accountsRec.Body.String())
	}
	accItemsJSON, _ := json.Marshal(accountsPage.Items)
	var accounts []store.TargetView
	if err := json.Unmarshal(accItemsJSON, &accounts); err != nil {
		t.Fatalf("unmarshal host accounts items: %v; body=%s", err, accItemsJSON)
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

func TestHandleTestConnectionUsesStoredCredentialsWhenPayloadOmitsSecrets(t *testing.T) {
	server := newTargetTestServer(t)
	sshAddr := startTestPasswordSSHServer(t, "root", "secret")
	host, portText, err := net.SplitHostPort(sshAddr)
	if err != nil {
		t.Fatalf("split ssh addr: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse ssh port: %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewBufferString(fmt.Sprintf(`{
		"id": "stored-secret-account",
		"host_id": "stored-secret-host",
		"host": %q,
		"port": %d,
		"username": "root",
		"password": "secret",
		"insecure_ignore_host_key": true
	}`, host, port)))
	createReq = asTestSuperAdmin(createReq)
	createRec := httptest.NewRecorder()
	server.handleTargets(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body=%s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}

	testReq := httptest.NewRequest(http.MethodPost, "/api/targets/test-connection", bytes.NewBufferString(fmt.Sprintf(`{
		"id": "stored-secret-account",
		"host_id": "stored-secret-host",
		"host": %q,
		"port": %d,
		"username": "root",
		"password": "",
		"private_key_pem": "",
		"passphrase": "",
		"insecure_ignore_host_key": true
	}`, host, port)))
	testReq = asTestSuperAdmin(testReq)
	testRec := httptest.NewRecorder()
	server.handleTestConnection(testRec, testReq)
	if testRec.Code != http.StatusOK {
		t.Fatalf("test status = %d, want %d; body=%s", testRec.Code, http.StatusOK, testRec.Body.String())
	}
	var result struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(testRec.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal test response: %v; body=%s", err, testRec.Body.String())
	}
	if !result.OK {
		t.Fatalf("test connection ok = false, want true; message=%q body=%s", result.Message, testRec.Body.String())
	}
}

func TestProtectedHandlersFailClosedWithoutAuthenticatedUser(t *testing.T) {
	server := newTargetTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/targets", nil)
	rec := httptest.NewRecorder()

	server.handleTargets(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestInitStatusReturnsSuperAdminSummaryAfterSetup(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	if err := db.Create(&model.User{
		ID:          "admin-user",
		Username:    "admin",
		DisplayName: "超级管理员",
		Email:       "admin@example.com",
		Status:      "active",
	}).Error; err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	server.superAdminIDs = map[string]bool{"admin-user": true}

	req := httptest.NewRequest(http.MethodGet, "/api/init/status", nil)
	rec := httptest.NewRecorder()
	server.handleInitStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got InitStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, rec.Body.String())
	}
	if !got.Initialized {
		t.Fatal("initialized = false, want true")
	}
	if got.Admin == nil {
		t.Fatalf("admin summary is nil; body=%s", rec.Body.String())
	}
	if got.Admin.Username != "admin" || got.Admin.DisplayName != "超级管理员" || got.Admin.Email != "admin@example.com" {
		t.Fatalf("unexpected admin summary: %#v", got.Admin)
	}
}

func TestCreateUserStoresBcryptHashAndLoginWorks(t *testing.T) {
	server, db := newAdminDBTestServer(t)

	createReq := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewBufferString(`{
		"username": "alice",
		"password": "correct horse battery staple",
		"display_name": "Alice"
	}`))
	createRec := httptest.NewRecorder()
	server.createUser(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body=%s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}

	var created struct {
		User  model.User `json:"user"`
		Token string     `json:"token"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v; body=%s", err, createRec.Body.String())
	}
	if created.Token == "" {
		t.Fatal("create response did not include an API token")
	}

	var stored model.User
	if err := db.First(&stored, "id = ?", created.User.ID).Error; err != nil {
		t.Fatalf("load stored user: %v", err)
	}
	if !verifyPassword(stored.PasswordHash, "correct horse battery staple") {
		t.Fatalf("stored password hash does not verify; hash=%q", stored.PasswordHash)
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBufferString(`{
		"username": "alice",
		"password": "correct horse battery staple"
	}`))
	loginRec := httptest.NewRecorder()
	server.handleLogin(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body=%s", loginRec.Code, http.StatusOK, loginRec.Body.String())
	}
}

func TestLoginRateLimitAfterRepeatedFailures(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	passwordHash, err := hashPassword("correct-password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := db.Create(&model.User{
		ID:           "rate-limited-user",
		Username:     "rate-limited",
		PasswordHash: passwordHash,
		Status:       "active",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	for i := 0; i < loginFailureLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBufferString(`{
			"username": "rate-limited",
			"password": "wrong-password"
		}`))
		req.RemoteAddr = "203.0.113.10:12345"
		rec := httptest.NewRecorder()
		server.handleLogin(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want %d; body=%s", i+1, rec.Code, http.StatusUnauthorized, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBufferString(`{
		"username": "rate-limited",
		"password": "wrong-password"
	}`))
	req.RemoteAddr = "203.0.113.10:12345"
	rec := httptest.NewRecorder()
	server.handleLogin(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked status = %d, want %d; body=%s", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header was not set")
	}
}

func TestDevAdminTokenIsRejected(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	if err := db.Create(&model.User{ID: "admin", Username: "admin", Status: "active"}).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}

	called := false
	handler := server.withAuthAndUser(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Authorization", "Bearer dev-admin-token")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if called {
		t.Fatal("handler was called with dev-admin-token")
	}
}

func TestEncryptionKeyRequiresSuperAdminToken(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	users := []model.User{
		{ID: "regular", Username: "regular", Status: "active", TokenHash: hashToken("regular-token")},
		{ID: "admin", Username: "admin", Status: "active", TokenHash: hashToken("admin-token")},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	server.superAdminIDs = map[string]bool{"admin": true}
	handler := server.withAuthAndUser(server.handleInitEncryptionKey)

	missingAuthReq := httptest.NewRequest(http.MethodGet, "/api/init/encryption-key", nil)
	missingAuthRec := httptest.NewRecorder()
	handler(missingAuthRec, missingAuthReq)
	if missingAuthRec.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status = %d, want %d", missingAuthRec.Code, http.StatusUnauthorized)
	}

	regularReq := httptest.NewRequest(http.MethodGet, "/api/init/encryption-key", nil)
	regularReq.Header.Set("Authorization", "Bearer regular-token")
	regularRec := httptest.NewRecorder()
	handler(regularRec, regularReq)
	if regularRec.Code != http.StatusForbidden {
		t.Fatalf("regular status = %d, want %d; body=%s", regularRec.Code, http.StatusForbidden, regularRec.Body.String())
	}

	adminReq := httptest.NewRequest(http.MethodGet, "/api/init/encryption-key", nil)
	adminReq.Header.Set("Authorization", "Bearer admin-token")
	adminRec := httptest.NewRecorder()
	handler(adminRec, adminReq)
	if adminRec.Code != http.StatusOK {
		t.Fatalf("admin status = %d, want %d; body=%s", adminRec.Code, http.StatusOK, adminRec.Body.String())
	}
	var keyResp EncryptionKeyResponse
	if err := json.Unmarshal(adminRec.Body.Bytes(), &keyResp); err != nil {
		t.Fatalf("unmarshal key response: %v; body=%s", err, adminRec.Body.String())
	}
	if len(keyResp.Key) != 64 {
		t.Fatalf("key length = %d, want 64 hex chars", len(keyResp.Key))
	}

	secondAdminReq := httptest.NewRequest(http.MethodGet, "/api/init/encryption-key", nil)
	secondAdminReq.Header.Set("Authorization", "Bearer admin-token")
	secondAdminRec := httptest.NewRecorder()
	handler(secondAdminRec, secondAdminReq)
	if secondAdminRec.Code != http.StatusForbidden {
		t.Fatalf("second admin status = %d, want %d; body=%s", secondAdminRec.Code, http.StatusForbidden, secondAdminRec.Body.String())
	}
}

func TestHandleTestDBConnectionPayloadRequiresCredentials(t *testing.T) {
	server, _ := newAdminDBTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/db/accounts/test", strings.NewReader(`{"instance_id":"","username":"","password":""}`))
	req = asTestSuperAdmin(req)
	rec := httptest.NewRecorder()

	server.handleTestDBConnection(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleTestDBConnectionPayloadDoesNotCreateAccount(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	inst := model.DatabaseInstance{Name: "temp-test", Protocol: "mysql", Address: "127.0.0.1", Port: 1, Status: "active"}
	if err := db.Create(&inst).Error; err != nil {
		t.Fatalf("create instance: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/db/accounts/test", strings.NewReader(fmt.Sprintf(`{"instance_id":%q,"username":"probe","password":"secret"}`, inst.ID)))
	req = asTestSuperAdmin(req)
	rec := httptest.NewRecorder()

	server.handleTestDBConnection(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var count int64
	if err := db.Model(&model.DatabaseAccount{}).Where("username = ?", "probe").Count(&count).Error; err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if count != 0 {
		t.Fatalf("temporary test created %d database accounts, want 0", count)
	}
}

func newTargetTestServer(t *testing.T) *Server {
	t.Helper()

	// 初始化加密密钥
	dataDir := t.TempDir()
	if _, err := crypto.Init(dataDir); err != nil {
		t.Fatalf("crypto.Init: %v", err)
	}

	// 创建内存 SQLite 数据库并自动迁移表结构
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	cfg := &config.Config{
		Admin: config.AdminConfig{},
		Users: []config.User{
			{
				ID:       "u-admin",
				Username: "admin",
				Password: "admin",
			},
		},
	}
	storeInst := store.NewDBStore(db)
	return &Server{
		cfg:           cfg,
		store:         storeInst,
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		superAdminIDs: map[string]bool{"u-admin": true},
	}
}

func newAdminDBTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()

	dataDir := t.TempDir()
	if _, err := crypto.Init(dataDir); err != nil {
		t.Fatalf("crypto.Init: %v", err)
	}

	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	cfg := &config.Config{Admin: config.AdminConfig{}}
	return &Server{
		cfg:           cfg,
		store:         store.NewDBStore(db),
		db:            db,
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		dataDir:       dataDir,
		superAdminIDs: map[string]bool{},
	}, db
}

func asTestSuperAdmin(req *http.Request) *http.Request {
	ctx := context.WithValue(req.Context(), ctxKeyUserID, "u-admin")
	ctx = context.WithValue(ctx, ctxKeyUsername, "admin")
	return req.WithContext(ctx)
}

func startTestPasswordSSHServer(t *testing.T, username, password string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen test SSH server: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("new host signer: %v", err)
	}
	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, got []byte) (*ssh.Permissions, error) {
			if conn.User() == username && string(got) == password {
				return nil, nil
			}
			return nil, fmt.Errorf("invalid credentials")
		},
	}
	serverConfig.AddHostKey(signer)

	go func() {
		for {
			rawConn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer rawConn.Close()
				conn, chans, reqs, err := ssh.NewServerConn(rawConn, serverConfig)
				if err != nil {
					return
				}
				defer conn.Close()
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					ch.Reject(ssh.Prohibited, "not supported")
				}
			}()
		}
	}()

	return listener.Addr().String()
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
