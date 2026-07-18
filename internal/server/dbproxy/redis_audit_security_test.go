package dbproxy

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestRedisRelayForwardsValuesButRedactsThemFromObserverAudit(t *testing.T) {
	tests := []struct {
		name      string
		command   []byte
		wantAudit string
		secrets   []string
	}{
		{
			name:      "SET value",
			command:   redisObserverTestCommand("SET", "password", "set-value-secret"),
			wantAudit: "SET password [REDACTED]",
			secrets:   []string{"set-value-secret"},
		},
		{
			name:      "MSET values",
			command:   redisObserverTestCommand("MSET", "first-key", "mset-first-secret", "second-key", "mset-second-secret"),
			wantAudit: "MSET first-key [REDACTED]",
			secrets:   []string{"mset-first-secret", "mset-second-secret"},
		},
		{
			name:      "EVAL script and argument",
			command:   redisObserverTestCommand("EVAL", "return ARGV[1]", "1", "eval-key", "eval-token-secret"),
			wantAudit: "EVAL eval-key [REDACTED]",
			secrets:   []string{"return ARGV[1]", "eval-token-secret"},
		},
		{
			name:      "unsafe key",
			command:   redisObserverTestCommand("SET", "unsafe-key\nline", "unsafe-key-value-secret"),
			wantAudit: "SET [REDACTED] [REDACTED]",
			secrets:   []string{"unsafe-key\nline", "unsafe-key-value-secret"},
		},
		{
			name:      "GEOADD key",
			command:   redisObserverTestCommand("GEOADD", "geo-key", "13.1", "52.5", "member-secret"),
			wantAudit: "GEOADD geo-key [REDACTED]",
			secrets:   []string{"13.1", "52.5", "member-secret"},
		},
		{
			name:      "BITFIELD key",
			command:   redisObserverTestCommand("BITFIELD", "bits-key", "SET", "u8", "0", "255"),
			wantAudit: "BITFIELD bits-key [REDACTED]",
			secrets:   []string{"u8", "255"},
		},
		{
			name:      "SORT key",
			command:   redisObserverTestCommand("SORT", "sort-key", "BY", "weight-secret", "STORE", "destination-secret"),
			wantAudit: "SORT sort-key [REDACTED]",
			secrets:   []string{"weight-secret", "destination-secret"},
		},
		{
			name:      "SINTERCARD skips numkeys",
			command:   redisObserverTestCommand("SINTERCARD", "2", "first-key", "second-key", "LIMIT", "1"),
			wantAudit: "SINTERCARD first-key [REDACTED]",
			secrets:   []string{"second-key"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newObserverCopyConn(tt.command)
			upstream := newObserverCopyConn(nil)
			sink := &captureSink{}

			copyClientToUpstream(client, upstream, &redisObserver{sink: sink})

			if got := upstream.writes.Bytes(); !bytes.Equal(got, tt.command) {
				t.Fatalf("upstream bytes = %q, want original command %q", got, tt.command)
			}
			if len(sink.queries) != 1 || sink.queries[0] != tt.wantAudit {
				t.Fatalf("audit queries = %v, want %q", sink.queries, tt.wantAudit)
			}
			auditText := strings.Join(sink.queries, " ") + fmt.Sprint(sink.details)
			for _, secret := range tt.secrets {
				if strings.Contains(auditText, secret) {
					t.Fatalf("observer audit exposed %q: %q", secret, auditText)
				}
				if !bytes.Contains(upstream.writes.Bytes(), []byte(secret)) {
					t.Fatalf("upstream did not receive original value %q", secret)
				}
			}
		})
	}
}
