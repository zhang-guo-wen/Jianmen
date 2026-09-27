package dbproxy

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jianmen/internal/config"
)

func TestProtocolListenerStartsOnlyItsProtocol(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	gateway := &Gateway{logger: slog.Default()}
	done := make(chan error, 1)
	go func() {
		done <- gateway.serveProtocolListener(ctx, listener, databaseProtocolMySQL, config.DatabaseProtocolListener{})
	}()

	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("MySQL listener did not send its handshake: %v", err)
	}
	_ = conn.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("listener returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("listener did not stop after context cancellation")
	}
}

func TestProtocolListenerCancellationClosesActiveConnections(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	gateway := &Gateway{logger: slog.Default()}
	done := make(chan error, 1)
	go func() {
		done <- gateway.serveProtocolListener(ctx, listener, databaseProtocolMySQL, config.DatabaseProtocolListener{})
	}()

	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := readMySQLPacket(conn); err != nil {
		t.Fatalf("read MySQL handshake: %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("listener returned %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("listener waited indefinitely for an active connection after cancellation")
	}

	if err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var buffer [1]byte
	if _, err := conn.Read(buffer[:]); err == nil {
		t.Fatal("active client connection remained open after listener cancellation")
	}
}

func TestProtocolConnectionSlowInitialPacketTimesOut(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	gateway := &Gateway{logger: slog.Default()}
	done := make(chan struct{})
	go func() {
		defer close(done)
		gateway.handleProtocolConnectionWithTimeout(
			context.Background(), server, databaseProtocolMySQL, config.DatabaseProtocolListener{}, 50*time.Millisecond,
		)
	}()

	if _, err := readMySQLPacket(client); err != nil {
		t.Fatalf("read MySQL handshake: %v", err)
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("slow MySQL login did not time out")
	}
}

func TestProtocolListenersFailStartupForUnreadableTLSConfiguration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	gateway := &Gateway{
		cfg: config.DatabaseGatewayConfig{
			Enabled: true,
			PostgreSQL: config.DatabaseProtocolListener{
				Enabled:    true,
				Address:    "127.0.0.1:0",
				CertFile:   filepath.Join(t.TempDir(), "missing.crt"),
				KeyFile:    filepath.Join(t.TempDir(), "missing.key"),
				ServerName: "localhost",
			},
		},
		logger: slog.Default(),
	}

	err := gateway.listenAndServeProtocolListeners(ctx)
	if err == nil || !strings.Contains(err.Error(), "load postgresql database listener TLS") {
		t.Fatalf("listenAndServeProtocolListeners() error = %v, want unreadable TLS configuration", err)
	}
}

func TestProtocolListenersReleaseEarlierBindingsWhenLaterBindFails(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	firstReservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	firstAddress := firstReservation.Addr().String()
	if err := firstReservation.Close(); err != nil {
		t.Fatal(err)
	}

	blocked, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blocked.Close()

	gateway := &Gateway{
		cfg: config.DatabaseGatewayConfig{
			Enabled: true,
			MySQL: config.DatabaseProtocolListener{
				Enabled:    true,
				Address:    firstAddress,
				CertFile:   certFile,
				KeyFile:    keyFile,
				ServerName: "localhost",
			},
			Redis: config.DatabaseProtocolListener{
				Enabled:    true,
				Address:    blocked.Addr().String(),
				CertFile:   certFile,
				KeyFile:    keyFile,
				ServerName: "localhost",
			},
		},
		logger: slog.Default(),
	}
	err = gateway.listenAndServeProtocolListeners(context.Background())
	if err == nil || !strings.Contains(err.Error(), "listen redis database gateway") {
		t.Fatalf("listenAndServeProtocolListeners() error = %v, want Redis bind failure", err)
	}

	rebound, err := net.Listen("tcp", firstAddress)
	if err != nil {
		t.Fatalf("earlier MySQL listener was not released after Redis bind failure: %v", err)
	}
	if err := rebound.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProtocolListenerTLSIdentityValidation(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	valid := protocolListener{
		protocol: databaseProtocolMySQL,
		config: config.DatabaseProtocolListener{
			CertFile: certFile, KeyFile: keyFile, ServerName: "localhost",
		},
	}
	if err := validateProtocolListenerTLS(valid); err != nil {
		t.Fatalf("validateProtocolListenerTLS() rejected valid identity: %v", err)
	}

	wrongName := valid
	wrongName.config.ServerName = "other.example.test"
	if err := validateProtocolListenerTLS(wrongName); err == nil {
		t.Fatal("validateProtocolListenerTLS() accepted a certificate with the wrong SAN")
	}
}

func TestPostgresRequiresTLSBeforeCleartextPassword(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	gateway := &Gateway{cfg: config.DatabaseGatewayConfig{
		ClientTLSMode: config.DatabaseGatewayClientTLSModeRequired,
	}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		gateway.handlePostgresConnection(context.Background(), server, config.DatabaseProtocolListener{})
	}()

	startup := make([]byte, 8)
	binary.BigEndian.PutUint32(startup[:4], 8)
	binary.BigEndian.PutUint32(startup[4:], 196608)
	if _, err := client.Write(startup); err != nil {
		t.Fatal(err)
	}
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 128)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("expected TLS-required rejection: %v", err)
	}
	if n == 0 || buf[0] != 'E' {
		t.Fatalf("first response = %q, want PostgreSQL error", buf[0])
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("PostgreSQL handler did not close insecure connection")
	}
}

