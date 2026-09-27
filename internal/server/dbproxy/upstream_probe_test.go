package dbproxy

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"jianmen/internal/dbtls"
	"jianmen/internal/model"
)

func TestProbePostgresAllowsCleartextPasswordWhenTLSDisabled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverResult := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverResult <- acceptErr
			return
		}
		defer conn.Close()
		startupLength := make([]byte, 4)
		if _, readErr := io.ReadFull(conn, startupLength); readErr != nil {
			serverResult <- readErr
			return
		}
		startup := make([]byte, binary.BigEndian.Uint32(startupLength)-4)
		if _, readErr := io.ReadFull(conn, startup); readErr != nil {
			serverResult <- readErr
			return
		}
		challenge := []byte{'R', 0, 0, 0, 8, 0, 0, 0, 3}
		if _, writeErr := conn.Write(challenge); writeErr != nil {
			serverResult <- writeErr
			return
		}
		passwordMessage, readErr := readPostgresMessage(conn, maxPostgresAuthMessageBytes)
		if readErr != nil {
			serverResult <- readErr
			return
		}
		if passwordMessage.kind != 'p' || string(passwordMessage.payload) != "secret\x00" {
			serverResult <- postgresProbeTestError("unexpected PostgreSQL cleartext password response")
			return
		}
		if writeErr := writePostgresTestMessage(conn, 'R', []byte{0, 0, 0, 0}); writeErr != nil {
			serverResult <- writeErr
			return
		}
		if writeErr := writePostgresTestMessage(conn, 'Z', []byte{'I'}); writeErr != nil {
			serverResult <- writeErr
			return
		}
		serverResult <- nil
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := net.LookupPort("tcp", portText)
	err = ProbeDatabaseAuthentication(context.Background(), model.DatabaseInstance{
		Protocol: "postgres", Address: host, Port: port, TLSMode: "disable",
	}, "probe", "secret")
	if err != nil {
		t.Fatalf("ProbeDatabaseAuthentication() error = %v", err)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestProbePostgresTLSCompletesSCRAMAuthentication(t *testing.T) {
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
	serverResult := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverResult <- acceptErr
			return
		}
		defer conn.Close()
		sslRequest := make([]byte, 8)
		if _, readErr := io.ReadFull(conn, sslRequest); readErr != nil || !isPostgresSSLRequest(sslRequest) {
			serverResult <- errInvalidPostgresSSLRequest
			return
		}
		if _, writeErr := conn.Write([]byte{'S'}); writeErr != nil {
			serverResult <- writeErr
			return
		}
		certificate, loadErr := tls.LoadX509KeyPair(certificateFile, keyFile)
		if loadErr != nil {
			serverResult <- loadErr
			return
		}
		secured := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}})
		if handshakeErr := secured.Handshake(); handshakeErr != nil {
			serverResult <- handshakeErr
			return
		}
		startupLength := make([]byte, 4)
		if _, readErr := io.ReadFull(secured, startupLength); readErr != nil || binary.BigEndian.Uint32(startupLength) < 8 {
			serverResult <- errInvalidPostgresSCRAMInitial
			return
		}
		startup := make([]byte, binary.BigEndian.Uint32(startupLength)-4)
		if _, readErr := io.ReadFull(secured, startup); readErr != nil {
			serverResult <- readErr
			return
		}
		if writeErr := writePostgresTestMessage(secured, 'R', append([]byte{0, 0, 0, 10}, []byte("SCRAM-SHA-256\x00\x00")...)); writeErr != nil {
			serverResult <- writeErr
			return
		}
		if scramErr := servePostgresSCRAMTest(secured, postgresSCRAMTestOptions{}); scramErr != nil {
			serverResult <- scramErr
			return
		}
		if writeErr := writePostgresTestMessage(secured, 'R', []byte{0, 0, 0, 0}); writeErr != nil {
			serverResult <- writeErr
			return
		}
		serverResult <- writePostgresTestMessage(secured, 'Z', []byte{'I'})
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := net.LookupPort("tcp", portText)
	err = ProbeDatabaseAuthentication(context.Background(), model.DatabaseInstance{
		Protocol: "postgres", Address: host, Port: port, TLSMode: dbtls.ModeVerifyCA, TLSCAPEM: string(certificatePEM),
	}, "probe", "secret")
	if err != nil {
		t.Fatalf("ProbeDatabaseAuthentication() error = %v", err)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestProbeRedisConnectionDoesNotExtendUpstreamHandshakeDeadline(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	wrapped, err := newUpstreamHandshakeConn(context.Background(), client, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buffer := make([]byte, 1024)
		_, _ = server.Read(buffer)
	}()

	started := time.Now()
	err = probeRedisAuthenticationOnConnection(wrapped, "probe", "secret")
	if err == nil {
		t.Fatal("silent Redis probe unexpectedly succeeded")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("Redis probe extended the armed upstream deadline: %s", elapsed)
	}
}

func writePostgresTestMessage(conn net.Conn, kind byte, payload []byte) error {
	message := make([]byte, 5+len(payload))
	message[0] = kind
	binary.BigEndian.PutUint32(message[1:5], uint32(4+len(payload)))
	copy(message[5:], payload)
	_, err := conn.Write(message)
	return err
}

var (
	errInvalidPostgresSSLRequest   = postgresProbeTestError("invalid PostgreSQL SSL request")
	errInvalidPostgresSCRAMInitial = postgresProbeTestError("invalid PostgreSQL SCRAM initial response")
	errInvalidPostgresSCRAMFinal   = postgresProbeTestError("invalid PostgreSQL SCRAM final response")
)

type postgresProbeTestError string

func (err postgresProbeTestError) Error() string { return string(err) }
