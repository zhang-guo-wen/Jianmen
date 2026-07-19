//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"jianmen/internal/config"
	"jianmen/internal/online"
	"jianmen/internal/server/dbproxy"
)

func init() {
	_ = mysqlDriver.SetLogger(&mysqlDriver.NopLogger{})
}

func TestPostgresGatewayTLSErrorCategory(t *testing.T) {
	unknownAuthority := fmt.Errorf("pgx wrapper: %w", x509.UnknownAuthorityError{Cert: &x509.Certificate{}})
	if !isPostgresGatewayTLSError(unknownAuthority, postgresTLSUnknownAuthority) {
		t.Fatal("wrapped x509.UnknownAuthorityError was not recognized")
	}

	hostname := fmt.Errorf("pgx wrapper: %w", x509.HostnameError{Certificate: &x509.Certificate{}, Host: "localhost"})
	if !isPostgresGatewayTLSError(hostname, postgresTLSHostname) {
		t.Fatal("wrapped x509.HostnameError was not recognized")
	}

	if isPostgresGatewayTLSError(errors.New("connection refused"), postgresTLSUnknownAuthority) {
		t.Fatal("unrelated connection error was accepted as a TLS identity error")
	}
}

type databaseGatewayEndpoint struct {
	address string
	caFile  string
}

func startDatabaseGateway(t *testing.T, fixture metadataFixture, protocol string, logger *slog.Logger) databaseGatewayEndpoint {
	t.Helper()
	addr := freeTCPAddress(t)
	authorizer := newIntegrationAuthorizer(t, fixture)
	gatewayConfig := config.DatabaseGatewayConfig{Enabled: true}
	switch protocol {
	case "mysql":
		gatewayConfig.MySQL = config.DatabaseProtocolListener{Enabled: true, Address: addr}
	case "postgresql":
		certFile, keyFile, caFile := writeIntegrationTLSCertificate(t)
		gatewayConfig.PostgreSQL = config.DatabaseProtocolListener{
			Enabled: true, Address: addr, CertFile: certFile, KeyFile: keyFile, CAFile: caFile, ServerName: "127.0.0.1",
		}
	default:
		t.Fatalf("unsupported database gateway protocol %q", protocol)
	}
	gateway := dbproxy.NewGateway(
		gatewayConfig,
		fixture.store,
		fixture.replayDir,
		logger,
		fixture.db,
		authorizer,
		online.NewRegistry(),
		fixture.store,
	)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- gateway.ListenAndServe(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("database gateway stopped with error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("database gateway did not stop")
		}
	})
	waitServerTCP(t, addr, errCh)
	endpoint := databaseGatewayEndpoint{address: addr}
	if protocol == "postgresql" {
		endpoint.caFile = gatewayConfig.PostgreSQL.CAFile
	}
	return endpoint
}

func databaseGatewayDebugLogger() (*slog.Logger, *bytes.Buffer) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return logger, &output
}

func writeIntegrationTLSCertificate(t *testing.T) (string, string, string) {
	t.Helper()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate integration TLS CA key: %v", err)
	}
	caTemplate := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "gateway integration CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caCertificate, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create integration TLS CA certificate: %v", err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate integration TLS server key: %v", err)
	}
	serverTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	certificate, err := x509.CreateCertificate(rand.Reader, &serverTemplate, &caTemplate, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create integration TLS server certificate: %v", err)
	}
	dir := t.TempDir()
	certFile := filepath.Join(dir, "database.crt")
	keyFile := filepath.Join(dir, "database.key")
	caFile := filepath.Join(dir, "database-ca.crt")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), 0o600); err != nil {
		t.Fatalf("write integration TLS certificate: %v", err)
	}
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertificate}), 0o600); err != nil {
		t.Fatalf("write integration TLS CA certificate: %v", err)
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal integration TLS key: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey}), 0o600); err != nil {
		t.Fatalf("write integration TLS key: %v", err)
	}
	return certFile, keyFile, caFile
}

type postgresTLSFailure string

const (
	postgresTLSUnknownAuthority postgresTLSFailure = "unknown authority"
	postgresTLSHostname         postgresTLSFailure = "hostname"
)

func assertPostgresGatewayTLSFailure(t *testing.T, dsn, name string, expected postgresTLSFailure) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		if !isPostgresGatewayTLSError(err, expected) {
			t.Fatalf("PostgreSQL gateway connection with %s returned %T, want x509 %s error: %v", name, err, expected, err)
		}
		return
	}
	defer db.Close()
	err = db.Ping()
	if err == nil {
		t.Fatalf("PostgreSQL gateway connection with %s unexpectedly succeeded", name)
	}
	if !isPostgresGatewayTLSError(err, expected) {
		t.Fatalf("PostgreSQL gateway connection with %s returned %T, want x509 %s error: %v", name, err, expected, err)
	}
}

