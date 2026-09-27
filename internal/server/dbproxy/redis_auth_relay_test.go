package dbproxy

import (
	"bytes"
	"strings"
	"testing"
)

func TestRedisAUTHReaderPreservesPipelinedCommandForRelayObserver(t *testing.T) {
	const password = "do-not-audit-auth-secret"
	auth := redisObserverTestCommand("AUTH", "R000100001", password)
	set := redisObserverTestCommand("SET", "key", "value")
	wire := append(append([]byte(nil), auth...), set...)
	conn := newObserverCopyConn(wire[1:])

	cmd, args, _, err := readRESPCommandFromBuf(conn, wire[0])
	if err != nil {
		t.Fatalf("read AUTH: %v", err)
	}
	if cmd != "AUTH" || len(args) != 3 {
		t.Fatalf("AUTH parsed as cmd=%q args=%v", cmd, args)
	}

	if got := conn.input.Len(); got != len(set) {
		t.Fatalf("bytes preserved for relay = %d, want %d", got, len(set))
	}

	sink := &captureSink{}
	observer := &redisObserver{sink: sink}
	upstream := newObserverCopyConn(nil)
	copyClientToUpstream(conn, upstream, observer)
	if got := upstream.writes.Bytes(); !bytes.Equal(got, set) {
		t.Fatalf("relayed bytes = %q, want complete SET frame %q", got, set)
	}
	if len(sink.queries) != 1 || sink.queries[0] != "SET key [REDACTED]" {
		t.Fatalf("observed queries = %v, want only redacted SET", sink.queries)
	}
	if strings.Contains(strings.Join(sink.queries, " "), password) ||
		bytes.Contains(upstream.writes.Bytes(), []byte(password)) {
		t.Fatal("AUTH password leaked into observer audit")
	}
}
