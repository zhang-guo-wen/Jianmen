package dbproxy

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/dbtls"
	"jianmen/internal/model"
)

func TestPostgresCancelRequestSupportsProtocol30AndVariableSecrets(t *testing.T) {
	for _, secretLength := range []int{4, 32, 256} {
		t.Run(decimalString(secretLength), func(t *testing.T) {
			secret := make([]byte, secretLength)
			for index := range secret {
				secret[index] = byte(index)
			}
			message := postgresCancelTestMessage(42, secret)
			key, err := parsePostgresCancelRequest(message)
			if err != nil {
				t.Fatal(err)
			}
			if key.processID != 42 || key.secret != string(secret) {
				t.Fatalf("cancel key = %#v", key)
			}
			backendPayload := make([]byte, 4+len(secret))
			binary.BigEndian.PutUint32(backendPayload[:4], 42)
			copy(backendPayload[4:], secret)
			backendKey, err := parsePostgresBackendKey(backendPayload)
			if err != nil {
				t.Fatal(err)
			}
			if backendKey != key {
				t.Fatalf("BackendKeyData = %#v, CancelRequest = %#v", backendKey, key)
			}
		})
	}
}

func TestPostgresCancelRequestRejectsMalformedLengths(t *testing.T) {
	tests := [][]byte{
		postgresCancelTestMessage(1, make([]byte, 3)),
		postgresCancelTestMessage(1, make([]byte, 257)),
		{0, 0, 0, 16, 0, 0, 0, 0},
	}
	mismatch := postgresCancelTestMessage(1, make([]byte, 4))
	binary.BigEndian.PutUint32(mismatch[:4], uint32(len(mismatch)+1))
	tests = append(tests, mismatch)

	for index, message := range tests {
		if _, err := parsePostgresCancelRequest(message); err == nil {
			t.Fatalf("malformed CancelRequest %d was accepted", index)
		}
	}
}

func TestPostgresRequiredAcceptsPlaintextCancelRequest(t *testing.T) {
	expected := postgresCancelTestMessage(42, []byte{1, 2, 3, 4})
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()

	upstreamResult := make(chan error, 1)
	go func() {
		connection, acceptErr := upstream.Accept()
		if acceptErr != nil {
			upstreamResult <- acceptErr
			return
		}
		defer connection.Close()
		actual := make([]byte, len(expected))
		if _, readErr := io.ReadFull(connection, actual); readErr != nil {
			upstreamResult <- readErr
			return
		}
		if string(actual) != string(expected) {
			upstreamResult <- errors.New("forwarded CancelRequest did not match")
			return
		}
		upstreamResult <- nil
	}()

	host, portText, err := net.SplitHostPort(upstream.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		t.Fatal(err)
	}
	gateway := &Gateway{
		cfg: config.DatabaseGatewayConfig{
			ClientTLSMode: config.DatabaseGatewayClientTLSModeRequired,
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	key, err := parsePostgresCancelRequest(expected)
	if err != nil {
		t.Fatal(err)
	}
	release := gateway.postgresCancels.register(key, model.DatabaseInstance{
		ID:       "plaintext-cancel",
		Protocol: "postgres",
		Address:  host,
		Port:     port,
		TLSMode:  dbtls.ModeDisable,
	})
	defer release()

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		gateway.handlePostgresConnection(
			context.Background(),
			server,
			config.DatabaseProtocolListener{},
		)
	}()
	if _, err := client.Write(expected); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-upstreamResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("plaintext CancelRequest was not forwarded in required mode")
	}
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("PostgreSQL cancel handler did not return")
	}
}

func TestPostgresCancelRegistryCleansUpAndRejectsAmbiguousKeys(t *testing.T) {
	var registry postgresCancelRegistry
	key := postgresCancelKey{processID: 7, secret: "secret"}
	first := model.DatabaseInstance{ID: "first"}
	second := model.DatabaseInstance{ID: "second"}

	releaseFirst := registry.register(key, first)
	if got, ok := registry.lookup(key); !ok || got.ID != first.ID {
		t.Fatalf("single route = (%#v, %t)", got, ok)
	}
	releaseSecond := registry.register(key, second)
	if _, ok := registry.lookup(key); ok {
		t.Fatal("ambiguous cancellation key was routed")
	}
	releaseSecond()
	if got, ok := registry.lookup(key); !ok || got.ID != first.ID {
		t.Fatalf("route after collision cleanup = (%#v, %t)", got, ok)
	}
	releaseFirst()
	releaseFirst()
	if _, ok := registry.lookup(key); ok {
		t.Fatal("released cancellation route remained registered")
	}
}

func TestForwardPostgresCancelReusesVerifiedUpstreamTLS(t *testing.T) {
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

	expected := postgresCancelTestMessage(99, []byte{1, 2, 3, 4})
	serverResult := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverResult <- acceptErr
			return
		}
		defer connection.Close()
		sslRequest := make([]byte, 8)
		if _, readErr := io.ReadFull(connection, sslRequest); readErr != nil {
			serverResult <- readErr
			return
		}
		if !isPostgresSSLRequest(sslRequest) {
			serverResult <- errors.New("cancel route did not request upstream TLS")
			return
		}
		if _, writeErr := connection.Write([]byte{'S'}); writeErr != nil {
			serverResult <- writeErr
			return
		}
		certificate, loadErr := tls.LoadX509KeyPair(certificateFile, keyFile)
		if loadErr != nil {
			serverResult <- loadErr
			return
		}
		secured := tls.Server(connection, &tls.Config{
			Certificates: []tls.Certificate{certificate},
			MinVersion:   tls.VersionTLS12,
		})
		if handshakeErr := secured.Handshake(); handshakeErr != nil {
			serverResult <- handshakeErr
			return
		}
		actual := make([]byte, len(expected))
		if _, readErr := io.ReadFull(secured, actual); readErr != nil {
			serverResult <- readErr
			return
		}
		if string(actual) != string(expected) {
			serverResult <- errors.New("forwarded CancelRequest did not match")
			return
		}
		serverResult <- nil
	}()

	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		t.Fatal(err)
	}
	instance := model.DatabaseInstance{
		ID: "tls-postgres", Protocol: "postgres", Address: host, Port: port,
		TLSMode: dbtls.ModeVerifyCA, TLSCAPEM: string(certificatePEM),
	}
	gateway := &Gateway{}
	key, err := parsePostgresCancelRequest(expected)
	if err != nil {
		t.Fatal(err)
	}
	release := gateway.postgresCancels.register(key, instance)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := gateway.forwardPostgresCancel(ctx, expected); err != nil {
		t.Fatal(err)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestForwardPostgresCancelRejectsUnknownKeyWithoutDial(t *testing.T) {
	gateway := &Gateway{}
	err := gateway.forwardPostgresCancel(
		context.Background(),
		postgresCancelTestMessage(1, []byte{1, 2, 3, 4}),
	)
	if !errors.Is(err, errPostgresCancelRouteNotFound) {
		t.Fatalf("unknown key error = %v", err)
	}
}

func postgresCancelTestMessage(processID uint32, secret []byte) []byte {
	message := make([]byte, 12+len(secret))
	binary.BigEndian.PutUint32(message[:4], uint32(len(message)))
	binary.BigEndian.PutUint32(message[4:8], postgresCancelRequestCode)
	binary.BigEndian.PutUint32(message[8:12], processID)
	copy(message[12:], secret)
	return message
}