func isPostgresGatewayTLSError(err error, expected postgresTLSFailure) bool {
	switch expected {
	case postgresTLSUnknownAuthority:
		var value x509.UnknownAuthorityError
		if errors.As(err, &value) {
			return true
		}
		var pointer *x509.UnknownAuthorityError
		return errors.As(err, &pointer)
	case postgresTLSHostname:
		var value x509.HostnameError
		if errors.As(err, &value) {
			return true
		}
		var pointer *x509.HostnameError
		return errors.As(err, &pointer)
	default:
		return false
	}
}

func writeMySQLIntegrationTLSFiles(t *testing.T) (string, string) {
	t.Helper()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := x509.Certificate{
		SerialNumber: big.NewInt(100), Subject: pkix.Name{CommonName: "jianmen mysql integration CA"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serverTemplate := x509.Certificate{
		SerialNumber: big.NewInt(101), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, &serverTemplate, &caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	caFile := filepath.Join(directory, "ca.crt")
	certFile := filepath.Join(directory, "server.crt")
	keyFile := filepath.Join(directory, "server.key")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}), 0o644); err != nil {
		t.Fatal(err)
	}
	keyPEM := x509.MarshalPKCS1PrivateKey(serverKey)
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyPEM}), 0o644); err != nil {
		t.Fatal(err)
	}
	return directory, caFile
}

func readIntegrationFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func waitMySQL(t *testing.T, addr, username, password string) {
	t.Helper()
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/appdb?timeout=2s&readTimeout=2s&writeTimeout=2s", username, password, addr)
	waitFor(t, 2*time.Minute, time.Second, func() error {
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			return err
		}
		defer db.Close()
		return db.Ping()
	})
}

func waitPostgres(t *testing.T, addr, username, password string) {
	t.Helper()
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s/%s?sslmode=disable",
		url.QueryEscape(username),
		url.QueryEscape(password),
		addr,
		username,
	)
	waitFor(t, 2*time.Minute, time.Second, func() error {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return err
		}
		defer db.Close()
		return db.Ping()
	})
}

func assertPostgresSCRAMConfigured(t *testing.T, addr, username, password string) {
	t.Helper()
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s/%s?sslmode=disable",
		url.QueryEscape(username),
		url.QueryEscape(password),
		addr,
		username,
	)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL SCRAM fixture: %v", err)
	}
	defer db.Close()

	var verifier string
	if err := db.QueryRow(
		"SELECT rolpassword FROM pg_authid WHERE rolname = current_user",
	).Scan(&verifier); err != nil {
		t.Fatalf("read PostgreSQL password verifier: %v", err)
	}
	if !strings.HasPrefix(verifier, "SCRAM-SHA-256$") {
		t.Fatalf("PostgreSQL password verifier is not SCRAM-SHA-256")
	}

	var hbaConfig string
	if err := db.QueryRow(
		"SELECT pg_read_file(current_setting('hba_file'))",
	).Scan(&hbaConfig); err != nil {
		t.Fatalf("read PostgreSQL host authentication config: %v", err)
	}
	if !postgresHBARequiresSCRAM(hbaConfig) {
		t.Fatalf("PostgreSQL host authentication is not configured for SCRAM-SHA-256")
	}

	wrongPasswordDSN := fmt.Sprintf(
		"postgres://%s:%s@%s/%s?sslmode=disable",
		url.QueryEscape(username),
		url.QueryEscape(password+"-wrong"),
		addr,
		username,
	)
	wrongPasswordDB, err := sql.Open("pgx", wrongPasswordDSN)
	if err != nil {
		t.Fatalf("open PostgreSQL wrong-password probe: %v", err)
	}
	defer wrongPasswordDB.Close()
	if err := wrongPasswordDB.Ping(); err == nil {
		t.Fatal("PostgreSQL accepted a wrong password; SCRAM fixture may be using trust authentication")
	}
}

func postgresHBARequiresSCRAM(config string) bool {
	for _, line := range strings.Split(config, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 5 &&
			fields[0] == "host" &&
			fields[1] == "all" &&
			fields[2] == "all" &&
			fields[len(fields)-1] == "scram-sha-256" {
			return true
		}
	}
	return false
}

func assertDBAuditSQLContains(t *testing.T, replayDir, query string) {
	t.Helper()
	query = strings.ToLower(query)
	waitFor(t, 5*time.Second, 100*time.Millisecond, func() error {
		paths, err := filepath.Glob(filepath.Join(replayDir, "db", "*", "queries.jsonl"))
		if err != nil {
			return err
		}
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for lineNumber, line := range bytes.Split(raw, []byte{'\n'}) {
				line = bytes.TrimSpace(line)
				if len(line) == 0 {
					continue
				}
				var event struct {
					SQL string `json:"sql"`
				}
				if err := json.Unmarshal(line, &event); err != nil {
					return fmt.Errorf("decode database audit %s line %d: %w", path, lineNumber+1, err)
				}
				if strings.Contains(strings.ToLower(event.SQL), query) {
					return nil
				}
			}
		}
		return fmt.Errorf("audit sql %q not found in %d db audit files", query, len(paths))
	})
}

func sanitizeTestName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "mysql"
	}
	return out
}
