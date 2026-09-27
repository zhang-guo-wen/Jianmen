package admin

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

func TestHandleTestConnectionRequiresExplicitHostKeyVerification(t *testing.T) {
	server := newTargetTestServer(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for unexpected SSH dial: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	accepted := make(chan bool, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			accepted <- false
			return
		}
		accepted <- true
		_ = conn.Close()
	}()

	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split ssh address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse ssh port: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/targets/test-connection", bytes.NewBufferString(`{
		"host": "`+host+`",
		"port": `+strconv.Itoa(port)+`,
		"username": "root",
		"password": "secret"
	}`))
	req = asTestSuperAdmin(req)
	rec := httptest.NewRecorder()
	server.handleTestConnection(rec, req)

	_ = listener.Close()
	if <-accepted {
		t.Fatal("SSH dial occurred before host key verification configuration was validated")
	}
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusPreconditionFailed, rec.Body.String())
	}
	var result struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				HostID         string `json:"host_id"`
				IdentityStatus string `json:"identity_status"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	if result.Error.Code != codeSSHHostIdentityUnavailable ||
		result.Error.Details.IdentityStatus != "unavailable" {
		t.Fatalf("unexpected identity error: %#v; body=%s", result.Error, rec.Body.String())
	}
}

func TestHandleTestConnectionReturnsStructuredHostKeyChangeAndDisablesHost(t *testing.T) {
	server := newTargetTestServer(t)
	sshAddress := startTestPasswordSSHServer(t, "root", "secret")
	host, portText, err := net.SplitHostPort(sshAddress)
	if err != nil {
		t.Fatalf("split SSH address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse SSH port: %v", err)
	}
	createManagedTestSSHHost(t, server, "changed-host", host, port)

	createReq := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewBufferString(`{
		"id": "changed-account",
		"host_id": "changed-host",
		"username": "root",
		"password": "secret"
	}`))
	createReq = asTestSuperAdmin(createReq)
	createRec := httptest.NewRecorder()
	server.handleTargets(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create account status = %d, want %d; body=%s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}

	_, replacementPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate replacement key: %v", err)
	}
	replacementSigner, err := ssh.NewSignerFromKey(replacementPrivateKey)
	if err != nil {
		t.Fatalf("create replacement signer: %v", err)
	}
	oldFingerprint := ssh.FingerprintSHA256(replacementSigner.PublicKey())
	if err := server.db.Model(&model.Host{}).Where("id = ?", "changed-host").Updates(map[string]any{
		"host_key_fingerprint": oldFingerprint,
		"known_hosts":          knownhosts.Line([]string{sshAddress}, replacementSigner.PublicKey()),
		"status":               "active",
	}).Error; err != nil {
		t.Fatalf("replace stored identity: %v", err)
	}

	testReq := httptest.NewRequest(http.MethodPost, "/api/targets/test-connection", bytes.NewBufferString(`{"id":"changed-account"}`))
	testReq = asTestSuperAdmin(testReq)
	testRec := httptest.NewRecorder()
	server.handleTestConnection(testRec, testReq)
	if testRec.Code != http.StatusConflict {
		t.Fatalf("test status = %d, want %d; body=%s", testRec.Code, http.StatusConflict, testRec.Body.String())
	}
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				HostID         string `json:"host_id"`
				OldFingerprint string `json:"old_fingerprint"`
				NewFingerprint string `json:"new_fingerprint"`
				HostDisabled   bool   `json:"host_disabled"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(testRec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode changed response: %v; body=%s", err, testRec.Body.String())
	}
	if response.Error.Code != codeSSHHostKeyChanged ||
		response.Error.Details.HostID != "changed-host" ||
		response.Error.Details.OldFingerprint != oldFingerprint ||
		response.Error.Details.NewFingerprint == "" ||
		!response.Error.Details.HostDisabled {
		t.Fatalf("unexpected changed response: %#v; body=%s", response, testRec.Body.String())
	}
	var persisted model.Host
	if err := server.db.First(&persisted, "id = ?", "changed-host").Error; err != nil {
		t.Fatalf("load changed host: %v", err)
	}
	if persisted.Status != "disabled" {
		t.Fatalf("changed host status = %q, want disabled", persisted.Status)
	}

	staleConfirmReq := httptest.NewRequest(
		http.MethodPost,
		"/api/hosts/changed-host/refresh-identity",
		bytes.NewBufferString(`{"confirmed":true,"expected_fingerprint":"SHA256:stale-warning"}`),
	)
	staleConfirmReq = asTestSuperAdmin(staleConfirmReq)
	staleConfirmRec := httptest.NewRecorder()
	server.handleHost(staleConfirmRec, staleConfirmReq)
	if staleConfirmRec.Code != http.StatusConflict {
		t.Fatalf("stale confirmation status = %d, want %d; body=%s",
			staleConfirmRec.Code, http.StatusConflict, staleConfirmRec.Body.String())
	}
	var staleConfirmation struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				ExpectedFingerprint string `json:"expected_fingerprint"`
				NewFingerprint      string `json:"new_fingerprint"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(staleConfirmRec.Body.Bytes(), &staleConfirmation); err != nil {
		t.Fatalf("decode stale confirmation: %v", err)
	}
	if staleConfirmation.Error.Code != codeSSHHostKeyChanged ||
		staleConfirmation.Error.Details.ExpectedFingerprint != "SHA256:stale-warning" ||
		staleConfirmation.Error.Details.NewFingerprint != response.Error.Details.NewFingerprint {
		t.Fatalf("unexpected stale confirmation: %#v", staleConfirmation)
	}
	if err := server.db.First(&persisted, "id = ?", "changed-host").Error; err != nil {
		t.Fatalf("reload changed host: %v", err)
	}
	if persisted.Status != "disabled" || persisted.HostKeyFingerprint != oldFingerprint {
		t.Fatalf("stale confirmation changed host: %#v", persisted)
	}

	enableReq := httptest.NewRequest(
		http.MethodPost,
		"/api/hosts/changed-host/refresh-identity",
		bytes.NewBufferString(`{"confirmed":true,"expected_fingerprint":"`+response.Error.Details.NewFingerprint+`"}`),
	)
	enableReq = asTestSuperAdmin(enableReq)
	enableRec := httptest.NewRecorder()
	server.handleHost(enableRec, enableReq)
	if enableRec.Code != http.StatusOK {
		t.Fatalf("reenable status = %d, want %d; body=%s", enableRec.Code, http.StatusOK, enableRec.Body.String())
	}
	var enabled struct {
		Status             string `json:"status"`
		IdentityStatus     string `json:"identity_status"`
		HostKeyFingerprint string `json:"host_key_fingerprint"`
	}
	if err := decodeTestData(t, enableRec.Body.Bytes(), &enabled); err != nil {
		t.Fatalf("decode reenabled host: %v", err)
	}
	if enabled.Status != "active" || enabled.IdentityStatus != "available" ||
		enabled.HostKeyFingerprint != response.Error.Details.NewFingerprint {
		t.Fatalf("unexpected reenabled host identity: %#v", enabled)
	}

	retryReq := httptest.NewRequest(http.MethodPost, "/api/targets/test-connection", bytes.NewBufferString(`{"id":"changed-account"}`))
	retryReq = asTestSuperAdmin(retryReq)
	retryRec := httptest.NewRecorder()
	server.handleTestConnection(retryRec, retryReq)
	if retryRec.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want %d; body=%s", retryRec.Code, http.StatusOK, retryRec.Body.String())
	}
}

