//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
	_ "github.com/jackc/pgx/v5/stdlib"

	jmstore "jianmen/internal/store"
	"jianmen/internal/util"
)

const (
	postgresCompatUpstreamUser     = "app"
	postgresCompatUpstreamPassword = "before\u00a0after"
	postgresCompatDatabase         = "app"
)

func TestDatabaseGatewayPostgresCompatibilityMatrix(t *testing.T) {
	requireDocker(t)
	for _, image := range postgresCompatImages() {
		image := image
		t.Run(sanitizeTestName(image), func(t *testing.T) {
			major := postgresCompatImageMajor(t, image)
			containerID := runContainer(
				t,
				"jianmen-it-postgres-compat-"+major,
				"-e", "POSTGRES_USER="+postgresCompatUpstreamUser,
				"-e", "POSTGRES_PASSWORD="+postgresCompatUpstreamPassword,
				"-e", "POSTGRES_DB="+postgresCompatDatabase,
				"-e", "POSTGRES_HOST_AUTH_METHOD=scram-sha-256",
				"-p", "127.0.0.1::5432",
				image,
			)
			upstreamAddress := containerAddress(t, containerID, "5432/tcp")
			waitPostgres(
				t,
				upstreamAddress,
				postgresCompatUpstreamUser,
				postgresCompatUpstreamPassword,
			)
			assertPostgresSCRAMConfigured(
				t,
				upstreamAddress,
				postgresCompatUpstreamUser,
				postgresCompatUpstreamPassword,
			)

			fixture := newMetadataFixture(t)
			host, port := splitAddress(t, upstreamAddress)
			instance, err := fixture.store.AddDatabaseInstance(jmstore.DatabaseInstanceInput{
				Name: "postgres-compat-" + major, Protocol: "postgres",
				Address: host, Port: port, TLSMode: "disable",
			})
			if err != nil {
				t.Fatalf("add PostgreSQL %s instance: %v", major, err)
			}
			account, err := fixture.store.AddDatabaseAccount(
				instance.ID,
				postgresCompatUpstreamUser,
				postgresCompatUpstreamPassword,
				"",
				"",
				nil,
			)
			if err != nil {
				t.Fatalf("add PostgreSQL %s account: %v", major, err)
			}

			gateway := startDatabaseGateway(t, fixture, "postgresql", testLogger())
			compactUsername := util.PrefixDatabase + account.ResourceID + fixture.session.SessionID
			applicationName := "jianmen-compat-pg" + major
			dsn := postgresCompatGatewayDSN(
				gateway,
				compactUsername,
				applicationName,
				nil,
			)

			postgresCompatVerifyDatabaseSQL(t, dsn, major, applicationName)
			postgresCompatVerifySimpleProtocol(t, gateway, compactUsername)
			postgresCompatVerifyCopy(t, dsn)
			postgresCompatVerifyContextCancel(t, dsn)
			if major == "18" {
				postgresCompatVerifyGatewayTLSFailures(
					t,
					gateway,
					compactUsername,
				)
			}
			if major == "17" || major == "18" {
				protocolMinor := uint32(0)
				if major == "18" {
					protocolMinor = 2
				}
				postgresCompatVerifyDirectTLS(
					t,
					gateway,
					compactUsername,
					major,
					protocolMinor,
				)
			}
			assertDBAuditSQLContains(
				t,
				fixture.replayDir,
				"SELECT [REDACTED] AS audit_probe",
			)
		})
	}
}

func TestPostgresCompatImagesEmptyOverrideUsesFullMatrix(t *testing.T) {
	t.Setenv("JIANMEN_POSTGRES_IMAGES", " , , ")
	got := strings.Join(postgresCompatImages(), ",")
	want := strings.Join(postgresCompatDefaultImages(), ",")
	if got != want {
		t.Fatalf("empty PostgreSQL image override = %q, want %q", got, want)
	}
}

