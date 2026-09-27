package dbproxy

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

func TestProbeErrorsDoNotExposeUpstreamTextOrPayload(t *testing.T) {
	t.Run("Redis response", func(t *testing.T) {
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()
		go func() {
			buffer := make([]byte, 1024)
			_, _ = server.Read(buffer)
			_, _ = server.Write([]byte("-ERR upstream-secret\r\n"))
		}()
		err := probeRedisAuthenticationOnConnection(client, "probe", "password")
		if err == nil {
			t.Fatal("probe unexpectedly succeeded")
		}
		if strings.Contains(err.Error(), "upstream-secret") {
			t.Fatalf("probe error exposed upstream text: %v", err)
		}
	})

	t.Run("MySQL malformed continuation", func(t *testing.T) {
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()
		_, _, err := readMySQLCachingSHA2ProbeMoreData(client, &mysqlPacket{payload: []byte{0x01, 0xde, 0xad, 0xbe, 0xef}})
		if err == nil {
			t.Fatal("malformed packet unexpectedly succeeded")
		}
		if strings.Contains(strings.ToLower(err.Error()), "deadbeef") {
			t.Fatalf("probe error exposed payload hex: %v", err)
		}
	})
}

func TestAuthenticationLogsDoNotContainUsernamesAccountIDsOrDynamicErrors(t *testing.T) {
	gateway, _ := newCrossProtocolGateway(t, "redis")
	var output bytes.Buffer
	gateway.logger = slog.New(slog.NewJSONHandler(&output, nil))
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan *gatewayConn, 1)
	go func() {
		done <- gateway.handleMySQL(context.Background(), server)
	}()
	if _, err := readMySQLPacket(client); err != nil {
		t.Fatal(err)
	}
	username := databaseCompactUsername()
	if _, err := client.Write(protocolValidationMySQLLogin(username)); err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, _ = io.Copy(io.Discard, client)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not return")
	}
	logs := output.String()
	for _, forbidden := range []string{username, "db-account-1", "belongs to", "instance, not"} {
		if strings.Contains(logs, forbidden) {
			t.Fatalf("authentication log exposed %q: %s", forbidden, logs)
		}
	}
}
