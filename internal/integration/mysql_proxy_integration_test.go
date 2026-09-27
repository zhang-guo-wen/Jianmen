//go:build integration

package integration

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"jianmen/internal/config"
	"jianmen/internal/online"
	"jianmen/internal/server/dbproxy"
	jmstore "jianmen/internal/store"
	"jianmen/internal/util"
)

func TestMySQLProxyCompatibilityAgainstDocker(t *testing.T) {
	requireDocker(t)

	for _, image := range mysqlCompatibilityImages() {
		image := image
		t.Run(sanitizeTestName(image), func(t *testing.T) {
			const upstreamUser = "app"
			const upstreamPassword = "app-password"

			containerArgs := []string{
				"-e", "MYSQL_ROOT_PASSWORD=root-password",
				"-e", "MYSQL_ROOT_HOST=%",
				"-e", "MYSQL_DATABASE=appdb",
				"-e", "MYSQL_USER=" + upstreamUser,
				"-e", "MYSQL_PASSWORD=" + upstreamPassword,
				"-p", "127.0.0.1::3306",
			}
			tlsMode := "disable"
			var tlsCAPEM *string
			if strings.Contains(image, ":5.7") {
				containerArgs = append(containerArgs, image, "--max-allowed-packet=64M")
			} else {
				certificateDirectory, caFile := writeMySQLIntegrationTLSFiles(t)
				containerArgs = append(
					[]string{"-v", dockerBindPath(t, certificateDirectory) + ":/certs:ro"},
					containerArgs...,
				)
				containerArgs = append(
					containerArgs,
					image,
					"--max-allowed-packet=64M",
					"--ssl-cert=/certs/server.crt",
					"--ssl-key=/certs/server.key",
				)
				caPEM := string(readIntegrationFile(t, caFile))
				tlsMode = "verify-ca"
				tlsCAPEM = &caPEM
			}

			containerID := runContainer(t, "jianmen-it-mysql-compat", containerArgs...)
			upstreamAddress := containerAddress(t, containerID, "3306/tcp")
			waitMySQL(t, upstreamAddress, "root", "root-password")
			assertMySQLCompatibilityServer(t, upstreamAddress, upstreamUser, image)

			fixture := newMetadataFixture(t)
			host, port := splitAddress(t, upstreamAddress)
			instance, err := fixture.store.AddDatabaseInstance(context.Background(), jmstore.DatabaseInstanceInput{
				Name:     "mysql-compat-" + sanitizeTestName(image),
				Protocol: "mysql",
				Address:  host,
				Port:     port,
				TLSMode:  tlsMode,
				TLSCAPEM: tlsCAPEM,
			})
			if err != nil {
				t.Fatalf("add MySQL compatibility instance: %v", err)
			}
			account, err := fixture.store.AddDatabaseAccount(
				context.Background(),
				instance.ID,
				upstreamUser,
				upstreamPassword,
				"",
				"",
				nil,
			)
			if err != nil {
				t.Fatalf("add MySQL compatibility account: %v", err)
			}

			for _, mode := range databaseGatewayModes() {
				mode := mode
				t.Run(mode, func(t *testing.T) {
					gatewayLogger, gatewayLogs := databaseGatewayDebugLogger()
					gateway := startMySQLCompatibilityGateway(t, fixture, gatewayLogger, mode)
					clientTLSName := registerMySQLCompatibilityTLS(t, gateway.caFile)
					compactUsername := util.PrefixDatabase + account.ResourceID + fixture.session.SessionID
					clientConfig := mysqlDriver.Config{
						User:                 compactUsername,
						Passwd:               integrationPassword,
						Net:                  "tcp",
						Addr:                 gateway.address,
						DBName:               "appdb",
						Timeout:              3 * time.Second,
						ReadTimeout:          30 * time.Second,
						WriteTimeout:         10 * time.Second,
						TLSConfig:            clientTLSName,
						AllowNativePasswords: true,
					}
					client, err := sql.Open("mysql", clientConfig.FormatDSN())
					if err != nil {
						t.Fatalf("open MySQL compatibility client: %v", err)
					}
					t.Cleanup(func() {
						if err := client.Close(); err != nil {
							t.Errorf("close MySQL compatibility client: %v", err)
						}
					})
					client.SetMaxOpenConns(1)

					assertMySQLCompatibilityLifecycle(t, client)
					assertMySQLFragmentedResponse(t, client)
					if !strings.Contains(image, ":5.7") {
						assertMySQLCompatibilityFullAuth(t, gatewayLogs.String(), upstreamPassword)
					}
					assertDBAuditSQLContains(t, fixture.replayDir, "SELECT [REDACTED] AS audit_probe")
				})
			}
		})
	}
}

