package dbproxy

import (
	"context"
	"crypto/tls"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"jianmen/internal/dbtls"
	"jianmen/internal/model"
)

func TestRedisUpstreamRejectsRemotePlaintext(t *testing.T) {
	_, err := dialRedisUpstream(context.Background(), model.DatabaseInstance{
		Address: "192.0.2.10",
		Port:    6379,
		TLSMode: "disable",
	})
	if err == nil || !strings.Contains(err.Error(), "requires TLS") {
		t.Fatalf("dialRedisUpstream() error = %v, want remote plaintext rejection", err)
	}
}

func TestPostgresCleartextPasswordRequiresVerifiedTLS(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	if err := requireVerifiedPostgresTLS(client); err == nil {
		t.Fatal("cleartext PostgreSQL password was allowed without verified TLS")
	}
}

func TestRedisUpstreamUsesVerifiedTLS(t *testing.T) {
	certificateFile, keyFile := writeListenerCertificate(t)
	certificatePEM, err := os.ReadFile(certificateFile)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer connection.Close()
		certificate, loadErr := tls.LoadX509KeyPair(certificateFile, keyFile)
		if loadErr != nil {
			serverDone <- loadErr
			return
		}
		serverDone <- tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{certificate}}).Handshake()
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := net.LookupPort("tcp", portText)
	connection, err := dialRedisUpstream(context.Background(), model.DatabaseInstance{
		Address: host, Port: port, TLSMode: dbtls.ModeVerifyCA, TLSCAPEM: string(certificatePEM),
	})
	if err != nil {
		t.Fatalf("dialRedisUpstream() error = %v", err)
	}
	defer connection.Close()
	if !dbtls.IsVerified(connection) {
		t.Fatal("Redis upstream TLS connection was not marked verified")
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("TLS server did not complete its handshake")
	}
}

func TestDialMySQLUpstreamContextCancellationInterruptsSilentGreeting(t *testing.T) {
	listener, accepted := silentUpstreamListener(t)
	defer listener.Close()
	host, port := splitTestListenerAddress(t, listener)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, _, err := dialMySQLUpstream(ctx, model.DatabaseInstance{
			Protocol: "mysql",
			Address:  host,
			Port:     port,
			TLSMode:  dbtls.ModeDisable,
		}, "")
		result <- err
	}()

	serverConn := <-accepted
	defer serverConn.Close()
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("silent MySQL greeting unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("context cancellation did not interrupt silent MySQL greeting")
	}
}

func TestDialPostgresUpstreamContextDeadlineInterruptsSilentSSLResponse(t *testing.T) {
	listener, accepted := silentUpstreamListener(t)
	defer listener.Close()
	host, port := splitTestListenerAddress(t, listener)

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := dialPostgresUpstream(ctx, model.DatabaseInstance{
			Protocol: "postgres",
			Address:  host,
			Port:     port,
			TLSMode:  dbtls.ModeVerifyCA,
		})
		result <- err
	}()

	serverConn := <-accepted
	defer serverConn.Close()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("silent PostgreSQL SSL response unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("context deadline did not interrupt silent PostgreSQL SSL response")
	}
}

func TestProbeMySQLContextCancellationInterruptsSilentGreeting(t *testing.T) {
	listener, accepted := silentUpstreamListener(t)
	defer listener.Close()
	host, port := splitTestListenerAddress(t, listener)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- ProbeDatabaseAuthentication(ctx, model.DatabaseInstance{
			Protocol: "mysql",
			Address:  host,
			Port:     port,
			TLSMode:  dbtls.ModeDisable,
		}, "probe", "secret")
	}()

	serverConn := <-accepted
	defer serverConn.Close()
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("silent MySQL probe unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("context cancellation did not interrupt silent MySQL probe")
	}
}

func TestClearingUpstreamHandshakeDeadlineStopsContextWatcher(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	wrapped, err := newUpstreamHandshakeConn(ctx, client, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := wrapped.SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	cancel()
	time.Sleep(75 * time.Millisecond)

	go func() { _, _ = server.Write([]byte{42}) }()
	var value [1]byte
	if _, err := wrapped.Read(value[:]); err != nil {
		t.Fatalf("cleared handshake deadline still interrupted relay: %v", err)
	}
	if value[0] != 42 {
		t.Fatalf("relay byte = %d", value[0])
	}
}

func TestCanceledUpstreamHandshakeCannotBeClearedIntoRelay(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	wrapped, err := newUpstreamHandshakeConn(ctx, client, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	time.Sleep(10 * time.Millisecond)
	if err := wrapped.SetDeadline(time.Time{}); err == nil {
		t.Fatal("a canceled upstream handshake was cleared into relay mode")
	}
}

func silentUpstreamListener(t *testing.T) (net.Listener, <-chan net.Conn) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()
	return listener, accepted
}

func splitTestListenerAddress(t *testing.T, listener net.Listener) (string, int) {
	t.Helper()
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}
