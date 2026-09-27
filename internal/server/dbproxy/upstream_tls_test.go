package dbproxy

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"jianmen/internal/dbtls"
	"jianmen/internal/model"
)

func TestRedisUpstreamDefaultsToPlaintext(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan byte, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		var first [1]byte
		if _, readErr := connection.Read(first[:]); readErr == nil {
			received <- first[0]
		}
	}()
	host, port := splitTestListenerAddress(t, listener)
	connection, err := dialRedisUpstream(context.Background(), model.DatabaseInstance{
		Address: host,
		Port:    port,
	})
	if err != nil {
		t.Fatalf("dialRedisUpstream() error = %v", err)
	}
	defer connection.Close()
	if _, err := connection.Write([]byte{'*'}); err != nil {
		t.Fatalf("write plaintext Redis preface: %v", err)
	}
	select {
	case first := <-received:
		if first != '*' {
			t.Fatalf("Redis upstream first byte = 0x%02x, want plaintext '*'", first)
		}
	case <-time.After(time.Second):
		t.Fatal("Redis upstream did not receive plaintext preface")
	}
}

func TestPostgresCleartextPasswordTransportPolicy(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	if err := validatePostgresCleartextPasswordTransport(client, dbtls.ModeDisable); err != nil {
		t.Fatalf("disabled TLS rejected cleartext PostgreSQL password: %v", err)
	}
	for _, mode := range []string{dbtls.ModeVerifyCA, dbtls.ModeVerifyFull} {
		if err := validatePostgresCleartextPasswordTransport(client, mode); !errors.Is(err, errVerifiedTLSRequired) {
			t.Fatalf("mode %q error = %v, want errVerifiedTLSRequired", mode, err)
		}
	}
}

func TestProbeMySQLReportsUpstreamTLSUnsupported(t *testing.T) {
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
		_, writeErr := sendFakeMySQLHandshakeWithTLS(connection, false)
		serverDone <- writeErr
	}()
	host, port := splitTestListenerAddress(t, listener)
	err = ProbeDatabaseAuthentication(context.Background(), model.DatabaseInstance{
		Protocol: "mysql", Address: host, Port: port, TLSMode: dbtls.ModeVerifyCA,
	}, "probe", "secret")
	if !errors.Is(err, ErrUpstreamTLSUnsupported) {
		t.Fatalf("ProbeDatabaseAuthentication() error = %v, want ErrUpstreamTLSUnsupported", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestProbePostgresReportsUpstreamTLSUnsupported(t *testing.T) {
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
		request := make([]byte, 8)
		if _, readErr := io.ReadFull(connection, request); readErr != nil {
			serverDone <- readErr
			return
		}
		if !isPostgresSSLRequest(request) {
			serverDone <- errors.New("invalid postgresql SSLRequest")
			return
		}
		_, writeErr := connection.Write([]byte{'N'})
		serverDone <- writeErr
	}()
	host, port := splitTestListenerAddress(t, listener)
	err = ProbeDatabaseAuthentication(context.Background(), model.DatabaseInstance{
		Protocol: "postgres", Address: host, Port: port, TLSMode: dbtls.ModeVerifyCA,
	}, "probe", "secret")
	if !errors.Is(err, ErrUpstreamTLSUnsupported) {
		t.Fatalf("ProbeDatabaseAuthentication() error = %v, want ErrUpstreamTLSUnsupported", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestProbePostgresDoesNotMisclassifyInvalidSSLResponse(t *testing.T) {
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
		request := make([]byte, 8)
		if _, readErr := io.ReadFull(connection, request); readErr != nil {
			serverDone <- readErr
			return
		}
		_, writeErr := connection.Write([]byte{'?'})
		serverDone <- writeErr
	}()
	host, port := splitTestListenerAddress(t, listener)
	err = ProbeDatabaseAuthentication(context.Background(), model.DatabaseInstance{
		Protocol: "postgres", Address: host, Port: port, TLSMode: dbtls.ModeVerifyCA,
	}, "probe", "secret")
	if err == nil || errors.Is(err, ErrUpstreamTLSUnsupported) {
		t.Fatalf("ProbeDatabaseAuthentication() error = %v, want non-TLS-unsupported protocol error", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
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
