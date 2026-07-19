package dbproxy

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"testing"
	"time"

	"jianmen/internal/config"
)

func TestPostgresDirectTLSRequiresAndNegotiatesALPN(t *testing.T) {
	certificateFile, keyFile := writeListenerCertificate(t)
	server, client := net.Pipe()
	defer client.Close()
	gateway := &Gateway{logger: slog.Default()}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		gateway.handlePostgresConnection(
			context.Background(),
			server,
			config.DatabaseProtocolListener{CertFile: certificateFile, KeyFile: keyFile},
		)
	}()

	secured := tls.Client(client, &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{postgresTLSALPN},
		MinVersion:         tls.VersionTLS12,
	})
	if err := secured.Handshake(); err != nil {
		t.Fatalf("direct TLS handshake: %v", err)
	}
	if got := secured.ConnectionState().NegotiatedProtocol; got != postgresTLSALPN {
		t.Fatalf("direct TLS ALPN = %q", got)
	}
	if err := secured.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("direct PostgreSQL TLS handler did not stop")
	}
}

func TestPostgresDirectTLSRejectsMissingALPN(t *testing.T) {
	certificateFile, keyFile := writeListenerCertificate(t)
	server, client := net.Pipe()
	defer client.Close()
	gateway := &Gateway{logger: slog.Default()}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		gateway.handlePostgresConnection(
			context.Background(),
			server,
			config.DatabaseProtocolListener{CertFile: certificateFile, KeyFile: keyFile},
		)
	}()

	if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	secured := tls.Client(client, &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	})
	if err := secured.Handshake(); err != nil {
		t.Fatalf("direct TLS handshake without ALPN: %v", err)
	}
	buffer := make([]byte, 1)
	if _, err := secured.Read(buffer); err == nil {
		t.Fatal("direct TLS connection without PostgreSQL ALPN remained open")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("missing-ALPN PostgreSQL TLS handler did not stop")
	}
}

func TestPostgresDirectTLSClientHelloDetectionRejectsProtocolLookalikes(t *testing.T) {
	if !isPostgresDirectTLSClientHello([]byte{22, 3, 1, 0, 64, 1, 0, 0}) {
		t.Fatal("valid TLS ClientHello prefix was not detected")
	}
	for _, header := range [][]byte{
		{22, 3, 1, 0, 64, 2, 0, 0},
		{22, 2, 1, 0, 64, 1, 0, 0},
		{22, 3, 1, 0, 3, 1, 0, 0},
		{0, 0, 0, 8, 0, 3, 0, 0},
	} {
		if isPostgresDirectTLSClientHello(header) {
			t.Fatalf("invalid direct TLS prefix was accepted: %x", header)
		}
	}
}
