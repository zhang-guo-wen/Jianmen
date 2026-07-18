package dbproxy

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
)

func TestReadPostgresStartupMessageReadsFragmentedFrame(t *testing.T) {
	message := postgresStartupTestMessage("bastion-user", "app")
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go writeFragments(server, message, 1, 2, 3, 1)

	first := make([]byte, 1)
	if _, err := io.ReadFull(client, first); err != nil {
		t.Fatal(err)
	}
	startup, err := readPostgresStartupMessage(client, first[0])
	if err != nil {
		t.Fatal(err)
	}
	username, database, err := parsePostgresStartupMessage(startup)
	if err != nil {
		t.Fatal(err)
	}
	if username != "bastion-user" || database != "app" {
		t.Fatalf("startup parameters = (%q, %q)", username, database)
	}
}

func TestReadPostgresStartupMessageRejectsOversizedDeclaration(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, maxPostgresStartupMessageBytes+1)
	go func() {
		_, _ = server.Write(header)
	}()

	first := make([]byte, 1)
	if _, err := io.ReadFull(client, first); err != nil {
		t.Fatal(err)
	}
	if _, err := readPostgresStartupMessage(client, first[0]); err == nil {
		t.Fatal("oversized PostgreSQL StartupMessage was accepted")
	}
}

func TestReadPostgresStartupMessageRejectsTruncatedFrame(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	go func() {
		defer server.Close()
		header := make([]byte, 8)
		binary.BigEndian.PutUint32(header[:4], 32)
		binary.BigEndian.PutUint32(header[4:], postgresProtocolVersion30)
		_, _ = server.Write(header)
	}()

	first := make([]byte, 1)
	if _, err := io.ReadFull(client, first); err != nil {
		t.Fatal(err)
	}
	if _, err := readPostgresStartupMessage(client, first[0]); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("readPostgresStartupMessage() error = %v, want unexpected EOF", err)
	}
}

func TestReadPostgresPasswordMessageReadsFragmentedFrame(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	message := postgresTestMessage('p', []byte(" secret with spaces \x00"))
	go writeFragments(server, message, 2, 1, 3, 2)

	password, err := readPostgresPasswordMessage(client)
	if err != nil {
		t.Fatal(err)
	}
	if password != " secret with spaces " {
		t.Fatalf("password = %q", password)
	}
}

func TestReadPostgresPasswordMessageRejectsOversizedAndTruncatedFrames(t *testing.T) {
	t.Run("oversized", func(t *testing.T) {
		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		header := []byte{'p', 0, 0, 0, 0}
		binary.BigEndian.PutUint32(header[1:], maxPostgresAuthMessageBytes+1)
		go func() { _, _ = server.Write(header) }()
		if _, err := readPostgresPasswordMessage(client); err == nil {
			t.Fatal("oversized PostgreSQL PasswordMessage was accepted")
		}
	})

	t.Run("truncated", func(t *testing.T) {
		client, server := net.Pipe()
		defer client.Close()
		go func() {
			defer server.Close()
			header := []byte{'p', 0, 0, 0, 12}
			_, _ = server.Write(append(header, []byte("short")...))
		}()
		if _, err := readPostgresPasswordMessage(client); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("readPostgresPasswordMessage() error = %v, want unexpected EOF", err)
		}
	})
}

func TestReadPostgresMessageReadsFragmentedFrameAndRejectsBadLength(t *testing.T) {
	t.Run("fragmented", func(t *testing.T) {
		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		raw := postgresTestMessage('R', []byte{0, 0, 0, 0})
		go writeFragments(server, raw, 1, 1, 2, 1)
		message, err := readPostgresMessage(client, maxPostgresAuthMessageBytes)
		if err != nil {
			t.Fatal(err)
		}
		if message.kind != 'R' || len(message.payload) != 4 {
			t.Fatalf("message = kind %q payload %x", message.kind, message.payload)
		}
	})

	t.Run("invalid short length", func(t *testing.T) {
		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		go func() { _, _ = server.Write([]byte{'R', 0, 0, 0, 3}) }()
		if _, err := readPostgresMessage(client, maxPostgresAuthMessageBytes); err == nil {
			t.Fatal("invalid PostgreSQL message length was accepted")
		}
	})
}

func postgresStartupTestMessage(username, database string) []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, postgresProtocolVersion30)
	for _, pair := range [][2]string{{"user", username}, {"database", database}} {
		payload = append(payload, pair[0]...)
		payload = append(payload, 0)
		payload = append(payload, pair[1]...)
		payload = append(payload, 0)
	}
	payload = append(payload, 0)
	message := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(message[:4], uint32(len(message)))
	copy(message[4:], payload)
	return message
}

func postgresTestMessage(kind byte, payload []byte) []byte {
	message := make([]byte, 5+len(payload))
	message[0] = kind
	binary.BigEndian.PutUint32(message[1:5], uint32(4+len(payload)))
	copy(message[5:], payload)
	return message
}

func writeFragments(conn net.Conn, data []byte, sizes ...int) {
	offset := 0
	for _, size := range sizes {
		if offset >= len(data) {
			return
		}
		end := offset + size
		if end > len(data) {
			end = len(data)
		}
		_, _ = conn.Write(data[offset:end])
		offset = end
	}
	if offset < len(data) {
		_, _ = conn.Write(data[offset:])
	}
}

func TestParsePostgresStartupMessageRejectsMalformedParameters(t *testing.T) {
	message := postgresStartupTestMessage("user", "database")
	message = message[:len(message)-1]
	binary.BigEndian.PutUint32(message[:4], uint32(len(message)))
	if _, _, err := parsePostgresStartupMessage(message); err == nil ||
		!strings.Contains(err.Error(), "terminator") {
		t.Fatalf("parsePostgresStartupMessage() error = %v", err)
	}
}
