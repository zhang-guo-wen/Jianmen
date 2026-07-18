package admin

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
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
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var result struct {
		Data struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	if result.Data.OK {
		t.Fatalf("connection test unexpectedly succeeded without host key verification: %s", rec.Body.String())
	}
	if !strings.Contains(result.Data.Error, "host key verification is required") {
		t.Fatalf("error = %q, want explicit host key verification error; body=%s", result.Data.Error, rec.Body.String())
	}
}
