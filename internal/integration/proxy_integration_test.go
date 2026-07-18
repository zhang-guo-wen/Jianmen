//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"jianmen/internal/config"
	"jianmen/internal/crypto"
	"jianmen/internal/model"
	"jianmen/internal/online"
	"jianmen/internal/rbac"
	"jianmen/internal/server/dbproxy"
	"jianmen/internal/server/sshserver"
	"jianmen/internal/service"
	"jianmen/internal/storage"
	jmstore "jianmen/internal/store"
	"jianmen/internal/util"
)

const (
	integrationUserID   = "integration-admin"
	integrationUsername = "integration-admin"
	integrationPassword = "integration-password"
)

func init() {
	_ = mysqlDriver.SetLogger(&mysqlDriver.NopLogger{})
}

type metadataFixture struct {
	db        *gorm.DB
	store     *jmstore.DBStore
	session   model.UserSession
	dataDir   string
	replayDir string
}

func TestSSHProxyAgainstDockerOpenSSH(t *testing.T) {
	requireDocker(t)

	const targetUser = "app"
	const targetPassword = "target-password"
	containerID := runContainer(t, "jianmen-it-ssh",
		"-e", "PUID=1000",
		"-e", "PGID=1000",
		"-e", "TZ=Etc/UTC",
		"-e", "USER_NAME="+targetUser,
		"-e", "USER_PASSWORD="+targetPassword,
		"-e", "PASSWORD_ACCESS=true",
		"-e", "SUDO_ACCESS=false",
		"-p", "127.0.0.1::2222",
		"lscr.io/linuxserver/openssh-server:latest",
	)
	targetAddr := containerAddress(t, containerID, "2222/tcp")
	waitSSHPassword(t, targetAddr, targetUser, targetPassword, 2*time.Minute)

	fixture := newMetadataFixture(t)
	targetHost, targetPort := splitAddress(t, targetAddr)
	target, err := fixture.store.AddTarget(config.Target{
		HostID:                "docker-openssh",
		Host:                  targetHost,
		Port:                  targetPort,
		Username:              targetUser,
		Password:              targetPassword,
		InsecureIgnoreHostKey: true,
	})
	if err != nil {
		t.Fatalf("add ssh target: %v", err)
	}

	bastionAddr := startSSHServer(t, fixture)
	compactUsername := util.PrefixHost + target.ResourceID + fixture.session.SessionID
	client, err := ssh.Dial("tcp", bastionAddr, &ssh.ClientConfig{
		User:            compactUsername,
		Auth:            []ssh.AuthMethod{ssh.Password(integrationPassword)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial bastion ssh: %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("new ssh session: %v", err)
	}
	output, err := session.CombinedOutput("printf JIANMEN_SSH_OK")
	if err != nil {
		t.Fatalf("run remote command: %v output=%q", err, string(output))
	}
	if !strings.Contains(string(output), "JIANMEN_SSH_OK") {
		t.Fatalf("unexpected ssh output %q", string(output))
	}
	assertSSHAuditContains(t, fixture.replayDir, "JIANMEN_SSH_OK")
}

func TestDatabaseGatewayMySQLAgainstDocker(t *testing.T) {
	requireDocker(t)

	for _, mysqlImage := range mysqlImages() {
		mysqlImage := mysqlImage
		t.Run(sanitizeTestName(mysqlImage), func(t *testing.T) {
			const upstreamUser = "app"
			const upstreamPassword = "app-password"
			const auditSQL = "SELECT 42 AS audit_probe"

			containerArgs := []string{
				"-e", "MYSQL_ROOT_PASSWORD=root-password",
				"-e", "MYSQL_DATABASE=appdb",
				"-e", "MYSQL_USER=" + upstreamUser,
				"-e", "MYSQL_PASSWORD=" + upstreamPassword,
				"-p", "127.0.0.1::3306",
				mysqlImage,
			}
			containerArgs = append(containerArgs, mysqlServerArgs(mysqlImage)...)
			containerID := runContainer(t, "jianmen-it-mysql", containerArgs...)
			upstreamAddr := containerAddress(t, containerID, "3306/tcp")
			waitMySQL(t, upstreamAddr, upstreamUser, upstreamPassword)

			fixture := newMetadataFixture(t)
			host, port := splitAddress(t, upstreamAddr)
			instance, err := fixture.store.AddDatabaseInstance("docker-mysql-"+sanitizeTestName(mysqlImage), "mysql", host, port, "", "")
			if err != nil {
				t.Fatalf("add mysql instance: %v", err)
			}
			account, err := fixture.store.AddDatabaseAccount(instance.ID, upstreamUser, upstreamPassword, "", "", nil)
			if err != nil {
				t.Fatalf("add mysql account: %v", err)
			}

			gatewayAddr := startDatabaseGateway(t, fixture)
			compactUsername := util.PrefixDatabase + account.ResourceID + fixture.session.SessionID
			clientDB, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s)/appdb?timeout=3s&readTimeout=3s&writeTimeout=3s", compactUsername, integrationPassword, gatewayAddr))
			if err != nil {
				t.Fatalf("open mysql through gateway: %v", err)
			}

			var got int
			if err := clientDB.QueryRow(auditSQL).Scan(&got); err != nil {
				_ = clientDB.Close()
				t.Fatalf("query mysql through gateway: %v", err)
			}
			if got != 42 {
				_ = clientDB.Close()
				t.Fatalf("mysql query result = %d, want 42", got)
			}
			if err := clientDB.Close(); err != nil {
				t.Fatalf("close mysql client: %v", err)
			}
			assertDBAuditContains(t, fixture.replayDir, auditSQL)
		})
	}
}

func TestDatabaseGatewayPostgresAgainstDocker(t *testing.T) {
	requireDocker(t)

	const upstreamUser = "app"
	containerID := runContainer(t, "jianmen-it-postgres",
		"-e", "POSTGRES_USER="+upstreamUser,
		"-e", "POSTGRES_DB="+upstreamUser,
		"-e", "POSTGRES_HOST_AUTH_METHOD=trust",
		"-p", "127.0.0.1::5432",
		"postgres:16-alpine",
	)
	upstreamAddr := containerAddress(t, containerID, "5432/tcp")
	waitPostgres(t, upstreamAddr, upstreamUser)

	fixture := newMetadataFixture(t)
	host, port := splitAddress(t, upstreamAddr)
	instance, err := fixture.store.AddDatabaseInstance("docker-postgres", "postgres", host, port, "", "")
	if err != nil {
		t.Fatalf("add postgres instance: %v", err)
	}
	account, err := fixture.store.AddDatabaseAccount(instance.ID, upstreamUser, "unused-by-trust-auth", "", "", nil)
	if err != nil {
		t.Fatalf("add postgres account: %v", err)
	}

	gatewayAddr := startDatabaseGateway(t, fixture)
	compactUsername := util.PrefixDatabase + account.ResourceID + fixture.session.SessionID
	dsn := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable",
		url.QueryEscape(compactUsername),
		url.QueryEscape(integrationPassword),
		gatewayAddr,
		upstreamUser,
	)
	clientDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres through gateway: %v", err)
	}
	defer clientDB.Close()

	var got int
	if err := clientDB.QueryRow("SELECT 1").Scan(&got); err != nil {
		t.Fatalf("query postgres through gateway: %v", err)
	}
	if got != 1 {
		t.Fatalf("postgres query result = %d, want 1", got)
	}
	if err := clientDB.Close(); err != nil {
		t.Fatalf("close postgres client: %v", err)
	}
	assertDBAuditContains(t, fixture.replayDir, "select 1")
}