func postgresCompatVerifyDatabaseSQL(
	t *testing.T,
	dsn, major, applicationName string,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL %s through gateway: %v", major, err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		t.Fatalf("ping PostgreSQL %s through gateway: %v", major, err)
	}

	var serverVersion, actualApplicationName string
	if err := database.QueryRowContext(
		ctx,
		"SELECT current_setting('server_version_num'), current_setting('application_name')",
	).Scan(&serverVersion, &actualApplicationName); err != nil {
		t.Fatalf("read PostgreSQL %s startup settings: %v", major, err)
	}
	if !strings.HasPrefix(serverVersion, major) {
		t.Fatalf("PostgreSQL server_version_num = %q, want major %s", serverVersion, major)
	}
	if actualApplicationName != applicationName {
		t.Fatalf("PostgreSQL application_name = %q, want %q", actualApplicationName, applicationName)
	}

	statement, err := database.PrepareContext(ctx, "SELECT $1::integer + 1")
	if err != nil {
		t.Fatalf("prepare PostgreSQL %s extended query: %v", major, err)
	}
	var preparedResult int
	if err := statement.QueryRowContext(ctx, 41).Scan(&preparedResult); err != nil {
		_ = statement.Close()
		t.Fatalf("execute PostgreSQL %s prepared query: %v", major, err)
	}
	if err := statement.Close(); err != nil {
		t.Fatalf("close PostgreSQL %s prepared query: %v", major, err)
	}
	if preparedResult != 42 {
		t.Fatalf("PostgreSQL %s prepared result = %d, want 42", major, preparedResult)
	}

	if _, err := database.ExecContext(
		ctx,
		"CREATE TEMP TABLE postgres_compat_tx (id integer PRIMARY KEY)",
	); err != nil {
		t.Fatalf("create PostgreSQL %s transaction table: %v", major, err)
	}
	commit, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin PostgreSQL %s commit transaction: %v", major, err)
	}
	if _, err := commit.ExecContext(ctx, "INSERT INTO postgres_compat_tx VALUES ($1)", 1); err != nil {
		_ = commit.Rollback()
		t.Fatalf("insert PostgreSQL %s committed row: %v", major, err)
	}
	if err := commit.Commit(); err != nil {
		t.Fatalf("commit PostgreSQL %s transaction: %v", major, err)
	}
	rollback, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin PostgreSQL %s rollback transaction: %v", major, err)
	}
	if _, err := rollback.ExecContext(ctx, "INSERT INTO postgres_compat_tx VALUES ($1)", 2); err != nil {
		_ = rollback.Rollback()
		t.Fatalf("insert PostgreSQL %s rolled-back row: %v", major, err)
	}
	if err := rollback.Rollback(); err != nil {
		t.Fatalf("rollback PostgreSQL %s transaction: %v", major, err)
	}
	var transactionRows int
	if err := database.QueryRowContext(
		ctx,
		"SELECT count(*) FROM postgres_compat_tx",
	).Scan(&transactionRows); err != nil {
		t.Fatalf("count PostgreSQL %s transaction rows: %v", major, err)
	}
	if transactionRows != 1 {
		t.Fatalf("PostgreSQL %s transaction row count = %d, want 1", major, transactionRows)
	}

	var largeValue string
	if err := database.QueryRowContext(
		ctx,
		"SELECT repeat('x', 300000)",
	).Scan(&largeValue); err != nil {
		t.Fatalf("read PostgreSQL %s large DataRow: %v", major, err)
	}
	if len(largeValue) != 300000 {
		t.Fatalf("PostgreSQL %s large DataRow size = %d", major, len(largeValue))
	}

	if _, err := database.ExecContext(ctx, "SELECT * FROM postgres_compat_missing_table"); err == nil {
		t.Fatalf("PostgreSQL %s missing-table query unexpectedly succeeded", major)
	}
	var afterError int
	if err := database.QueryRowContext(ctx, "SELECT 1").Scan(&afterError); err != nil || afterError != 1 {
		t.Fatalf("PostgreSQL %s connection did not recover after ErrorResponse: %d, %v", major, afterError, err)
	}
	var auditProbe int
	if err := database.QueryRowContext(
		ctx,
		"SELECT 42 AS audit_probe",
	).Scan(&auditProbe); err != nil || auditProbe != 42 {
		t.Fatalf("PostgreSQL %s audit probe = %d, %v", major, auditProbe, err)
	}
}

