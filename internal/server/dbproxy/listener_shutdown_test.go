package dbproxy

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"jianmen/internal/config"
)

func TestProtocolListenersShutdownAllConnectionsAfterAcceptFailure(t *testing.T) {
	clientListener := newControlledListener()
	failingListener := newControlledListener()
	server, client := net.Pipe()
	defer client.Close()
	clientListener.accepts <- listenerAccept{connection: server}

	gateway := &Gateway{logger: slog.Default()}
	done := make(chan error, 1)
	go func() {
		done <- gateway.serveBoundProtocolListeners(context.Background(), []boundProtocolListener{
			{listener: clientListener, item: protocolListener{protocol: databaseProtocolMySQL, config: config.DatabaseProtocolListener{}}},
			{listener: failingListener, item: protocolListener{protocol: databaseProtocolRedis, config: config.DatabaseProtocolListener{}}},
		})
	}()

	if _, err := readMySQLPacket(client); err != nil {
		t.Fatalf("read MySQL handshake: %v", err)
	}
	failingListener.accepts <- listenerAccept{err: errors.New("injected accept failure")}

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "injected accept failure") {
			t.Fatalf("serveBoundProtocolListeners() error = %v, want injected accept failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("listener group did not wait for shutdown after an accept failure")
	}

	for _, listener := range []*controlledListener{clientListener, failingListener} {
		select {
		case <-listener.closed:
		case <-time.After(time.Second):
			t.Fatal("listener remained open after peer listener accept failure")
		}
	}
	var buffer [1]byte
	if _, err := client.Read(buffer[:]); err == nil {
		t.Fatal("active connection remained open after listener accept failure")
	}
}

func TestProtocolListenerReturnsAcceptErrorContainingClosed(t *testing.T) {
	listener := newControlledListener()
	listener.accepts <- listenerAccept{err: errors.New("upstream closed circuit unexpectedly")}

	err := (&Gateway{logger: slog.Default()}).serveProtocolListener(
		context.Background(),
		listener,
		databaseProtocolMySQL,
		config.DatabaseProtocolListener{},
	)
	if err == nil || !strings.Contains(err.Error(), "upstream closed circuit unexpectedly") {
		t.Fatalf("serveProtocolListener() error = %v, want the real Accept error", err)
	}
}

func TestPostgresGSSENCRejectsPlaintextStartupAndClosesConnection(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- (&Gateway{
			cfg: config.DatabaseGatewayConfig{
				ClientTLSMode: config.DatabaseGatewayClientTLSModeRequired,
			},
			logger: slog.Default(),
		}).serveProtocolListener(
			ctx,
			listener,
			databaseProtocolPostgreSQL,
			config.DatabaseProtocolListener{CertFile: certFile, KeyFile: keyFile},
		)
	}()
	client, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	gssRequest := make([]byte, 8)
	binary.BigEndian.PutUint32(gssRequest[:4], 8)
	binary.BigEndian.PutUint32(gssRequest[4:], 80877104)
	if _, err := client.Write(gssRequest); err != nil {
		t.Fatal(err)
	}
	var response [1]byte
	if _, err := client.Read(response[:]); err != nil {
		t.Fatal(err)
	}
	if response[0] != 'N' {
		t.Fatalf("GSSENC negotiation response = %q, want N", response[0])
	}

	if _, err := client.Write(postgresStartupPacket(databaseCompactUsername(), "postgres")); err != nil {
		t.Fatal(err)
	}
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Read(response[:]); err != nil {
		t.Fatalf("read plaintext Startup rejection: %v", err)
	}
	if response[0] != 'E' {
		t.Fatalf("plaintext Startup response type = %q, want TLS-required error", response[0])
	}
	var remainder [256]byte
	for {
		if _, err := client.Read(remainder[:]); err != nil {
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("PostgreSQL connection remained open after plaintext Startup rejection")
			}
			break
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveProtocolListener() error = %v, want normal cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("PostgreSQL listener did not stop after cancellation")
	}
}

type listenerAccept struct {
	connection net.Conn
	err        error
}

type controlledListener struct {
	accepts   chan listenerAccept
	closed    chan struct{}
	closeOnce sync.Once
}

func newControlledListener() *controlledListener {
	return &controlledListener{
		accepts: make(chan listenerAccept, 1),
		closed:  make(chan struct{}),
	}
}

func (l *controlledListener) Accept() (net.Conn, error) {
	select {
	case result := <-l.accepts:
		return result.connection, result.err
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *controlledListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *controlledListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
}