type mysqlCompatibilityGateway struct {
	address string
	caFile  string
}

func startMySQLCompatibilityGateway(
	t *testing.T,
	fixture metadataFixture,
	logger *slog.Logger,
	mode string,
) mysqlCompatibilityGateway {
	t.Helper()
	address := freeTCPAddress(t)
	certFile, keyFile, caFile := writeIntegrationTLSCertificate(t)
	listener := config.DatabaseProtocolListener{
		Enabled:    true,
		Address:    address,
		CertFile:   certFile,
		KeyFile:    keyFile,
		CAFile:     caFile,
		ServerName: "127.0.0.1",
	}
	gatewayConfig := config.DatabaseGatewayConfig{Enabled: true}
	configureDatabaseGatewayMode(t, &gatewayConfig, mode, "mysql", listener)
	gateway := dbproxy.NewGateway(
		gatewayConfig,
		fixture.store,
		fixture.replayDir,
		logger,
		fixture.db,
		newIntegrationAuthorizer(t, fixture),
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
				t.Errorf("MySQL compatibility gateway stopped with error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("MySQL compatibility gateway did not stop")
		}
	})
	waitServerTCP(t, address, errCh)
	return mysqlCompatibilityGateway{address: address, caFile: caFile}
}

func registerMySQLCompatibilityTLS(t *testing.T, caFile string) string {
	t.Helper()
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatalf("read MySQL compatibility gateway CA: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("parse MySQL compatibility gateway CA")
	}
	name := fmt.Sprintf("jianmen_mysql_compat_%d", time.Now().UnixNano())
	if err := mysqlDriver.RegisterTLSConfig(name, &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
		ServerName: "127.0.0.1",
	}); err != nil {
		t.Fatalf("register MySQL compatibility TLS config: %v", err)
	}
	t.Cleanup(func() {
		mysqlDriver.DeregisterTLSConfig(name)
	})
	return name
}

func mysqlCompatibilityImages() []string {
	raw := strings.TrimSpace(os.Getenv("JIANMEN_MYSQL_IMAGES"))
	if raw == "" {
		return []string{"mysql:5.7", "mysql:8.0", "mysql:8.4"}
	}
	var images []string
	for _, value := range strings.Split(raw, ",") {
		if image := strings.TrimSpace(value); image != "" {
			images = append(images, image)
		}
	}
	if len(images) == 0 {
		return []string{"mysql:5.7", "mysql:8.0", "mysql:8.4"}
	}
	return images
}

func assertMySQLCompatibilityServer(t *testing.T, address, username, image string) {
	t.Helper()
	config := mysqlDriver.Config{
		User:                 "root",
		Passwd:               "root-password",
		Net:                  "tcp",
		Addr:                 address,
		DBName:               "mysql",
		Timeout:              3 * time.Second,
		ReadTimeout:          3 * time.Second,
		WriteTimeout:         3 * time.Second,
		AllowNativePasswords: true,
	}
	database, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatalf("open MySQL compatibility metadata connection: %v", err)
	}
	defer database.Close()

	var version string
	if err := database.QueryRow("SELECT VERSION()").Scan(&version); err != nil {
		t.Fatalf("query MySQL compatibility server version: %v", err)
	}
	tag := strings.TrimPrefix(image, "mysql:")
	if !strings.HasPrefix(version, tag) {
		t.Fatalf("MySQL image %s reported version %q", image, version)
	}

	var plugin string
	if err := database.QueryRow(
		"SELECT plugin FROM mysql.user WHERE user = ? LIMIT 1",
		username,
	).Scan(&plugin); err != nil {
		t.Fatalf("query MySQL compatibility authentication plugin: %v", err)
	}
	wantPlugin := "caching_sha2_password"
	if strings.Contains(image, ":5.7") {
		wantPlugin = "mysql_native_password"
	}
	if plugin != wantPlugin {
		t.Fatalf("MySQL image %s authentication plugin = %q, want %q", image, plugin, wantPlugin)
	}
}

