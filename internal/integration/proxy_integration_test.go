//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"jianmen/internal/config"
	"jianmen/internal/crypto"
	"jianmen/internal/model"
	"jianmen/internal/online"
	"jianmen/internal/rbac"
	"jianmen/internal/server/sshserver"
	"jianmen/internal/service"
	"jianmen/internal/sshhost"
	"jianmen/internal/storage"
	jmstore "jianmen/internal/store"
	"jianmen/internal/util"
)

const (
	integrationUserID   = "integration-admin"
	integrationUsername = "integration-admin"
	integrationPassword = "integration-password"
)

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
	identity, err := sshhost.NewCollector(10*time.Second).Collect(context.Background(), targetHost, targetPort)
	if err != nil {
		t.Fatalf("collect ssh target identity: %v", err)
	}
	if _, err := fixture.store.AddHost(context.Background(), jmstore.HostRecord{
		ID: "docker-openssh", Name: "docker-openssh", Address: targetHost, Port: targetPort,
		Protocol: "ssh", Status: "active",
		HostKeyFingerprint: identity.Fingerprint, KnownHosts: identity.KnownHosts,
	}); err != nil {
		t.Fatalf("add ssh host: %v", err)
	}
	target, err := fixture.store.AddTarget(context.Background(), config.Target{
		HostID:   "docker-openssh",
		Host:     targetHost,
		Port:     targetPort,
		Username: targetUser,
		Password: targetPassword,
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

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn}))
}
