package dbproxy

import (
	"bytes"
	"strings"
	"testing"
)

func TestRedisAllowedComplexCommandsHaveValidatedKeySpecs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantAudit string
	}{
		{name: "BITOP", args: []string{"BITOP", "AND", "dest", "source-one", "source-two"}, wantAudit: "BITOP dest [REDACTED]"},
		{name: "XREAD", args: []string{"XREAD", "COUNT", "2", "STREAMS", "stream-one", "stream-two", "0", "0"}, wantAudit: "XREAD stream-one [REDACTED]"},
		{name: "XREADGROUP", args: []string{"XREADGROUP", "GROUP", "workers", "one", "STREAMS", "stream-one", ">"}, wantAudit: "XREADGROUP stream-one [REDACTED]"},
		{name: "XGROUP", args: []string{"XGROUP", "CREATE", "stream-one", "workers", "$"}, wantAudit: "XGROUP stream-one [REDACTED]"},
		{name: "XINFO", args: []string{"XINFO", "STREAM", "stream-one"}, wantAudit: "XINFO stream-one [REDACTED]"},
		{name: "LMPOP", args: []string{"LMPOP", "2", "list-one", "list-two", "LEFT"}, wantAudit: "LMPOP list-one [REDACTED]"},
		{name: "BLMPOP", args: []string{"BLMPOP", "1", "2", "list-one", "list-two", "LEFT"}, wantAudit: "BLMPOP list-one [REDACTED]"},
		{name: "ZINTER", args: []string{"ZINTER", "2", "sorted-one", "sorted-two"}, wantAudit: "ZINTER sorted-one [REDACTED]"},
		{name: "ZDIFF", args: []string{"ZDIFF", "2", "sorted-one", "sorted-two"}, wantAudit: "ZDIFF sorted-one [REDACTED]"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := redisObserverTestCommand(test.args...)
			client := newObserverCopyConn(command)
			upstream := newObserverCopyConn(nil)
			sink := &captureSink{}
			copyClientToUpstream(client, upstream, &redisObserver{sink: sink})
			if !bytes.Equal(upstream.writes.Bytes(), command) {
				t.Fatalf("upstream = %q, want command", upstream.writes.Bytes())
			}
			if len(sink.queries) != 1 || sink.queries[0] != test.wantAudit {
				t.Fatalf("audit = %v, want %q", sink.queries, test.wantAudit)
			}
			if strings.Contains(strings.Join(sink.queries, " "), "source-two") ||
				strings.Contains(strings.Join(sink.queries, " "), "sorted-two") {
				t.Fatalf("audit exposed non-primary key/argument: %v", sink.queries)
			}
		})
	}
}

func TestRedisRejectsComplexCommandWhenKeySyntaxCannotBeValidated(t *testing.T) {
	for _, args := range [][]string{
		{"BITOP", "AND", "dest"},
		{"XREAD", "COUNT", "2", "stream-without-streams-keyword"},
		{"XREAD", "STREAMS", "stream-one"},
		{"XGROUP", "UNKNOWN", "stream-one"},
		{"LMPOP", "2", "only-one-list", "LEFT"},
		{"ZINTER", "2", "only-one-key"},
	} {
		client := newObserverCopyConn(redisObserverTestCommand(args...))
		upstream := newObserverCopyConn(nil)
		copyClientToUpstream(client, upstream, &redisObserver{sink: &captureSink{}})
		if upstream.writes.Len() != 0 {
			t.Fatalf("invalid %v reached upstream: %q", args, upstream.writes.Bytes())
		}
	}
}

func TestEveryAllowedRedisCommandHasAnExplicitAuditKeySpec(t *testing.T) {
	for command := range allowedRedisPostAuthCommands {
		if !redisCommandHasAuditSpec(command) {
			t.Errorf("allowed command %s has no audit key specification", command)
		}
	}
}