func mysqlImages() []string {
	raw := strings.TrimSpace(os.Getenv("JIANMEN_MYSQL_IMAGES"))
	if raw == "" {
		return []string{"mysql:5.7", "mysql:8.0"}
	}
	parts := strings.Split(raw, ",")
	images := make([]string, 0, len(parts))
	for _, part := range parts {
		if image := strings.TrimSpace(part); image != "" {
			images = append(images, image)
		}
	}
	if len(images) == 0 {
		return []string{"mysql:5.7", "mysql:8.0"}
	}
	return images
}

func mysqlServerArgs(image string) []string {
	if strings.Contains(image, ":8.0") {
		return []string{"--default-authentication-plugin=mysql_native_password"}
	}
	return nil
}

func newMetadataFixture(t *testing.T) metadataFixture {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	replayDir := filepath.Join(root, "replay")
	if _, err := crypto.Init(dataDir); err != nil {
		t.Fatalf("crypto init: %v", err)
	}

	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    filepath.Join(root, "metadata.db"),
	})
	if err != nil {
		t.Fatalf("open metadata db: %v", err)
	}
	db = db.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get metadata sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate metadata: %v", err)
	}
	if err := storage.BootstrapMetadata(db, &config.Config{
		Users: []config.User{{
			ID:         integrationUserID,
			Username:   integrationUsername,
			Password:   integrationPassword,
			SuperAdmin: true,
		}},
	}); err != nil {
		t.Fatalf("bootstrap metadata: %v", err)
	}

	var session model.UserSession
	if err := db.Where("user_id = ? AND type = ? AND status = ?", integrationUserID, "permanent", "active").
		First(&session).Error; err != nil {
		t.Fatalf("load permanent user session: %v", err)
	}
	return metadataFixture{
		db:        db,
		store:     jmstore.NewDBStore(db),
		session:   session,
		dataDir:   dataDir,
		replayDir: replayDir,
	}
}

