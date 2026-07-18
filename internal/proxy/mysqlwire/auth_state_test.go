package mysqlwire

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"
)

func TestContinueAuthenticationHandlesAuthSwitchThenCachingSHA2FullAuth(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	serverResult := make(chan error, 1)
	go func() {
		switchPayload := append([]byte{0xfe}, []byte("caching_sha2_password\x00")...)
		switchPayload = append(switchPayload, []byte("abcdefghijklmnopqrst")...)
		if _, err := server.Write(mustEncodePacketForTest(2, switchPayload)); err != nil {
			serverResult <- err
			return
		}
		response, err := ReadPacket(context.Background(), server, 1024)
		if err != nil {
			serverResult <- err
			return
		}
		if response.Sequence != 3 || len(response.Payload) != 32 {
			t.Errorf("auth-switch response = seq %d payload %x", response.Sequence, response.Payload)
		}
		if _, err := server.Write(mustEncodePacketForTest(4, []byte{0x01, 0x04})); err != nil {
			serverResult <- err
			return
		}
		fullAuth, err := ReadPacket(context.Background(), server, 1024)
		if err != nil {
			serverResult <- err
			return
		}
		if fullAuth.Sequence != 5 || !bytes.Equal(fullAuth.Payload, []byte("secret\x00")) {
			t.Errorf("full-auth response = seq %d payload %q", fullAuth.Sequence, fullAuth.Payload)
		}
		_, err = server.Write(mustEncodePacketForTest(6, []byte{0x00}))
		serverResult <- err
	}()

	err := ContinueAuthentication(
		context.Background(),
		client,
		AuthenticationOptions{Password: "secret", VerifiedTLS: true, MaxPacketBytes: 1024},
	)
	if err != nil {
		t.Fatalf("continue authentication: %v", err)
	}
	if err := <-serverResult; err != nil {
		t.Fatalf("serve authentication: %v", err)
	}
}

func TestContinueAuthenticationReadsFastAuthOKAndSplitFullAuthSequence(t *testing.T) {
	t.Run("fast auth", func(t *testing.T) {
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()
		go func() {
			_, _ = server.Write(mustEncodePacketForTest(2, []byte{0x01, 0x03}))
			_, _ = server.Write(mustEncodePacketForTest(3, []byte{0x00}))
		}()
		if err := ContinueAuthentication(
			context.Background(),
			client,
			AuthenticationOptions{Password: "secret", VerifiedTLS: false, MaxPacketBytes: 1024},
		); err != nil {
			t.Fatalf("fast authentication: %v", err)
		}
	})

	t.Run("split full auth", func(t *testing.T) {
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()
		serverResult := make(chan error, 1)
		go func() {
			if _, err := server.Write(mustEncodePacketForTest(2, []byte{0x01})); err != nil {
				serverResult <- err
				return
			}
			if _, err := server.Write(mustEncodePacketForTest(3, []byte{0x04})); err != nil {
				serverResult <- err
				return
			}
			response, err := ReadPacket(context.Background(), server, 1024)
			if err == nil && (response.Sequence != 4 || !bytes.Equal(response.Payload, []byte("secret\x00"))) {
				t.Errorf("split full-auth response = seq %d payload %q", response.Sequence, response.Payload)
			}
			if err == nil {
				_, err = server.Write(mustEncodePacketForTest(5, []byte{0x00}))
			}
			serverResult <- err
		}()
		if err := ContinueAuthentication(
			context.Background(),
			client,
			AuthenticationOptions{Password: "secret", VerifiedTLS: true, MaxPacketBytes: 1024},
		); err != nil {
			t.Fatalf("split full authentication: %v", err)
		}
		if err := <-serverResult; err != nil {
			t.Fatalf("serve split full authentication: %v", err)
		}
	})
}

func TestContinueAuthenticationRejectsFullAuthWithoutVerifiedTLS(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	go func() {
		_, _ = server.Write(mustEncodePacketForTest(2, []byte{0x01, 0x04}))
	}()
	_ = client.SetDeadline(time.Now().Add(time.Second))
	err := ContinueAuthentication(
		context.Background(),
		client,
		AuthenticationOptions{Password: "secret", VerifiedTLS: false, MaxPacketBytes: 1024},
	)
	if err == nil || err.Error() != "mysql full authentication requires verified TLS" {
		t.Fatalf("full authentication error = %v", err)
	}
}

func mustEncodePacketForTest(sequence byte, payload []byte) []byte {
	packet, err := EncodePacket(sequence, payload)
	if err != nil {
		panic(err)
	}
	return packet
}
