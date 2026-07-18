package dbproxy

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

func TestReadRESPCommandFromBufRejectsOversizedAuthenticationFrames(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "too many arguments", input: "*17\r\n"},
		{name: "512MiB bulk declaration", input: "*3\r\n$4\r\nAUTH\r\n$1\r\nu\r\n$536870912\r\n"},
		{name: "oversized bulk declaration", input: "*2\r\n$4\r\nAUTH\r\n$16385\r\n"},
		{name: "negative bulk declaration", input: "*2\r\n$4\r\nAUTH\r\n$-1\r\n"},
		{name: "overflowing bulk declaration", input: "*2\r\n$4\r\nAUTH\r\n$999999999999999999999999999\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			go func() { _, _ = server.Write([]byte(tt.input)) }()

			first, err := client.Read(make([]byte, 1))
			if err != nil || first != 1 {
				t.Fatalf("read first byte: n=%d err=%v", first, err)
			}
			_ = client.SetReadDeadline(time.Now().Add(time.Second))
			_, _, _, err = readRESPCommandFromBuf(client, '*')
			if err == nil {
				t.Fatal("expected authentication frame to be rejected")
			}
		})
	}
}

func TestReadRESPCommandFromBufRejectsOversizedTotalAuthenticationCommand(t *testing.T) {
	var input strings.Builder
	input.WriteString("*4\r\n")
	for i := 0; i < 4; i++ {
		input.WriteString("$16384\r\n")
		input.WriteString(strings.Repeat("x", 16384))
		input.WriteString("\r\n")
	}

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go func() { _, _ = server.Write([]byte(input.String())) }()
	first := make([]byte, 1)
	if _, err := client.Read(first); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, _, err := readRESPCommandFromBuf(client, first[0]); err == nil {
		t.Fatal("expected oversized total authentication command to be rejected")
	}
}

func TestReadRESPCommandFromBufRejectsTruncatedBulkWithoutPanic(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go func() { _, _ = server.Write([]byte("*2\r\n$4\r\nAUTH\r\n$4\r\nab")) }()
	first := make([]byte, 1)
	if _, err := client.Read(first); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, _, _, err := readRESPCommandFromBuf(client, first[0]); err == nil {
		t.Fatal("expected truncated bulk string to be rejected")
	}
}

func TestReadRESPCommandFromBufConcurrentOversizedFrames(t *testing.T) {
	const clients = 32
	errs := make(chan error, clients)
	for i := 0; i < clients; i++ {
		go func() {
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			go func() { _, _ = server.Write([]byte("*17\r\n")) }()
			first := make([]byte, 1)
			if _, err := client.Read(first); err != nil {
				errs <- err
				return
			}
			_, _, _, err := readRESPCommandFromBuf(client, first[0])
			errs <- err
		}()
	}
	for i := 0; i < clients; i++ {
		if err := <-errs; err == nil {
			t.Fatal("expected oversized frame to be rejected")
		}
	}
}

func TestReadRESPCommandFromBufRejectsUnterminatedOversizedFirstLine(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go func() { _, _ = server.Write([]byte("*" + strings.Repeat("1", 1025))) }()

	buf := make([]byte, 1)
	if _, err := client.Read(buf); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	_, _, _, err := readRESPCommandFromBuf(client, buf[0])
	if err == nil {
		t.Fatal("expected oversized unterminated line to be rejected")
	}
}

func TestHandleRedisRejectsHELLOWithoutStartingSession(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{name: "RESP2", command: "*2\r\n$5\r\nHELLO\r\n$1\r\n2\r\n"},
		{name: "RESP3", command: "*2\r\n$5\r\nHELLO\r\n$1\r\n3\r\n"},
		{name: "RESP3 AUTH", command: "*5\r\n$5\r\nHELLO\r\n$1\r\n3\r\n$4\r\nAUTH\r\n$4\r\nuser\r\n$12\r\nhello-secret\r\n"},
		{name: "RESP3 SETNAME", command: "*4\r\n$5\r\nHELLO\r\n$1\r\n3\r\n$7\r\nSETNAME\r\n$6\r\nclient\r\n"},
		{name: "RESP3 AUTH SETNAME", command: "*7\r\n$5\r\nHELLO\r\n$1\r\n3\r\n$4\r\nAUTH\r\n$4\r\nuser\r\n$12\r\nhello-secret\r\n$7\r\nSETNAME\r\n$6\r\nclient\r\n"},
		{name: "RESP2 AUTH SETNAME", command: "*7\r\n$5\r\nHELLO\r\n$1\r\n2\r\n$4\r\nAUTH\r\n$4\r\nuser\r\n$12\r\nhello-secret\r\n$7\r\nSETNAME\r\n$6\r\nclient\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, client := net.Pipe()
			defer server.Close()
			defer client.Close()
			gateway := &Gateway{logger: slog.Default()}
			done := make(chan *gatewayConn, 1)
			go func() {
				done <- gateway.handleRedis(context.Background(), server, tt.command[0])
			}()

			if _, err := client.Write([]byte(tt.command[1:])); err != nil {
				t.Fatalf("write HELLO command: %v", err)
			}
			_ = client.SetReadDeadline(time.Now().Add(time.Second))
			response := make([]byte, 256)
			n, err := client.Read(response)
			if err != nil {
				t.Fatalf("read HELLO response: %v", err)
			}
			got := string(response[:n])
			if !strings.HasPrefix(got, "-NOPROTO ") {
				t.Fatalf("HELLO response = %q, want -NOPROTO rejection", got)
			}
			if strings.Contains(got, "hello-secret") {
				t.Fatal("HELLO response exposed the authentication password")
			}
			if connection := <-done; connection != nil {
				t.Fatal("HELLO unexpectedly established a gateway session")
			}
		})
	}
}