func postgresCompatVerifySimpleProtocol(
	t *testing.T,
	gateway databaseGatewayEndpoint,
	compactUsername string,
) {
	t.Helper()
	dsn := postgresCompatGatewayDSN(
		gateway,
		compactUsername,
		"jianmen-simple-protocol",
		url.Values{"default_query_exec_mode": {"simple_protocol"}},
	)
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL simple-protocol client: %v", err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var value int
	if err := database.QueryRowContext(ctx, "SELECT 7").Scan(&value); err != nil {
		t.Fatalf("execute PostgreSQL simple Query: %v", err)
	}
	if value != 7 {
		t.Fatalf("PostgreSQL simple Query result = %d", value)
	}
}

func postgresCompatVerifyCopy(t *testing.T, dsn string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect PostgreSQL COPY client: %v", err)
	}
	defer connection.Close(context.Background())
	if _, err := connection.Exec(
		ctx,
		"CREATE TEMP TABLE postgres_compat_copy (id integer, payload text)",
	); err != nil {
		t.Fatalf("create PostgreSQL COPY table: %v", err)
	}
	largePayload := strings.Repeat("x", 300000)
	rows, err := connection.CopyFrom(
		ctx,
		pgx.Identifier{"postgres_compat_copy"},
		[]string{"id", "payload"},
		pgx.CopyFromRows([][]any{{1, "small"}, {2, largePayload}}),
	)
	if err != nil {
		t.Fatalf("PostgreSQL CopyFrom: %v", err)
	}
	if rows != 2 {
		t.Fatalf("PostgreSQL CopyFrom rows = %d", rows)
	}
	var output bytes.Buffer
	tag, err := connection.PgConn().CopyTo(
		ctx,
		&output,
		"COPY (SELECT payload FROM postgres_compat_copy ORDER BY id) TO STDOUT",
	)
	if err != nil {
		t.Fatalf("PostgreSQL CopyTo: %v", err)
	}
	if tag.RowsAffected() != 2 || output.Len() < len(largePayload) {
		t.Fatalf("PostgreSQL CopyTo tag/output = %s/%d", tag, output.Len())
	}
}

func postgresCompatVerifyContextCancel(t *testing.T, dsn string) {
	t.Helper()
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse PostgreSQL cancel client config: %v", err)
	}
	config.BuildContextWatcherHandler = func(connection *pgconn.PgConn) ctxwatch.Handler {
		return &pgconn.CancelRequestContextWatcherHandler{
			Conn: connection, CancelRequestDelay: 25 * time.Millisecond,
			DeadlineDelay: 5 * time.Second,
		}
	}
	connection, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("connect PostgreSQL cancel client: %v", err)
	}
	defer connection.Close(context.Background())

	queryContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	_, err = connection.Exec(queryContext, "SELECT pg_sleep(30)")
	elapsed := time.Since(started)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "57014" {
		t.Fatalf("PostgreSQL canceled query error = %T %v, want SQLSTATE 57014", err, err)
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("PostgreSQL CancelRequest took %s, upstream pg_sleep was not canceled promptly", elapsed)
	}
	var value int
	if err := connection.QueryRow(context.Background(), "SELECT 1").Scan(&value); err != nil {
		t.Fatalf("PostgreSQL connection unusable after CancelRequest: %v", err)
	}
	if value != 1 {
		t.Fatalf("PostgreSQL post-cancel query result = %d", value)
	}
}

func postgresCompatVerifyGatewayTLSFailures(
	t *testing.T,
	gateway databaseGatewayEndpoint,
	compactUsername string,
) {
	t.Helper()
	extra := url.Values{"connect_timeout": {"5"}}
	_, _, wrongCAFile := writeIntegrationTLSCertificate(t)
	wrongCA := gateway
	wrongCA.caFile = wrongCAFile
	assertPostgresGatewayTLSFailure(
		t,
		postgresCompatGatewayDSN(
			wrongCA,
			compactUsername,
			"jianmen-wrong-ca",
			extra,
		),
		"wrong CA",
		postgresTLSUnknownAuthority,
	)

	_, port, err := net.SplitHostPort(gateway.address)
	if err != nil {
		t.Fatalf("split PostgreSQL gateway address: %v", err)
	}
	wrongHostname := gateway
	wrongHostname.address = net.JoinHostPort("localhost", port)
	assertPostgresGatewayTLSFailure(
		t,
		postgresCompatGatewayDSN(
			wrongHostname,
			compactUsername,
			"jianmen-wrong-hostname",
			extra,
		),
		"wrong hostname",
		postgresTLSHostname,
	)
}