func assertMySQLCompatibilityLifecycle(t *testing.T, database *sql.DB) {
	t.Helper()
	var probe int
	if err := database.QueryRow("SELECT 42 AS audit_probe").Scan(&probe); err != nil {
		t.Fatalf("query MySQL compatibility probe: %v", err)
	}
	if probe != 42 {
		t.Fatalf("MySQL compatibility probe = %d, want 42", probe)
	}
	if _, err := database.Exec(
		"CREATE TEMPORARY TABLE jianmen_protocol_matrix (value INT NOT NULL)",
	); err != nil {
		t.Fatalf("create MySQL protocol matrix table: %v", err)
	}

	statement, err := database.Prepare(
		"INSERT INTO jianmen_protocol_matrix (value) VALUES (?)",
	)
	if err != nil {
		t.Fatalf("prepare MySQL insert: %v", err)
	}
	if _, err := statement.Exec(7); err != nil {
		_ = statement.Close()
		t.Fatalf("execute MySQL prepared insert: %v", err)
	}
	if err := statement.Close(); err != nil {
		t.Fatalf("close MySQL prepared insert: %v", err)
	}

	rollback, err := database.Begin()
	if err != nil {
		t.Fatalf("begin MySQL rollback transaction: %v", err)
	}
	if _, err := rollback.Exec(
		"INSERT INTO jianmen_protocol_matrix (value) VALUES (8)",
	); err != nil {
		_ = rollback.Rollback()
		t.Fatalf("execute MySQL rollback insert: %v", err)
	}
	if err := rollback.Rollback(); err != nil {
		t.Fatalf("rollback MySQL transaction: %v", err)
	}

	commit, err := database.Begin()
	if err != nil {
		t.Fatalf("begin MySQL commit transaction: %v", err)
	}
	if _, err := commit.Exec(
		"INSERT INTO jianmen_protocol_matrix (value) VALUES (9)",
	); err != nil {
		_ = commit.Rollback()
		t.Fatalf("execute MySQL commit insert: %v", err)
	}
	if err := commit.Commit(); err != nil {
		t.Fatalf("commit MySQL transaction: %v", err)
	}

	var values string
	if err := database.QueryRow(
		"SELECT GROUP_CONCAT(value ORDER BY value) FROM jianmen_protocol_matrix",
	).Scan(&values); err != nil {
		t.Fatalf("verify MySQL transaction outcome: %v", err)
	}
	if values != "7,9" {
		t.Fatalf("MySQL transaction values = %q, want %q", values, "7,9")
	}
}

func assertMySQLFragmentedResponse(t *testing.T, database *sql.DB) {
	t.Helper()
	const (
		physicalPayloadBytes = 0xFFFFFF
		lengthPrefixBytes    = 9
		responseBytes        = physicalPayloadBytes + 4096
		valueBeforeMarker    = physicalPayloadBytes - lengthPrefixBytes
		valueAfterMarker     = responseBytes - valueBeforeMarker - 1
	)
	var value []byte
	if err := database.QueryRow(
		fmt.Sprintf(
			"SELECT CONCAT(REPEAT('x', %d), UNHEX('FF'), REPEAT('y', %d))",
			valueBeforeMarker,
			valueAfterMarker,
		),
	).Scan(&value); err != nil {
		t.Fatalf("query fragmented MySQL response through gateway: %v", err)
	}
	if len(value) != responseBytes {
		t.Fatalf("fragmented MySQL response length = %d, want %d", len(value), responseBytes)
	}
	if value[valueBeforeMarker] != 0xff {
		t.Fatalf("fragmented MySQL continuation marker = %#x, want 0xff", value[valueBeforeMarker])
	}

	var next int
	if err := database.QueryRow("SELECT 43").Scan(&next); err != nil {
		t.Fatalf("query after fragmented MySQL response: %v", err)
	}
	if next != 43 {
		t.Fatalf("query after fragmented MySQL response = %d, want 43", next)
	}
}

func assertMySQLCompatibilityFullAuth(t *testing.T, logs, password string) {
	t.Helper()
	if !strings.Contains(logs, `"event":"caching_sha2_full_auth"`) {
		t.Fatalf("MySQL caching_sha2 full authentication was not observed: %s", logs)
	}
	if strings.Contains(logs, password) {
		t.Fatalf("MySQL full-auth observation leaked the upstream password: %s", logs)
	}
}
