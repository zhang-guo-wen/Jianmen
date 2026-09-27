package sshserver

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/online"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

const sshCancellationTestTimeout = 2 * time.Second

func TestHandleConnCancellationInterruptsSlowHandshake(t *testing.T) {
	serverSide, clientSide := newSSHTestConnectionPair(t)
	defer clientSide.Close()
	server := &Server{logger: discardSSHTestLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverConfig := newSSHTestServerConfig(t)
	handleDone := make(chan struct{})
	go func() {
		defer close(handleDone)
		server.handleConn(ctx, serverSide, serverConfig)
	}()

	if err := clientSide.SetReadDeadline(time.Now().Add(sshCancellationTestTimeout)); err != nil {
		t.Fatalf("set client read deadline: %v", err)
	}
	if _, err := bufio.NewReader(clientSide).ReadString('\n'); err != nil {
		t.Fatalf("read SSH server banner: %v", err)
	}
	cancel()

	waitForSSHTestSignal(t, handleDone, "slow SSH handshake cancellation")
}

func TestHandleConnCancellationInterruptsEstablishedIdleConnection(t *testing.T) {
	target := startIdleSSHTestTarget(t)
	repository := newCancellationRuntimeRepository(target)
	server := &Server{
		cfg:            &config.Config{},
		targetResolver: repository,
		userSessions:   repository,
		auditSessions:  repository,
		auditEvents:    repository,
		authorizer: &captureConnectionAuthorizer{allow: map[string]bool{
			rbac.ActionSessionConnect: true,
			rbac.ActionSFTPConnect:    true,
		}},
		logger:         discardSSHTestLogger(),
		onlineSessions: online.NewRegistry(),
	}
	serverSide, clientSide := newSSHTestConnectionPair(t)
	defer clientSide.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverConfig := newSSHTestServerConfig(t)
	handleDone := make(chan struct{})
	go func() {
		defer close(handleDone)
		server.handleConn(ctx, serverSide, serverConfig)
	}()

	if err := clientSide.SetDeadline(time.Now().Add(sshCancellationTestTimeout)); err != nil {
		t.Fatalf("set SSH client deadline: %v", err)
	}
	clientConn, channels, requests, err := ssh.NewClientConn(
		clientSide,
		"127.0.0.1",
		&ssh.ClientConfig{
			User:            "operator",
			Auth:            []ssh.AuthMethod{ssh.Password("bastion-password")},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
	)
	if err != nil {
		t.Fatalf("establish idle SSH client: %v", err)
	}
	client := ssh.NewClient(clientConn, channels, requests)
	defer client.Close()
	if err := clientSide.SetDeadline(time.Time{}); err != nil {
		t.Fatalf("clear SSH client deadline: %v", err)
	}
	waitForSSHTestSignal(t, repository.started, "SSH audit session start")

	cancel()

	waitForSSHTestSignal(t, handleDone, "established idle SSH connection cancellation")
	waitForSSHTestSignal(t, repository.ended, "SSH audit session finalization")
}

type cancellationRuntimeRepository struct {
	focusedRuntimeRepository
	target  store.TargetConfig
	started chan struct{}
	ended   chan struct{}
}

func newCancellationRuntimeRepository(target store.TargetConfig) *cancellationRuntimeRepository {
	return &cancellationRuntimeRepository{
		target: target, started: make(chan struct{}), ended: make(chan struct{}),
	}
}

func (r *cancellationRuntimeRepository) DefaultTarget(context.Context, model.User) (store.TargetConfig, error) {
	return r.target, nil
}

func (r *cancellationRuntimeRepository) CreateAuditSession(context.Context, *model.AuditSession) error {
	close(r.started)
	return nil
}

func (r *cancellationRuntimeRepository) EndAuditSession(context.Context, string) error {
	close(r.ended)
	return nil
}

func newSSHTestConnectionPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for SSH test connection: %v", err)
	}
	defer listener.Close()
	clientConn, err := net.DialTimeout("tcp", listener.Addr().String(), sshCancellationTestTimeout)
	if err != nil {
		t.Fatalf("dial SSH test connection: %v", err)
	}
	serverConn, err := listener.Accept()
	if err != nil {
		_ = clientConn.Close()
		t.Fatalf("accept SSH test connection: %v", err)
	}
	return serverConn, clientConn
}

func newSSHTestServerConfig(t *testing.T) *ssh.ServerConfig {
	t.Helper()
	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
			return permissionsForUser(model.User{ID: "user-1", Username: "operator"}), nil
		},
	}
	serverConfig.AddHostKey(newSSHTestSigner(t))
	return serverConfig
}

func newSSHTestSigner(t *testing.T) ssh.Signer {
	t.Helper()
	privateKey, err := generateEd25519HostKey()
	if err != nil {
		t.Fatalf("generate SSH test key: %v", err)
	}
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		t.Fatalf("create SSH test signer: %v", err)
	}
	return signer
}

func startIdleSSHTestTarget(t *testing.T) store.TargetConfig {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for SSH target: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	signer := newSSHTestSigner(t)
	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
			return nil, nil
		},
	}
	serverConfig.AddHostKey(signer)
	go func() {
		defer close(done)
		rawConn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer rawConn.Close()
		go func() {
			<-ctx.Done()
			_ = rawConn.Close()
		}()
		serverConn, channels, requests, handshakeErr := ssh.NewServerConn(rawConn, serverConfig)
		if handshakeErr != nil {
			return
		}
		defer serverConn.Close()
		go ssh.DiscardRequests(requests)
		for channel := range channels {
			_ = channel.Reject(ssh.UnknownChannelType, "test target is idle")
		}
	}()
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		waitForSSHTestSignal(t, done, "idle SSH target shutdown")
	})
	port := listener.Addr().(*net.TCPAddr).Port
	return store.TargetConfig{
		ID: "account-1", HostID: "host-1", HostName: "target-host",
		Host: "127.0.0.1", Port: port, Protocol: "ssh", Username: "target-user",
		Password:           "target-password",
		HostKeyFingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
		KnownHosts:         knownhosts.Line([]string{listener.Addr().String()}, signer.PublicKey()),
	}
}

func waitForSSHTestSignal(t *testing.T, signal <-chan struct{}, operation string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(sshCancellationTestTimeout):
		t.Fatalf("%s did not finish within %s", operation, sshCancellationTestTimeout)
	}
}

func discardSSHTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
