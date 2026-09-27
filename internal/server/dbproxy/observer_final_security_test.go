package dbproxy

import (
	"bytes"
	"strings"
	"testing"
)

func TestMySQLObserverDoesNotForwardFatalResultSetPacket(t *testing.T) {
	for _, test := range []struct {
		name    string
		command byte
		payload []byte
	}{
		{name: "malformed result set", command: 0x03, payload: []byte{0xfc, 0x01}},
		{name: "malformed prepare", command: 0x16, payload: []byte{0x01}},
	} {
		t.Run(test.name, func(t *testing.T) {
			sink := &captureSink{}
			observer := newQueryObserver("mysql", sink)
			request := buildMySQLPacket(0, append([]byte{test.command}, []byte("select secret")...))
			if forward, decision := observer.(relayClientQueryObserver).ObserveClientRelayBytes(request); decision != nil || !bytes.Equal(forward, request) {
				t.Fatalf("client relay = (%x, %#v), want original request", forward, decision)
			}

			response := buildMySQLPacket(1, test.payload)
			forward, decision := observer.(relayServerQueryObserver).ObserveServerRelayBytes(response)
			if decision == nil || decision.Allowed {
				t.Fatalf("server decision = %#v, want fatal", decision)
			}
			if len(forward) != 0 {
				t.Fatalf("fatal upstream packet was forwarded: %x", forward)
			}
			if len(sink.finished) != 1 || sink.finished[0].Status != queryStatusError {
				t.Fatalf("finished audit = %#v, want one error", sink.finished)
			}
		})
	}
}

func TestDatabaseObserversRedactSQLBeforeAuditWithoutChangingRelayBytes(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		sql      string
		request  func(string) []byte
		secrets  []string
	}{
		{
			name:     "mysql literals and assignments",
			protocol: "mysql",
			sql:      "ALTER USER app IDENTIFIED BY 'mysql-secret'; INSERT INTO tokens(token, n, raw, bits) VALUES ('bearer-secret', 12345, 0xDEADBEEF, 0b101101); SET authorization = Bearer unquoted-auth-secret",
			request: func(sql string) []byte {
				return buildMySQLPacket(0, append([]byte{0x03}, []byte(sql)...))
			},
			secrets: []string{"mysql-secret", "bearer-secret", "12345", "DEADBEEF", "101101", "unquoted-auth-secret"},
		},
		{
			name:     "postgres strings numbers binary and dollar quote",
			protocol: "postgres",
			sql:      "SELECT $tag$postgres-secret$tag$, E'escape-secret', decode('A1B2', 'hex'), 987654; SET authorization = 'auth-secret'",
			request: func(sql string) []byte {
				return postgresMessage('Q', append([]byte(sql), 0))
			},
			secrets: []string{"postgres-secret", "escape-secret", "A1B2", "987654", "auth-secret"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sink := &captureSink{}
			observer := newQueryObserver(test.protocol, sink)
			request := test.request(test.sql)
			forward, decision := observer.(relayClientQueryObserver).ObserveClientRelayBytes(request)
			if decision != nil {
				t.Fatalf("decision = %#v", decision)
			}
			if !bytes.Equal(forward, request) {
				t.Fatal("observer changed bytes sent upstream")
			}
			if len(sink.queries) != 1 {
				t.Fatalf("audited queries = %v", sink.queries)
			}
			audited := sink.queries[0]
			if !strings.Contains(audited, "[REDACTED]") {
				t.Fatalf("audited SQL was not redacted: %q", audited)
			}
			for _, secret := range test.secrets {
				if strings.Contains(audited, secret) {
					t.Fatalf("audited SQL exposed %q: %q", secret, audited)
				}
				if !bytes.Contains(forward, []byte(secret)) {
					t.Fatalf("upstream request lost original literal %q", secret)
				}
			}
		})
	}
}

func TestObserverErrorResponsesNeverUseDynamicMessages(t *testing.T) {
	const injected = "secret\r\nINJECTED"
	decision := queryDecision{Allowed: false, ErrorCode: "DYNAMIC", ErrorMessage: injected}
	for _, test := range []struct {
		name     string
		observer queryObserver
	}{
		{name: "mysql", observer: &mysqlObserver{}},
		{name: "postgres", observer: &postgresObserver{}},
		{name: "redis", observer: &redisObserver{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := string(test.observer.ErrorResponse(decision))
			if strings.Contains(response, "secret") || strings.Contains(response, "INJECTED") {
				t.Fatalf("dynamic observer message reached client: %q", response)
			}
		})
	}
}

func TestPostgresPortalSuspendedCompletesOnlyCurrentExecute(t *testing.T) {
	sink := &captureSink{}
	observer := &postgresObserver{sink: sink, startupDone: true}
	parse := postgresMessage('P', append(append([]byte("statement\x00"), []byte("select generate_series(1, 10)\x00")...), 0, 0))
	bind := postgresBindNoParameters("portal", "statement")
	executeTwo := postgresMessage('E', append([]byte("portal\x00"), 0, 0, 0, 2))
	syncMessage := postgresMessage('S', nil)
	if decision := observer.ObserveClientBytes(append(append(append(parse, bind...), executeTwo...), append(executeTwo, syncMessage...)...)); decision != nil {
		t.Fatalf("client decision = %#v", decision)
	}

	observer.ObserveServerBytes(postgresMessage('1', nil))
	observer.ObserveServerBytes(postgresMessage('2', nil))
	observer.ObserveServerBytes(postgresMessage('s', nil))
	observer.ObserveServerBytes(postgresMessage('C', []byte("SELECT 8\x00")))
	observer.ObserveServerBytes(postgresMessage('Z', []byte{'I'}))

	if len(sink.finished) != 2 {
		t.Fatalf("finished = %#v, want two executes", sink.finished)
	}
	if sink.finished[0].Status != queryStatusSuccess || sink.finished[1].Status != queryStatusSuccess {
		t.Fatalf("statuses = %#v, want both success", sink.finished)
	}
	if sink.finished[1].RowsAffected == nil || *sink.finished[1].RowsAffected != 8 {
		t.Fatalf("second execute rows = %v, want 8", sink.finished[1].RowsAffected)
	}
}