func postgresCompatVerifyDirectTLS(
	t *testing.T,
	gateway databaseGatewayEndpoint,
	compactUsername string,
	serverMajor string,
	protocolMinor uint32,
) {
	t.Helper()
	caPEM, err := os.ReadFile(gateway.caFile)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("load PostgreSQL gateway CA")
	}
	raw, err := net.DialTimeout("tcp", gateway.address, 5*time.Second)
	if err != nil {
		t.Fatalf("dial PostgreSQL direct TLS: %v", err)
	}
	secured := tls.Client(raw, &tls.Config{
		RootCAs: roots, ServerName: "127.0.0.1",
		NextProtos: []string{"postgresql"}, MinVersion: tls.VersionTLS12,
	})
	defer secured.Close()
	if err := secured.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := secured.Handshake(); err != nil {
		t.Fatalf("PostgreSQL direct TLS handshake: %v", err)
	}
	if secured.ConnectionState().NegotiatedProtocol != "postgresql" {
		t.Fatalf("PostgreSQL direct TLS ALPN = %q", secured.ConnectionState().NegotiatedProtocol)
	}

	parameters := [][2]string{
		{"user", compactUsername},
		{"database", postgresCompatDatabase},
		{"application_name", "jianmen-direct-pg" + serverMajor},
	}
	if protocolMinor == 2 {
		parameters = append(parameters, [2]string{"_pq_.jianmen_test", "1"})
	}
	startup := postgresCompatStartupMessage(3, protocolMinor, parameters)
	if err := postgresCompatWriteAll(secured, startup); err != nil {
		t.Fatal(err)
	}
	messageType, payload, err := postgresCompatReadMessage(secured)
	if err != nil {
		t.Fatal(err)
	}
	if protocolMinor == 2 {
		if messageType != 'v' || len(payload) < 8 ||
			binary.BigEndian.Uint32(payload[:4]) != 0 ||
			binary.BigEndian.Uint32(payload[4:8]) != 1 {
			t.Fatalf("PostgreSQL protocol negotiation = %q %x", messageType, payload)
		}
		messageType, payload, err = postgresCompatReadMessage(secured)
		if err != nil {
			t.Fatal(err)
		}
	}
	if messageType != 'R' || len(payload) != 4 || binary.BigEndian.Uint32(payload) != 3 {
		t.Fatalf("PostgreSQL gateway auth challenge = %q %x", messageType, payload)
	}
	if err := postgresCompatWriteMessage(
		secured,
		'p',
		append([]byte(integrationPassword), 0),
	); err != nil {
		t.Fatal(err)
	}
	sawAuthOK := false
	sawBackendKey := false
	for {
		messageType, payload, err = postgresCompatReadMessage(secured)
		if err != nil {
			t.Fatal(err)
		}
		switch messageType {
		case 'R':
			sawAuthOK = len(payload) == 4 && binary.BigEndian.Uint32(payload) == 0
		case 'K':
			sawBackendKey = len(payload) == 8
		case 'E':
			t.Fatalf("PostgreSQL direct TLS startup failed: %x", payload)
		case 'Z':
			if !sawAuthOK || !sawBackendKey {
				t.Fatalf("PostgreSQL direct TLS startup auth/key = %t/%t", sawAuthOK, sawBackendKey)
			}
			goto query
		}
	}