func TestUnavailableHostIdentityCanBeConfirmedRefreshedAndRetried(t *testing.T) {
	server := newTargetTestServer(t)
	sshAddress := startTestPasswordSSHServer(t, "root", "secret")
	host, portText, err := net.SplitHostPort(sshAddress)
	if err != nil {
		t.Fatalf("split SSH address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse SSH port: %v", err)
	}
	if _, err := server.hostTargets.AddHost(context.Background(), store.HostRecord{
		ID: "identity-missing-host", Name: "identity-missing-host",
		Address: host, Port: port, Protocol: "ssh", Status: "active",
	}); err != nil {
		t.Fatalf("create identity-missing host: %v", err)
	}
	createReq := asTestSuperAdmin(httptest.NewRequest(
		http.MethodPost,
		"/api/targets",
		bytes.NewBufferString(`{
			"id":"identity-missing-account",
			"host_id":"identity-missing-host",
			"username":"root",
			"password":"secret"
		}`),
	))
	createRec := httptest.NewRecorder()
	server.handleTargets(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create account status = %d, want %d; body=%s",
			createRec.Code, http.StatusCreated, createRec.Body.String())
	}

	testReq := asTestSuperAdmin(httptest.NewRequest(
		http.MethodPost,
		"/api/targets/test-connection",
		bytes.NewBufferString(`{"id":"identity-missing-account"}`),
	))
	testRec := httptest.NewRecorder()
	server.handleTestConnection(testRec, testReq)
	if testRec.Code != http.StatusPreconditionFailed {
		t.Fatalf("missing identity status = %d, want %d; body=%s",
			testRec.Code, http.StatusPreconditionFailed, testRec.Body.String())
	}
	var unavailable struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				HostID         string `json:"host_id"`
				NewFingerprint string `json:"new_fingerprint"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(testRec.Body.Bytes(), &unavailable); err != nil {
		t.Fatalf("decode unavailable identity: %v", err)
	}
	if unavailable.Error.Code != codeSSHHostIdentityUnavailable ||
		unavailable.Error.Details.HostID != "identity-missing-host" ||
		unavailable.Error.Details.NewFingerprint == "" {
		t.Fatalf("unexpected unavailable identity: %#v", unavailable)
	}
	var beforeConfirm model.Host
	if err := server.db.First(&beforeConfirm, "id = ?", "identity-missing-host").Error; err != nil {
		t.Fatalf("load identity-missing host: %v", err)
	}
	if beforeConfirm.HostKeyFingerprint != "" || beforeConfirm.KnownHosts != "" ||
		beforeConfirm.Status != "active" {
		t.Fatalf("identity probe mutated host before confirmation: %#v", beforeConfirm)
	}

	confirmReq := asTestSuperAdmin(httptest.NewRequest(
		http.MethodPost,
		"/api/hosts/identity-missing-host/refresh-identity",
		bytes.NewBufferString(`{"confirmed":true,"expected_fingerprint":"`+
			unavailable.Error.Details.NewFingerprint+`"}`),
	))
	confirmRec := httptest.NewRecorder()
	server.handleHost(confirmRec, confirmReq)
	if confirmRec.Code != http.StatusOK {
		t.Fatalf("identity confirmation status = %d, want %d; body=%s",
			confirmRec.Code, http.StatusOK, confirmRec.Body.String())
	}
	var confirmed struct {
		Status             string `json:"status"`
		IdentityStatus     string `json:"identity_status"`
		HostKeyFingerprint string `json:"host_key_fingerprint"`
	}
	if err := decodeTestData(t, confirmRec.Body.Bytes(), &confirmed); err != nil {
		t.Fatalf("decode confirmed host: %v", err)
	}
	if confirmed.Status != "active" || confirmed.IdentityStatus != "available" ||
		confirmed.HostKeyFingerprint != unavailable.Error.Details.NewFingerprint {
		t.Fatalf("confirmed host = %#v", confirmed)
	}

	retryReq := asTestSuperAdmin(httptest.NewRequest(
		http.MethodPost,
		"/api/targets/test-connection",
		bytes.NewBufferString(`{"id":"identity-missing-account"}`),
	))
	retryRec := httptest.NewRecorder()
	server.handleTestConnection(retryRec, retryReq)
	if retryRec.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want %d; body=%s",
			retryRec.Code, http.StatusOK, retryRec.Body.String())
	}
}

