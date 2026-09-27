package dbproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestRedisInitialCommandParseLogsAreFixedAndSanitized(t *testing.T) {
	const fixedMessage = "redis gateway rejected invalid initial authentication command"
	tests := []struct {
		name   string
		secret string
		input  []byte
	}{
		{
			name:   "inline AUTH",
			secret: "inline-password-secret",
			input:  []byte("AUTH inline-password-secret\r\n"),
		},
		{
			name:   "single argument AUTH",
			secret: "single-bulk-auth-secret",
			input:  redisObserverTestCommand("AUTH", "single-bulk-auth-secret"),
		},
		{
			name:   "single top-level bulk AUTH",
			secret: "top-level-bulk-secret",
			input:  []byte("$26\r\nAUTH top-level-bulk-secret\r\n"),
		},
		{
			name:   "non AUTH with secret argument",
			secret: "non-auth-argument-secret",
			input:  redisObserverTestCommand("GET", "non-auth-argument-secret"),
		},
		{
			name:   "non AUTH secret command",
			secret: "non-auth-command-secret",
			input:  redisObserverTestCommand("non-auth-command-secret", "key"),
		},
		{
			name:   "HELLO AUTH",
			secret: "hello-auth-secret",
			input:  redisObserverTestCommand("HELLO", "3", "AUTH", "user", "hello-auth-secret"),
		},
		{
			name:   "AUTH with extra argument",
			secret: "extra-auth-secret",
			input:  redisObserverTestCommand("AUTH", "user", "extra-auth-secret", "extra"),
		},
		{
			name:   "invalid array length",
			secret: "array-password-secret",
			input:  []byte("*array-password-secret\r\n"),
		},
		{
			name:   "non bulk argument",
			secret: "bulk-password-secret",
			input:  []byte("*2\r\n$4\r\nAUTH\r\n+bulk-password-secret\r\n"),
		},
		{
			name:   "malformed bulk terminator",
			secret: "secret-terminator",
			input:  []byte("*2\r\n$4\r\nAUTH\r\n$17\r\nsecret-terminator\nX"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			gateway := &Gateway{logger: logger}
			connection := newObserverCopyConn(tt.input[1:])
			if established := gateway.handleRedis(context.Background(), connection, tt.input[0]); established != nil {
				t.Fatal("invalid initial authentication command established a session")
			}

			got := logs.String()
			var record map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &record); err != nil {
				t.Fatalf("parse log %q: %v", got, err)
			}
			if record["msg"] != fixedMessage {
				t.Fatalf("log message = %q, want %q", record["msg"], fixedMessage)
			}
			lowerLog := strings.ToLower(got)
			if strings.Contains(lowerLog, strings.ToLower(tt.secret)) ||
				strings.Contains(lowerLog, strings.ToLower(string(tt.input))) {
				t.Fatalf("log exposed invalid command input or password: %s", got)
			}
			for _, attribute := range []string{"cmd", "args", "error", "count"} {
				if _, exists := record[attribute]; exists {
					t.Fatalf("initial rejection log included variable %q attribute: %s", attribute, got)
				}
			}
		})
	}
}

func TestRedisInitialAccountResolutionFailureLogIsFixedAndSanitized(t *testing.T) {
	const (
		username = "client-controlled-username-secret"
		password = "client-controlled-password-secret"
	)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	gateway := &Gateway{logger: logger}
	command := redisObserverTestCommand("AUTH", username, password)
	connection := newObserverCopyConn(command[1:])

	if established := gateway.handleRedis(context.Background(), connection, command[0]); established != nil {
		t.Fatal("invalid compact username established a session")
	}

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &record); err != nil {
		t.Fatalf("parse log %q: %v", logs.String(), err)
	}
	if record["msg"] != redisInitialAuthenticationRejectedLog {
		t.Fatalf("log message = %q, want %q", record["msg"], redisInitialAuthenticationRejectedLog)
	}
	got := logs.String()
	for _, secret := range []string{username, password, "invalid username format"} {
		if strings.Contains(got, secret) {
			t.Fatalf("account resolution log exposed %q: %s", secret, got)
		}
	}
	for _, attribute := range []string{"username", "user", "account", "error"} {
		if _, exists := record[attribute]; exists {
			t.Fatalf("initial rejection log included variable %q attribute: %s", attribute, got)
		}
	}
}