func TestListenAndServeRequiresIndependentProtocolListener(t *testing.T) {
	gateway := &Gateway{cfg: config.DatabaseGatewayConfig{Enabled: true}}
	err := gateway.ListenAndServe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no enabled protocol listeners") {
		t.Fatalf("ListenAndServe() error = %v, want independent-listener rejection", err)
	}
}

func TestPostgresNegotiatesTLSBeforeAuthentication(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	gateway := &Gateway{logger: slog.Default()}
	done := make(chan struct{})
	go func() {
		defer close(done)
		gateway.handlePostgresConnection(context.Background(), server, config.DatabaseProtocolListener{CertFile: certFile, KeyFile: keyFile})
	}()

	request := make([]byte, 8)
	binary.BigEndian.PutUint32(request[:4], 8)
	binary.BigEndian.PutUint32(request[4:], 80877103)
	if _, err := client.Write(request); err != nil {
		t.Fatal(err)
	}
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 1)
	if _, err := client.Read(response); err != nil {
		t.Fatal(err)
	}
	if response[0] != 'S' {
		t.Fatalf("SSL negotiation response = %q, want S", response[0])
	}
	secured := tls.Client(client, &tls.Config{InsecureSkipVerify: true}) // test certificate is self-signed
	if err := secured.Handshake(); err != nil {
		t.Fatalf("TLS handshake: %v", err)
	}
	_ = secured.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("PostgreSQL TLS handler did not stop")
	}
}

func TestPostgresGSSENCRequestRequiresSubsequentTLSNegotiation(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	gateway := &Gateway{logger: slog.Default()}
	done := make(chan struct{})
	go func() {
		defer close(done)
		gateway.handlePostgresConnection(context.Background(), server, config.DatabaseProtocolListener{CertFile: certFile, KeyFile: keyFile})
	}()

	gssRequest := make([]byte, 8)
	binary.BigEndian.PutUint32(gssRequest[:4], 8)
	binary.BigEndian.PutUint32(gssRequest[4:], 80877104)
	if _, err := client.Write(gssRequest); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 1)
	if _, err := client.Read(response); err != nil {
		t.Fatal(err)
	}
	if response[0] != 'N' {
		t.Fatalf("GSSENC negotiation response = %q, want N", response[0])
	}

	sslRequest := make([]byte, 8)
	binary.BigEndian.PutUint32(sslRequest[:4], 8)
	binary.BigEndian.PutUint32(sslRequest[4:], 80877103)
	if _, err := client.Write(sslRequest); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Read(response); err != nil {
		t.Fatal(err)
	}
	if response[0] != 'S' {
		t.Fatalf("SSL negotiation response after GSSENC = %q, want S", response[0])
	}
	secured := tls.Client(client, &tls.Config{InsecureSkipVerify: true}) // test certificate is self-signed
	if err := secured.Handshake(); err != nil {
		t.Fatalf("TLS handshake after GSSENC negotiation: %v", err)
	}
	_ = secured.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("PostgreSQL GSSENC handler did not stop")
	}
}