func TestHandleTestConnectionAllowsConnectOnlyStoredAccount(t *testing.T) {
	server := newTargetTestServer(t)
	sshAddress := startTestPasswordSSHServer(t, "root", "secret")
	host, portText, err := net.SplitHostPort(sshAddress)
	if err != nil {
		t.Fatalf("split SSH address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse SSH port: %v", err)
	}
	createManagedTestSSHHost(t, server, "connect-only-host", host, port)

	createReq := asTestSuperAdmin(httptest.NewRequest(
		http.MethodPost,
		"/api/targets",
		bytes.NewBufferString(`{
			"id": "connect-only-account",
			"host_id": "connect-only-host",
			"username": "root",
			"password": "secret"
		}`),
	))
	createRec := httptest.NewRecorder()
	server.handleTargets(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create account status = %d, want %d; body=%s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}

	seedConnectionAction(t, server.db, "connection-test-only-user", rbac.ActionSessionConnect)
	seedResourceGrant(t, server.db, "connection-test-only-user", model.ResourceTypeHostAccount, "connect-only-account")
	testReq := withTestUser(
		httptest.NewRequest(http.MethodPost, "/api/targets/test-connection", bytes.NewBufferString(`{"id":"connect-only-account"}`)),
		"connection-test-only-user",
		"connection-test-only-user",
	)
	testRec := httptest.NewRecorder()
	server.handleTestConnection(testRec, testReq)
	if testRec.Code != http.StatusOK {
		t.Fatalf("connect-only test status = %d, want %d; body=%s", testRec.Code, http.StatusOK, testRec.Body.String())
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := decodeTestData(t, testRec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode connection result: %v", err)
	}
	if !result.OK {
		t.Fatalf("connect-only connection test failed: %s", testRec.Body.String())
	}

	refreshReq := withTestUser(
		httptest.NewRequest(
			http.MethodPost,
			"/api/hosts/connect-only-host/refresh-identity",
			bytes.NewBufferString(`{"confirmed":true,"expected_fingerprint":"SHA256:not-authorized"}`),
		),
		"connection-test-only-user",
		"connection-test-only-user",
	)
	refreshRec := httptest.NewRecorder()
	server.handleHost(refreshRec, refreshReq)
	if refreshRec.Code != http.StatusForbidden {
		t.Fatalf("connect-only identity refresh status = %d, want %d; body=%s",
			refreshRec.Code, http.StatusForbidden, refreshRec.Body.String())
	}
}

func TestReenableHostRefreshFailureReturnsConflictAndKeepsDisabled(t *testing.T) {
	server := newTargetTestServer(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve unavailable SSH port: %v", err)
	}
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split unavailable SSH address: %v", err)
	}
	port, _ := strconv.Atoi(portText)
	_ = listener.Close()
	if _, err := server.hostTargets.AddHost(context.Background(), store.HostRecord{
		ID: "refresh-failure-host", Name: "refresh-failure-host", Address: host,
		Port: port, Protocol: "ssh", Status: "disabled",
	}); err != nil {
		t.Fatalf("create disabled host: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/hosts/refresh-failure-host", bytes.NewBufferString(`{
		"name": "refresh-failure-host",
		"address": "`+host+`",
		"port": `+strconv.Itoa(port)+`,
		"protocol": "ssh",
		"status": "active"
	}`))
	req = asTestSuperAdmin(req)
	rec := httptest.NewRecorder()
	server.handleHost(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("reenable status = %d, want %d; body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				HostID     string `json:"host_id"`
				HostStatus string `json:"host_status"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode refresh failure: %v", err)
	}
	if response.Error.Code != codeSSHHostIdentityRefreshFailure ||
		response.Error.Details.HostID != "refresh-failure-host" ||
		response.Error.Details.HostStatus != "disabled" {
		t.Fatalf("unexpected refresh failure response: %#v; body=%s", response, rec.Body.String())
	}
	var persisted model.Host
	if err := server.db.First(&persisted, "id = ?", "refresh-failure-host").Error; err != nil {
		t.Fatalf("load disabled host: %v", err)
	}
	if persisted.Status != "disabled" {
		t.Fatalf("host status = %q, want disabled", persisted.Status)
	}
}
