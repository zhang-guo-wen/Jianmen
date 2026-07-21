package dbproxy

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"jianmen/internal/dbtls"
	"jianmen/internal/model"
)

func TestProbeUpstreamTLSCompletesWithoutSendingAuthenticationData(t *testing.T) {
	certificateFile, keyFile := writeListenerCertificate(t)
	certificatePEM, err := os.ReadFile(certificateFile)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.LoadX509KeyPair(certificateFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, protocol string
		serve          func(net.Conn, tls.Certificate) error
	}{
		{name: "mysql", protocol: "mysql", serve: serveMySQLTLSPreflight},
		{name: "postgres", protocol: "postgres", serve: servePostgresTLSPreflight},
		{name: "redis", protocol: "redis", serve: serveRedisTLSPreflight},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			serverResult := make(chan error, 1)
			go func() {
				connection, acceptErr := listener.Accept()
				if acceptErr != nil {
					serverResult <- acceptErr
					return
				}
				defer connection.Close()
				serverResult <- test.serve(connection, certificate)
			}()

			host, port := splitTestListenerAddress(t, listener)
			probeErr := ProbeUpstreamTLS(context.Background(), model.DatabaseInstance{
				Protocol: test.protocol, Address: host, Port: port,
				TLSMode: dbtls.ModeVerifyCA, TLSCAPEM: string(certificatePEM),
			})
			if probeErr != nil {
				t.Fatalf("ProbeUpstreamTLS() error = %v", probeErr)
			}
			select {
			case err := <-serverResult:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("TLS preflight server did not finish")
			}
		})
	}
}

func serveMySQLTLSPreflight(connection net.Conn, certificate tls.Certificate) error {
	if _, err := sendFakeMySQLHandshakeWithTLS(connection, true); err != nil {
		return err
	}
	request, err := readMySQLPacket(connection)
	if err != nil {
		return err
	}
	if request.seq != 1 || len(request.payload) != 32 || binary.LittleEndian.Uint32(request.payload[:4])&mysqlClientSSL == 0 {
		return errors.New("invalid MySQL SSLRequest")
	}
	return serveTLSPreflightAndRequireEOF(connection, certificate)
}

func servePostgresTLSPreflight(connection net.Conn, certificate tls.Certificate) error {
	request := make([]byte, 8)
	if _, err := io.ReadFull(connection, request); err != nil {
		return err
	}
	if !isPostgresSSLRequest(request) {
		return errors.New("invalid PostgreSQL SSLRequest")
	}
	if _, err := connection.Write([]byte{'S'}); err != nil {
		return err
	}
	return serveTLSPreflightAndRequireEOF(connection, certificate)
}

func serveRedisTLSPreflight(connection net.Conn, certificate tls.Certificate) error {
	return serveTLSPreflightAndRequireEOF(connection, certificate)
}

func serveTLSPreflightAndRequireEOF(connection net.Conn, certificate tls.Certificate) error {
	secured := tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{certificate}})
	if err := secured.Handshake(); err != nil {
		return err
	}
	if err := secured.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}
	var applicationData [1]byte
	count, err := secured.Read(applicationData[:])
	if count != 0 {
		return errors.New("TLS preflight sent application authentication data")
	}
	if err == nil {
		return errors.New("TLS preflight connection stayed open without authentication data")
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return errors.New("TLS preflight did not close after the handshake")
	}
	return nil
}
