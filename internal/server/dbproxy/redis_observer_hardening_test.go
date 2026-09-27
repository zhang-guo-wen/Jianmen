package dbproxy

import (
	"bytes"
	"strings"
	"testing"
)

func TestRedisServerRelayBuffersIncompleteMatchedResponse(t *testing.T) {
	observer := &redisObserver{sink: &captureSink{}}
	if forward, decision := observer.ObserveClientRelayBytes(redisObserverTestCommand("GET", "key")); decision != nil || len(forward) == 0 {
		t.Fatalf("GET forward=%q decision=%#v", forward, decision)
	}

	forward, decision := observer.ObserveServerRelayBytes([]byte("+O"))
	if decision != nil || len(forward) != 0 {
		t.Fatalf("partial response forwarded=%q decision=%#v", forward, decision)
	}

	forward, decision = observer.ObserveServerRelayBytes([]byte("K\r\n"))
	if decision != nil || !bytes.Equal(forward, []byte("+OK\r\n")) {
		t.Fatalf("complete response forwarded=%q decision=%#v", forward, decision)
	}
}

func TestRedisServerRelayRejectsZeroSlotResponseWithoutForwarding(t *testing.T) {
	observer := &redisObserver{sink: &captureSink{}}

	forward, decision := observer.ObserveServerRelayBytes([]byte("+UNSOLICITED\r\n"))

	if len(forward) != 0 {
		t.Fatalf("zero-slot response forwarded: %q", forward)
	}
	requireObserverDenied(t, decision)
}

func TestRedisServerRelayPreservesMatchedPrefixBeforeExtraResponse(t *testing.T) {
	sink := &captureSink{}
	observer := &redisObserver{sink: sink}
	if _, decision := observer.ObserveClientRelayBytes(redisObserverTestCommand("GET", "key")); decision != nil {
		t.Fatalf("GET denied: %#v", decision)
	}

	forward, decision := observer.ObserveServerRelayBytes([]byte("+OK\r\n+EXTRA\r\n"))

	if !bytes.Equal(forward, []byte("+OK\r\n")) {
		t.Fatalf("safe response prefix = %q, want +OK only", forward)
	}
	requireObserverDenied(t, decision)
	if len(sink.finished) != 1 || sink.finished[0].Status != queryStatusSuccess {
		t.Fatalf("paired audit = %#v, want one successful GET", sink.finished)
	}
}

func TestRedisServerRelayDoesNotReleaseIncompleteExtraResponse(t *testing.T) {
	observer := &redisObserver{sink: &captureSink{}}
	if _, decision := observer.ObserveClientRelayBytes(redisObserverTestCommand("GET", "key")); decision != nil {
		t.Fatalf("GET denied: %#v", decision)
	}

	forward, decision := observer.ObserveServerRelayBytes([]byte("+OK\r\n+EX"))

	if decision != nil {
		t.Fatalf("incomplete extra response denied too early: %#v", decision)
	}
	if !bytes.Equal(forward, []byte("+OK\r\n")) {
		t.Fatalf("forwarded bytes = %q, want matched complete response only", forward)
	}

	forward, decision = observer.ObserveServerRelayBytes([]byte("TRA\r\n"))
	if len(forward) != 0 {
		t.Fatalf("completed zero-slot response forwarded: %q", forward)
	}
	requireObserverDenied(t, decision)
}

func TestRedisObjectAndXInfoRejectInvalidSubcommandsBeforeAudit(t *testing.T) {
	const secret = "must-not-enter-audit"
	tests := []struct {
		name string
		args []string
	}{
		{name: "object unknown", args: []string{"OBJECT", "SECRET", secret}},
		{name: "object help arity", args: []string{"OBJECT", "HELP", secret}},
		{name: "object encoding arity", args: []string{"OBJECT", "ENCODING", secret, "extra"}},
		{name: "xinfo unknown", args: []string{"XINFO", "SECRET", secret}},
		{name: "xinfo consumers missing group", args: []string{"XINFO", "CONSUMERS", secret}},
		{name: "xinfo stream invalid option", args: []string{"XINFO", "STREAM", secret, "SECRET"}},
		{name: "xinfo help arity", args: []string{"XINFO", "HELP", secret}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &captureSink{}
			observer := &redisObserver{sink: sink}

			forward, decision := observer.ObserveClientRelayBytes(redisObserverTestCommand(tt.args...))

			if len(forward) != 0 {
				t.Fatalf("invalid subcommand forwarded: %q", forward)
			}
			requireObserverDenied(t, decision)
			for _, query := range sink.queries {
				if strings.Contains(query, secret) {
					t.Fatalf("secret persisted in audit query %q", query)
				}
			}
			if len(sink.queries) != 0 {
				t.Fatalf("invalid subcommand audited: %v", sink.queries)
			}
		})
	}
}

func TestRedisObjectAndXInfoUseValidatedKeyPositions(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantAudit string
	}{
		{name: "object encoding", args: []string{"OBJECT", "ENCODING", "object-key"}, wantAudit: "OBJECT object-key [REDACTED]"},
		{name: "xinfo consumers", args: []string{"XINFO", "CONSUMERS", "stream-key", "group-secret"}, wantAudit: "XINFO stream-key [REDACTED]"},
		{name: "xinfo groups", args: []string{"XINFO", "GROUPS", "stream-key"}, wantAudit: "XINFO stream-key [REDACTED]"},
		{name: "xinfo stream", args: []string{"XINFO", "STREAM", "stream-key", "FULL", "COUNT", "10"}, wantAudit: "XINFO stream-key [REDACTED]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &captureSink{}
			observer := &redisObserver{sink: sink}

			forward, decision := observer.ObserveClientRelayBytes(redisObserverTestCommand(tt.args...))

			if decision != nil {
				t.Fatalf("valid subcommand denied: %#v", decision)
			}
			if len(forward) == 0 {
				t.Fatal("valid subcommand was not forwarded")
			}
			if len(sink.queries) != 1 || sink.queries[0] != tt.wantAudit {
				t.Fatalf("audit = %v, want %q", sink.queries, tt.wantAudit)
			}
		})
	}
}