func TestMySQLNegotiatesTLSWhenListenerCertificateIsConfigured(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	gateway := &Gateway{logger: slog.Default()}
	done := make(chan struct{})
	go func() {
		defer close(done)
		gateway.handleMySQLWithListener(context.Background(), server, config.DatabaseProtocolListener{CertFile: certFile, KeyFile: keyFile})
	}()

	if _, err := readMySQLPacket(client); err != nil {
		t.Fatalf("read MySQL handshake: %v", err)
	}
	sslRequest := make([]byte, 32)
	binary.LittleEndian.PutUint32(sslRequest[:4], mysqlClientProtocol41|mysqlClientSSL)
	binary.LittleEndian.PutUint32(sslRequest[4:8], 1<<24)
	sslRequest[8] = 45
	if _, err := client.Write(mysqlPacketWithSeq(1, sslRequest)); err != nil {
		t.Fatal(err)
	}
	secured := tls.Client(client, &tls.Config{InsecureSkipVerify: true}) // test certificate is self-signed
	if err := secured.Handshake(); err != nil {
		t.Fatalf("MySQL TLS handshake: %v", err)
	}
	_ = secured.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("MySQL TLS handler did not stop")
	}
}

func TestMySQLTLSListenerRejectsPlaintextLogin(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	gateway := &Gateway{
		cfg: config.DatabaseGatewayConfig{
			ClientTLSMode: config.DatabaseGatewayClientTLSModeRequired,
		},
		logger: slog.Default(),
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		gateway.handleMySQLWithListener(context.Background(), server, config.DatabaseProtocolListener{CertFile: certFile, KeyFile: keyFile})
	}()

	if _, err := readMySQLPacket(client); err != nil {
		t.Fatalf("read MySQL handshake: %v", err)
	}
	if _, err := client.Write(protocolValidationMySQLLogin(databaseCompactUsername())); err != nil {
		t.Fatal(err)
	}
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	rejection, err := readMySQLPacket(client)
	if err != nil {
		t.Fatalf("read TLS-required rejection: %v", err)
	}
	if len(rejection.payload) == 0 || rejection.payload[0] != 0xff {
		t.Fatalf("TLS-required rejection payload = %x, want MySQL error", rejection.payload)
	}
	if rejection.seq != 2 {
		t.Fatalf("TLS-required rejection sequence = %d, want 2", rejection.seq)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("MySQL TLS listener did not reject plaintext login")
	}
}

