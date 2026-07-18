package service

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"jianmen/internal/proxy/mysqlwire"
)

func TestReadMySQLAuthResultRejectsCombinedFullAuthWithoutVerifiedTLS(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	serverDone := make(chan error, 1)
	go func() {
		packet, encodeErr := mysqlwire.EncodePacket(3, []byte{0x01, 0x04})
		if encodeErr != nil {
			serverDone <- encodeErr
			return
		}
		_, err := server.Write(packet)
		serverDone <- err
	}()
	err := mysqlwire.ContinueAuthentication(
		context.Background(),
		client,
		mysqlwire.AuthenticationOptions{
			Password: "secret", VerifiedTLS: false, MaxPacketBytes: maxMySQLAuthPacketBytes,
		},
	)
	if err == nil || !strings.Contains(err.Error(), "verified TLS") {
		t.Fatalf("ContinueAuthentication() error = %v, want verified TLS rejection", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestReadMySQLAuthResultReadsFragmentedPacket(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	go func() {
		for _, fragment := range [][]byte{{1}, {0, 0}, {3}, {0}} {
			_, _ = server.Write(fragment)
		}
	}()

	err := mysqlwire.ContinueAuthentication(
		context.Background(),
		client,
		mysqlwire.AuthenticationOptions{
			Password: "secret", VerifiedTLS: false, MaxPacketBytes: maxMySQLAuthPacketBytes,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestReadMySQLAuthResultRejectsHeaderWithoutPayload(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	go func() { _, _ = server.Write([]byte{1, 0, 0, 3}) }()
	_ = client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

	if err := mysqlwire.ContinueAuthentication(
		context.Background(),
		client,
		mysqlwire.AuthenticationOptions{
			Password: "secret", VerifiedTLS: false, MaxPacketBytes: maxMySQLAuthPacketBytes,
		},
	); err == nil {
		t.Fatal("expected header without payload to be rejected")
	}
}

func TestReadMySQLAuthResultRejectsTruncatedAuthSwitch(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	go func() {
		packet, _ := mysqlwire.EncodePacket(2, []byte{0xfe, 'b', 'a', 'd'})
		_, _ = server.Write(packet)
	}()

	err := mysqlwire.ContinueAuthentication(
		context.Background(),
		client,
		mysqlwire.AuthenticationOptions{
			Password: "secret", VerifiedTLS: false, MaxPacketBytes: maxMySQLAuthPacketBytes,
		},
	)
	if err == nil || !strings.Contains(err.Error(), "authentication switch") {
		t.Fatalf("error = %v, want malformed auth switch", err)
	}
}

var _ DatabaseAccountProvisioner = MySQLDatabaseProvisioner{}
