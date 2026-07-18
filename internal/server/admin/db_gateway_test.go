package admin

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

func TestParseListenAddrDoesNotAdvertiseWildcardAsLoopback(t *testing.T) {
	// Ensure wildcard listeners are never advertised as local-only client endpoints.
	cases := []struct {
		name     string
		addr     string
		wantHost string
		wantPort int
	}{
		{name: "empty", addr: "", wantHost: "", wantPort: 33060},
		{name: "ipv4 wildcard", addr: "0.0.0.0:33060", wantHost: "", wantPort: 33060},
		{name: "ipv6 wildcard", addr: "[::]:33060", wantHost: "", wantPort: 33060},
		{name: "loopback", addr: "127.0.0.1:33061", wantHost: "127.0.0.1", wantPort: 33061},
		{name: "hostname", addr: "db.example.com:33062", wantHost: "db.example.com", wantPort: 33062},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			host, port := parseListenAddr(tt.addr)
			if host != tt.wantHost || port != tt.wantPort {
				t.Fatalf("parseListenAddr(%q) = %q:%d, want %q:%d", tt.addr, host, port, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func TestHandleDBGatewayReturnsProtocolListenerToConnectOnlyUser(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	certFile, caFile, leafFingerprint := writeGatewayTLSMaterial(t, "pg-gateway.example.test")
	if err := db.Create(&model.User{ID: "db-connect-user", Username: "connector", Status: "active"}).Error; err != nil {
		t.Fatalf("create connect-only user: %v", err)
	}
	instance := model.DatabaseInstance{ID: "gateway-secret-instance", Name: "gateway-secret-instance", Protocol: "postgres", Address: "127.0.0.1", Port: 5432, Status: "active"}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create database instance: %v", err)
	}
	if err := db.Create(&model.DatabaseAccount{ID: "gateway-secret-account", InstanceID: instance.ID, UniqueName: "gateway-secret-account", Username: "app", Password: model.NewEncryptedField("database-password"), Status: "active", ResourceID: "5001"}).Error; err != nil {
		t.Fatalf("create database account: %v", err)
	}
	seedGlobalAction(t, db, "db-connect-user", rbac.ActionDBConnect)
	server.cfg.DatabaseGateway = config.DatabaseGatewayConfig{
		Enabled: true,
		MySQL: config.DatabaseProtocolListener{
			Enabled: true,
			Address: "0.0.0.0:33060",
		},
		PostgreSQL: config.DatabaseProtocolListener{
			Enabled:    true,
			Address:    "0.0.0.0:54330",
			CertFile:   certFile,
			KeyFile:    "private-key-content",
			CAFile:     caFile,
			ServerName: "pg-gateway.example.test",
		},
	}

	request := asTestUser(
		httptest.NewRequest(http.MethodGet, "/api/db/gateway?protocol=postgresql", nil),
		"db-connect-user",
		"connector",
	)
	recorder := httptest.NewRecorder()
	server.handleDBGateway(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("gateway status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Enabled       bool   `json:"enabled"`
		Protocol      string `json:"protocol"`
		ListenAddr    string `json:"listen_addr"`
		Host          string `json:"host"`
		Port          int    `json:"port"`
		TLSEnabled    bool   `json:"tls_enabled"`
		TLSServerName string `json:"tls_server_name"`
		TLSCAPEM      string `json:"tls_ca_pem"`
		TLSCertSHA256 string `json:"tls_cert_sha256"`
	}
	if err := decodeTestData(t, recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode gateway response: %v", err)
	}
	if !response.Enabled || response.Protocol != "postgresql" || response.ListenAddr != "0.0.0.0:54330" ||
		response.Host != "" || response.Port != 54330 || !response.TLSEnabled ||
		response.TLSServerName != "pg-gateway.example.test" || response.TLSCAPEM == "" ||
		response.TLSCertSHA256 != leafFingerprint {
		t.Fatalf("unexpected PostgreSQL gateway response: %#v", response)
	}
	if strings.Contains(recorder.Body.String(), "private-key-content") || strings.Contains(recorder.Body.String(), "database-password") || strings.Contains(recorder.Body.String(), "key_file") {
		t.Fatalf("gateway response exposed a secret or key path: %s", recorder.Body.String())
	}
}

func TestHandleDBGatewayFallsBackToLeafCertificateAsTrustAnchor(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	certFile, fingerprint := writeGatewaySelfSignedTLSMaterial(t, "mysql-gateway.example.test")
	if err := db.Create(&model.User{ID: "db-view-user", Username: "viewer", Status: "active"}).Error; err != nil {
		t.Fatalf("create view-only user: %v", err)
	}
	seedGlobalAction(t, db, "db-view-user", rbac.ActionDBProxyView)
	server.cfg.DatabaseGateway = config.DatabaseGatewayConfig{Enabled: true, MySQL: config.DatabaseProtocolListener{
		Enabled: true, Address: "127.0.0.1:33060", CertFile: certFile, KeyFile: "private-key-content", ServerName: "mysql-gateway.example.test",
	}}

	recorder := httptest.NewRecorder()
	server.handleDBGateway(recorder, asTestUser(httptest.NewRequest(http.MethodGet, "/api/db/gateway?protocol=mysql", nil), "db-view-user", "viewer"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("gateway status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		TLSCAPEM      string `json:"tls_ca_pem"`
		TLSCertSHA256 string `json:"tls_cert_sha256"`
		TLSServerName string `json:"tls_server_name"`
	}
	if err := decodeTestData(t, recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode gateway response: %v", err)
	}
	if response.TLSCAPEM != string(readGatewayTestFile(t, certFile)) || response.TLSCertSHA256 != fingerprint || response.TLSServerName != "mysql-gateway.example.test" {
		t.Fatalf("unexpected fallback TLS material: %#v", response)
	}
}

func TestHandleDBGatewayRejectsMalformedCAWithoutLeakingTLSMaterial(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	certFile, _, _ := writeGatewayTLSMaterial(t, "pg-gateway.example.test")
	caFile := filepath.Join(t.TempDir(), "invalid-ca.pem")
	if err := os.WriteFile(caFile, []byte("-----BEGIN PRIVATE KEY-----\nnot-a-key\n-----END PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatalf("write malformed CA: %v", err)
	}
	if err := db.Create(&model.User{ID: "db-connect-user", Username: "connector", Status: "active"}).Error; err != nil {
		t.Fatalf("create connect-only user: %v", err)
	}
	seedGlobalAction(t, db, "db-connect-user", rbac.ActionDBConnect)
	server.cfg.DatabaseGateway = config.DatabaseGatewayConfig{Enabled: true, PostgreSQL: config.DatabaseProtocolListener{
		Enabled: true, Address: "127.0.0.1:54330", CertFile: certFile, KeyFile: "private-key-content", CAFile: caFile, ServerName: "pg-gateway.example.test",
	}}

	recorder := httptest.NewRecorder()
	server.handleDBGateway(recorder, asTestUser(httptest.NewRequest(http.MethodGet, "/api/db/gateway?protocol=postgresql", nil), "db-connect-user", "connector"))
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), databaseGatewayTLSMaterialUnavailable) {
		t.Fatalf("malformed CA response = status %d body %q", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "PRIVATE KEY") || strings.Contains(recorder.Body.String(), caFile) {
		t.Fatalf("malformed CA response leaked sensitive material: %s", recorder.Body.String())
	}
}

func TestHandleDBGatewayFailsClosedForUnverifiableTLSIdentity(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		serverName string
		build      func(t *testing.T) (string, string)
	}{
		{
			name:       "wrong SAN",
			serverName: "other.example.test",
			build: func(t *testing.T) (string, string) {
				certFile, caFile, _ := writeGatewayTLSMaterial(t, "gateway.example.test")
				return certFile, caFile
			},
		},
		{
			name:       "wrong CA",
			serverName: "gateway.example.test",
			build: func(t *testing.T) (string, string) {
				certFile, _, _ := writeGatewayTLSMaterial(t, "gateway.example.test")
				_, unrelatedCAFile, _ := writeGatewayTLSMaterial(t, "unrelated.example.test")
				return certFile, unrelatedCAFile
			},
		},
		{
			name:       "expired leaf",
			serverName: "gateway.example.test",
			build: func(t *testing.T) (string, string) {
				certFile, caFile, _ := writeGatewayTLSMaterialAt(t, "gateway.example.test", now.Add(-2*time.Hour), now.Add(-time.Hour))
				return certFile, caFile
			},
		},
		{
			name:       "non CA trust anchor",
			serverName: "gateway.example.test",
			build: func(t *testing.T) (string, string) {
				certFile, _, _ := writeGatewayTLSMaterial(t, "gateway.example.test")
				return certFile, certFile
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, db := newAdminDBTestServer(t)
			seedTestSuperAdmin(t, db, "u-admin")
			certFile, caFile := tt.build(t)
			server.cfg.DatabaseGateway = config.DatabaseGatewayConfig{Enabled: true, PostgreSQL: config.DatabaseProtocolListener{
				Enabled: true, Address: "127.0.0.1:54330", CertFile: certFile, KeyFile: "private-key-content", CAFile: caFile, ServerName: tt.serverName,
			}}

			recorder := httptest.NewRecorder()
			server.handleDBGateway(recorder, asTestSuperAdmin(httptest.NewRequest(http.MethodGet, "/api/db/gateway?protocol=postgresql", nil)))
			if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), databaseGatewayTLSMaterialUnavailable) {
				t.Fatalf("unverifiable identity response = status %d body %q", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestHandleDBGatewayDisabledStateDoesNotReadOrReturnTLSMaterial(t *testing.T) {
	tests := []struct {
		name            string
		gatewayEnabled  bool
		listenerEnabled bool
	}{
		{name: "gateway disabled", gatewayEnabled: false, listenerEnabled: true},
		{name: "listener disabled", gatewayEnabled: true, listenerEnabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, db := newAdminDBTestServer(t)
			seedTestSuperAdmin(t, db, "u-admin")
			server.cfg.DatabaseGateway = config.DatabaseGatewayConfig{
				Enabled: tt.gatewayEnabled,
				MySQL: config.DatabaseProtocolListener{
					Enabled: true,
					Address: "127.0.0.1:33060",
				},
				PostgreSQL: config.DatabaseProtocolListener{
					Enabled:    tt.listenerEnabled,
					Address:    "127.0.0.1:54330",
					CertFile:   filepath.Join(t.TempDir(), "missing.crt"),
					KeyFile:    filepath.Join(t.TempDir(), "missing.key"),
					CAFile:     filepath.Join(t.TempDir(), "missing-ca.crt"),
					ServerName: "gateway.example.test",
				},
			}

			recorder := httptest.NewRecorder()
			server.handleDBGateway(recorder, asTestSuperAdmin(httptest.NewRequest(http.MethodGet, "/api/db/gateway?protocol=postgresql", nil)))
			if recorder.Code != http.StatusOK {
				t.Fatalf("disabled gateway status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
			}
			var response struct {
				Enabled       bool    `json:"enabled"`
				TLSEnabled    bool    `json:"tls_enabled"`
				TLSServerName *string `json:"tls_server_name"`
				TLSCAPEM      *string `json:"tls_ca_pem"`
				TLSCertSHA256 *string `json:"tls_cert_sha256"`
			}
			if err := decodeTestData(t, recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode disabled gateway response: %v", err)
			}
			if response.Enabled || response.TLSEnabled || response.TLSServerName != nil || response.TLSCAPEM != nil || response.TLSCertSHA256 != nil {
				t.Fatalf("disabled gateway exposed TLS identity material: %#v", response)
			}
		})
	}
}

func TestHandleDBGatewayRejectsUnauthorizedUser(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	if err := db.Create(&model.User{ID: "no-db-gateway", Username: "denied", Status: "active"}).Error; err != nil {
		t.Fatalf("create unauthorized user: %v", err)
	}
	server.cfg.DatabaseGateway = config.DatabaseGatewayConfig{Enabled: true, MySQL: config.DatabaseProtocolListener{Enabled: true, Address: "127.0.0.1:33060"}}

	recorder := httptest.NewRecorder()
	server.handleDBGateway(recorder, asTestUser(httptest.NewRequest(http.MethodGet, "/api/db/gateway?protocol=mysql", nil), "no-db-gateway", "denied"))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("unauthorized gateway status = %d, want %d; body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
}

func writeGatewayTLSMaterial(t *testing.T, serverName string) (certFile, caFile, fingerprint string) {
	t.Helper()
	return writeGatewayTLSMaterialAt(t, serverName, time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
}

func writeGatewayTLSMaterialAt(t *testing.T, serverName string, notBefore, notAfter time.Time) (certFile, caFile, fingerprint string) {
	t.Helper()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(100), Subject: pkix.Name{CommonName: "gateway test CA"}, NotBefore: now.Add(-24 * time.Hour), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	leafTemplate := &x509.Certificate{SerialNumber: big.NewInt(101), Subject: pkix.Name{CommonName: serverName}, DNSNames: []string{serverName}, NotBefore: notBefore, NotAfter: notAfter, KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caTemplate, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create leaf certificate: %v", err)
	}
	dir := t.TempDir()
	certFile = filepath.Join(dir, "gateway.crt")
	caFile = filepath.Join(dir, "gateway-ca.crt")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), 0o600); err != nil {
		t.Fatalf("write leaf certificate: %v", err)
	}
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600); err != nil {
		t.Fatalf("write CA certificate: %v", err)
	}
	sum := sha256.Sum256(leafDER)
	return certFile, caFile, fmt.Sprintf("%x", sum[:])
}

func writeGatewaySelfSignedTLSMaterial(t *testing.T, serverName string) (certFile, fingerprint string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(102),
		Subject:      pkix.Name{CommonName: serverName},
		DNSNames:     []string{serverName},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certFile = filepath.Join(t.TempDir(), "self-signed-gateway.crt")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	return certFile, fmt.Sprintf("%x", sum[:])
}

func readGatewayTestFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return contents
}
