package dbproxy

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func FuzzReadPostgresStartupMessage(f *testing.F) {
	valid := postgresStartupMessageForVersion(
		3,
		0,
		[][2]string{{"user", "fuzz-user"}, {"database", "fuzz-db"}},
	)
	oversized := make([]byte, 4)
	binary.BigEndian.PutUint32(oversized, maxPostgresStartupMessageBytes+1)
	f.Add(valid)
	f.Add(valid[:3])
	f.Add(valid[:len(valid)-1])
	f.Add(oversized)
	f.Add([]byte{0, 0, 0, 7, 0, 3, 0, 0})

	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) == 0 {
			return
		}
		connection := &postgresFuzzConn{reader: bytes.NewReader(raw[1:])}
		message, err := readPostgresStartupMessage(connection, raw[0])
		if err != nil {
			return
		}
		if len(message) < minPostgresStartupMessageBytes ||
			len(message) > maxPostgresStartupMessageBytes {
			t.Fatalf("successful StartupMessage size = %d", len(message))
		}
		if declared := int(binary.BigEndian.Uint32(message[:4])); declared != len(message) {
			t.Fatalf("successful StartupMessage length = %d, declared %d", len(message), declared)
		}
		startup, err := parsePostgresStartup(message)
		if err == nil && startup.username == "" {
			t.Fatal("parsed StartupMessage has an empty user")
		}
	})
}

func FuzzReadPostgresTypedMessage(f *testing.F) {
	validQuery := postgresTestMessage('Q', []byte("select 1\x00"))
	validReady := postgresTestMessage('Z', []byte{'I'})
	truncated := validQuery[:len(validQuery)-1]
	shortLength := []byte{'Q', 0, 0, 0, 3}
	oversized := []byte{'Q', 0, 0, 0, 0}
	binary.BigEndian.PutUint32(oversized[1:], maxPostgresAuthMessageBytes+1)
	f.Add(byte(0), validQuery)
	f.Add(byte(1), validReady)
	f.Add(byte(0), truncated)
	f.Add(byte(0), shortLength)
	f.Add(byte(1), oversized)

	f.Fuzz(func(t *testing.T, direction byte, raw []byte) {
		connection := &postgresFuzzConn{reader: bytes.NewReader(raw)}
		message, err := readPostgresMessage(connection, maxPostgresAuthMessageBytes)
		if err != nil {
			return
		}
		encoded := message.raw()
		if len(encoded) > len(raw) {
			t.Fatalf("decoded frame size = %d, input = %d", len(encoded), len(raw))
		}
		if !bytes.Equal(encoded, raw[:len(encoded)]) {
			t.Fatalf("decoded frame changed bytes: %x != %x", encoded, raw[:len(encoded)])
		}
		if direction%2 == 0 {
			_ = validPostgresFrontendMessage(message.kind, message.payload)
		} else {
			_ = validPostgresBackendMessage(message.kind, message.payload)
		}
	})
}

type postgresFuzzConn struct {
	reader *bytes.Reader
}

func (connection *postgresFuzzConn) Read(buffer []byte) (int, error) {
	return connection.reader.Read(buffer)
}

func (connection *postgresFuzzConn) Write(buffer []byte) (int, error) {
	return len(buffer), nil
}

func (connection *postgresFuzzConn) Close() error {
	return nil
}

func (connection *postgresFuzzConn) LocalAddr() net.Addr {
	return postgresFuzzAddr("local")
}

func (connection *postgresFuzzConn) RemoteAddr() net.Addr {
	return postgresFuzzAddr("remote")
}

func (connection *postgresFuzzConn) SetDeadline(time.Time) error {
	return nil
}

func (connection *postgresFuzzConn) SetReadDeadline(time.Time) error {
	return nil
}

func (connection *postgresFuzzConn) SetWriteDeadline(time.Time) error {
	return nil
}

type postgresFuzzAddr string

func (address postgresFuzzAddr) Network() string {
	return "fuzz"
}

func (address postgresFuzzAddr) String() string {
	return string(address)
}