query:
	if err := postgresCompatWriteMessage(secured, 'Q', []byte("SELECT 32\x00")); err != nil {
		t.Fatal(err)
	}
	got := ""
	for {
		messageType, payload, err = postgresCompatReadMessage(secured)
		if err != nil {
			t.Fatal(err)
		}
		if messageType == 'D' && len(payload) >= 6 &&
			binary.BigEndian.Uint16(payload[:2]) == 1 {
			length := int(binary.BigEndian.Uint32(payload[2:6]))
			if length >= 0 && len(payload) == 6+length {
				got = string(payload[6:])
			}
		}
		if messageType == 'E' {
			t.Fatalf("PostgreSQL direct TLS query failed: %x", payload)
		}
		if messageType == 'Z' {
			break
		}
	}
	if got != "32" {
		t.Fatalf("PostgreSQL direct TLS query result = %q", got)
	}
	_ = postgresCompatWriteMessage(secured, 'X', nil)
}

func postgresCompatGatewayDSN(
	gateway databaseGatewayEndpoint,
	compactUsername, applicationName string,
	extra url.Values,
) string {
	values := url.Values{
		"sslmode":          {"verify-full"},
		"sslrootcert":      {gateway.caFile},
		"application_name": {applicationName},
	}
	for key, entries := range extra {
		for _, entry := range entries {
			values.Add(key, entry)
		}
	}
	return fmt.Sprintf(
		"postgres://%s:%s@%s/%s?%s",
		url.QueryEscape(compactUsername),
		url.QueryEscape(integrationPassword),
		gateway.address,
		postgresCompatDatabase,
		values.Encode(),
	)
}

func postgresCompatImages() []string {
	raw := strings.TrimSpace(os.Getenv("JIANMEN_POSTGRES_IMAGES"))
	if raw == "" {
		return postgresCompatDefaultImages()
	}
	var images []string
	for _, value := range strings.Split(raw, ",") {
		if image := strings.TrimSpace(value); image != "" {
			images = append(images, image)
		}
	}
	if len(images) == 0 {
		return postgresCompatDefaultImages()
	}
	return images
}

func postgresCompatDefaultImages() []string {
	return []string{
		"postgres:14-alpine",
		"postgres:15-alpine",
		"postgres:16-alpine",
		"postgres:17-alpine",
		"postgres:18-alpine",
	}
}

func postgresCompatImageMajor(t *testing.T, image string) string {
	t.Helper()
	version := strings.TrimPrefix(image, "postgres:")
	version = strings.SplitN(version, "-", 2)[0]
	version = strings.SplitN(version, ".", 2)[0]
	switch version {
	case "14", "15", "16", "17", "18":
		return version
	default:
		t.Fatalf("unsupported PostgreSQL compatibility image %q", image)
		return ""
	}
}

func postgresCompatStartupMessage(
	major, minor uint32,
	parameters [][2]string,
) []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, major<<16|minor)
	for _, parameter := range parameters {
		payload = append(payload, parameter[0]...)
		payload = append(payload, 0)
		payload = append(payload, parameter[1]...)
		payload = append(payload, 0)
	}
	payload = append(payload, 0)
	message := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(message[:4], uint32(len(message)))
	copy(message[4:], payload)
	return message
}

func postgresCompatReadMessage(connection net.Conn) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(connection, header[:]); err != nil {
		return 0, nil, err
	}
	length := int(binary.BigEndian.Uint32(header[1:]))
	if length < 4 || length > 16*1024*1024 {
		return 0, nil, fmt.Errorf("invalid PostgreSQL message length %d", length)
	}
	payload := make([]byte, length-4)
	if _, err := io.ReadFull(connection, payload); err != nil {
		return 0, nil, err
	}
	return header[0], payload, nil
}

func postgresCompatWriteMessage(connection net.Conn, kind byte, payload []byte) error {
	message := make([]byte, 5+len(payload))
	message[0] = kind
	binary.BigEndian.PutUint32(message[1:5], uint32(4+len(payload)))
	copy(message[5:], payload)
	return postgresCompatWriteAll(connection, message)
}

func postgresCompatWriteAll(connection net.Conn, message []byte) error {
	for len(message) > 0 {
		written, err := connection.Write(message)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(message) {
			return io.ErrShortWrite
		}
		message = message[written:]
	}
	return nil
}