func TestOptionalTLSListenersStillAuthenticatePlaintextClients(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	listener := config.DatabaseProtocolListener{
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	t.Run("MySQL", func(t *testing.T) {
		gateway, resolver := newCrossProtocolGateway(t, "mysql")
		gateway.cfg.ClientTLSMode = config.DatabaseGatewayClientTLSModeOptional
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()

		done := make(chan *gatewayConn, 1)
		go func() {
			done <- gateway.handleMySQLWithListener(
				context.Background(),
				server,
				listener,
			)
		}()
		if _, err := readMySQLPacket(client); err != nil {
			t.Fatalf("read MySQL handshake: %v", err)
		}
		if _, err := client.Write(
			protocolValidationMySQLLogin(databaseCompactUsername()),
		); err != nil {
			t.Fatalf("write plaintext MySQL login: %v", err)
		}
		go drainProtocolRejection(client)

		select {
		case connection := <-done:
			if connection != nil {
				t.Fatal("test authentication unexpectedly established a MySQL connection")
			}
		case <-time.After(time.Second):
			t.Fatal("optional MySQL plaintext handler did not return")
		}
		if len(resolver.mysqlContexts) != 1 {
			t.Fatalf(
				"MySQL plaintext authentication calls = %d, want 1",
				len(resolver.mysqlContexts),
			)
		}
	})

	t.Run("PostgreSQL", func(t *testing.T) {
		gateway, resolver := newCrossProtocolGateway(t, "postgres")
		gateway.cfg.ClientTLSMode = config.DatabaseGatewayClientTLSModeOptional
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()

		done := make(chan *gatewayConn, 1)
		go func() {
			done <- gateway.handlePostgresConnection(
				context.Background(),
				server,
				listener,
			)
		}()
		startup := postgresStartupPacket(databaseCompactUsername(), "postgres")
		if _, err := client.Write(startup); err != nil {
			t.Fatalf("write plaintext PostgreSQL startup: %v", err)
		}
		response := make([]byte, 9)
		if _, err := io.ReadFull(client, response); err != nil {
			t.Fatalf("read PostgreSQL password request: %v", err)
		}
		if response[0] != 'R' {
			t.Fatalf("PostgreSQL response kind = %q, want R", response[0])
		}
		if err := writePostgresMessage(
			client,
			'p',
			append([]byte("bastion-password"), 0),
		); err != nil {
			t.Fatalf("write PostgreSQL password: %v", err)
		}
		go drainProtocolRejection(client)

		select {
		case connection := <-done:
			if connection != nil {
				t.Fatal("test authentication unexpectedly established a PostgreSQL connection")
			}
		case <-time.After(time.Second):
			t.Fatal("optional PostgreSQL plaintext handler did not return")
		}
		if len(resolver.passwordContexts) != 1 {
			t.Fatalf(
				"PostgreSQL plaintext authentication calls = %d, want 1",
				len(resolver.passwordContexts),
			)
		}
	})

	t.Run("Redis", func(t *testing.T) {
		gateway, resolver := newCrossProtocolGateway(t, "redis")
		gateway.cfg.ClientTLSMode = config.DatabaseGatewayClientTLSModeOptional
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()

		done := make(chan *gatewayConn, 1)
		go func() {
			done <- gateway.handleRedisConnection(
				context.Background(),
				server,
				listener,
			)
		}()
		if _, err := client.Write(
			redisAuthCommand(databaseCompactUsername(), "bastion-password"),
		); err != nil {
			t.Fatalf("write plaintext Redis AUTH: %v", err)
		}
		go drainProtocolRejection(client)

		select {
		case connection := <-done:
			if connection != nil {
				t.Fatal("test authentication unexpectedly established a Redis connection")
			}
		case <-time.After(time.Second):
			t.Fatal("optional Redis plaintext handler did not return")
		}
		if len(resolver.passwordContexts) != 1 {
			t.Fatalf(
				"Redis plaintext authentication calls = %d, want 1",
				len(resolver.passwordContexts),
			)
		}
	})
}

func TestRedisRejectsRemotePlaintextAuth(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	gateway := &Gateway{cfg: config.DatabaseGatewayConfig{
		ClientTLSMode: config.DatabaseGatewayClientTLSModeRequired,
	}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		gateway.handleRedisConnection(context.Background(), &remoteAddrConn{Conn: server, remote: "192.0.2.10:12345"}, config.DatabaseProtocolListener{})
	}()
	if _, err := client.Write([]byte{'*'}); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 128)
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("expected Redis TLS rejection: %v", err)
	}
	if got := string(buf[:n]); got != "-NOAUTH TLS is required for remote AUTH\r\n" {
		t.Fatalf("rejection = %q", got)
	}
	<-done
}

func TestMySQLInsecurePasswordFullAuthIsRejected(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	if _, err := (&Gateway{}).handleMySQLCachingSha2FullAuth(client, "secret"); err == nil {
		t.Fatal("expected full authentication to reject an unverified upstream")
	}
}

type remoteAddrConn struct {
	net.Conn
	remote string
}

func (c *remoteAddrConn) RemoteAddr() net.Addr {
	host, _, _ := net.SplitHostPort(c.remote)
	return &net.TCPAddr{IP: net.ParseIP(host), Port: 12345}
}

func writeListenerCertificate(t *testing.T) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificate, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile := filepath.Join(dir, "server.crt")
	keyFile := filepath.Join(dir, "server.key")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), 0o600); err != nil {
		t.Fatal(err)
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}