func startDatabaseGateway(t *testing.T, fixture metadataFixture) string {
	t.Helper()
	addr := freeTCPAddress(t)
	authorizer := newIntegrationAuthorizer(t, fixture)
	gateway := dbproxy.NewGateway(
		config.DatabaseGatewayConfig{Enabled: true, ListenAddr: addr},
		nil,
		fixture.replayDir,
		testLogger(),
		fixture.db,
		authorizer,
		online.NewRegistry(),
		nil,
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
	return addr
}

func startSSHServer(t *testing.T, fixture metadataFixture) string {
	t.Helper()
	addr := freeTCPAddress(t)
	cfg := &config.Config{
		ListenAddr:  addr,
		HostKeyPath: filepath.Join(fixture.dataDir, "host_key"),
		ReplayDir:   fixture.replayDir,
		Recording: config.RecordingConfig{
			Enabled:        true,
			RecordCommands: true,
		},
		Users: []config.User{{
			ID:         integrationUserID,
			Username:   integrationUsername,
			SuperAdmin: true,
		}},
	}
	server, err := sshserver.New(cfg, fixture.store, newIntegrationAuthorizer(t, fixture), testLogger(), online.NewRegistry())
	if err != nil {
		t.Fatalf("new ssh server: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("ssh server stopped with error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("ssh server did not stop")
		}
	})
	waitServerTCP(t, addr, errCh)
	return addr
}

func newIntegrationAuthorizer(t *testing.T, fixture metadataFixture) *service.AuthorizationService {
	t.Helper()
	identity, err := service.NewIdentityService(fixture.store)
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}
	authorizer, err := service.NewAuthorizationService(
		identity,
		rbac.NewChecker(fixture.db),
		rbac.NewResourceGrantChecker(fixture.db),
	)
	if err != nil {
		t.Fatalf("new authorization service: %v", err)
	}
	return authorizer
}

func waitServerTCP(t *testing.T, addr string, errCh <-chan error) {
	t.Helper()
	waitFor(t, 5*time.Second, 100*time.Millisecond, func() error {
		select {
		case err := <-errCh:
			if err == nil {
				return fmt.Errorf("server exited")
			}
			return fmt.Errorf("server exited: %w", err)
		default:
		}
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			return err
		}
		return conn.Close()
	})
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

func waitPostgres(t *testing.T, addr, username string) {
	t.Helper()
	dsn := fmt.Sprintf("postgres://%s@%s/%s?sslmode=disable", url.QueryEscape(username), addr, username)
	waitFor(t, 2*time.Minute, time.Second, func() error {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return err
		}
		defer db.Close()
		return db.Ping()
	})
}

func waitSSHPassword(t *testing.T, addr, username, password string, timeout time.Duration) {
	t.Helper()
	waitFor(t, timeout, time.Second, func() error {
		client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
			User:            username,
			Auth:            []ssh.AuthMethod{ssh.Password(password)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         3 * time.Second,
		})
		if err != nil {
			return err
		}
		return client.Close()
	})
}

func assertDBAuditContains(t *testing.T, replayDir, query string) {
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
			if strings.Contains(strings.ToLower(string(raw)), query) {
				return nil
			}
		}
		return fmt.Errorf("query %q not found in %d db audit files", query, len(paths))
	})
}

func assertSSHAuditContains(t *testing.T, replayDir, expectedOutput string) {
	t.Helper()
	waitFor(t, 5*time.Second, 100*time.Millisecond, func() error {
		paths, err := filepath.Glob(filepath.Join(replayDir, "ssh", "*", "terminal.cast"))
		if err != nil {
			return err
		}
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(raw), expectedOutput) {
				metaPath := filepath.Join(filepath.Dir(path), "meta.json")
				metaRaw, err := os.ReadFile(metaPath)
				if err != nil {
					return err
				}
				if !strings.Contains(string(metaRaw), `"protocol": "ssh"`) {
					return fmt.Errorf("ssh meta missing protocol: %s", string(metaRaw))
				}
				return nil
			}
		}
		return fmt.Errorf("ssh output %q not found in %d casts", expectedOutput, len(paths))
	})
}

func splitAddress(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split address %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse port %q: %v", portText, err)
	}
	return host, port
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

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn}))
}
