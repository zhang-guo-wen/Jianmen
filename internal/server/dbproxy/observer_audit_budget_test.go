package dbproxy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestPrepareDatabaseSQLAuditBoundsRedactionExpansion(t *testing.T) {
	const limit = 64 * 1024
	input := strings.Repeat("1,", limit/2)

	audit := prepareDatabaseSQLAudit(input, limit)

	if len(audit.text) != limit {
		t.Fatalf("bounded audit SQL = %d bytes, want %d", len(audit.text), limit)
	}
	if !audit.truncated {
		t.Fatal("expanding redaction was not marked truncated")
	}
	if audit.originalBytes != int64(len(input)) {
		t.Fatalf("original SQL bytes = %d, want %d", audit.originalBytes, len(input))
	}
	if strings.Contains(audit.text, "1") || !strings.Contains(audit.text, sqlAuditRedacted) {
		t.Fatalf("bounded audit SQL did not redact numeric literals: %q", audit.text[:128])
	}
}

func TestBoundedSQLRedactionDoesNotExposeUnquotedSensitiveAssignments(t *testing.T) {
	const limit = 1024
	input := strings.Repeat("password=x;", 1024)

	redacted, truncated := redactDatabaseSQLWithLimit(input, limit)

	if len(redacted) > limit || !truncated {
		t.Fatalf("bounded sensitive SQL = (%d bytes, truncated=%v), want at most %d and truncated", len(redacted), truncated, limit)
	}
	if strings.Contains(redacted, "password=x") {
		t.Fatalf("bounded redaction exposed a sensitive assignment: %q", redacted)
	}
}

func TestBoundedSQLRedactionNeverPersistsPartialUTF8(t *testing.T) {
	input := "select " + strings.Repeat("界", 100)

	redacted, truncated := redactDatabaseSQLWithLimit(input, 32)

	if !truncated {
		t.Fatal("multibyte SQL was not marked truncated")
	}
	if len(redacted) > 32 || !utf8.ValidString(redacted) {
		t.Fatalf("bounded multibyte SQL = (%d bytes, valid=%v), want valid UTF-8 within limit", len(redacted), utf8.ValidString(redacted))
	}
}

func TestMySQLObserverCapsPendingAuditTextByConfiguredBytes(t *testing.T) {
	const limit = 128
	sink := &captureSink{}
	observer := &mysqlObserver{
		sink:                  sink,
		maxClientMessageBytes: limit,
	}
	sql := "select " + strings.Repeat("a", 90)
	request := buildMySQLPacket(0, append([]byte{0x03}, []byte(sql)...))

	if forward, decision := observer.ObserveClientRelayBytes(request); decision != nil || len(forward) != len(request) {
		t.Fatalf("first query relay = (%d bytes, %#v), want accepted", len(forward), decision)
	}
	forward, decision := observer.ObserveClientRelayBytes(request)
	if len(forward) != 0 || decision == nil || decision.ErrorCode != observerErrorPendingLimit {
		t.Fatalf("second query relay = (%d bytes, %#v), want pending byte limit", len(forward), decision)
	}
}

func TestPostgresObserverCapsPendingAuditTextByConfiguredBytes(t *testing.T) {
	const limit = 128
	sink := &captureSink{}
	observer := &postgresObserver{
		sink:                  sink,
		startupDone:           true,
		maxClientMessageBytes: limit,
	}
	sql := "select " + strings.Repeat("a", 90)
	request := postgresMessage('Q', append([]byte(sql), 0))

	if forward, decision := observer.ObserveClientRelayBytes(request); decision != nil || len(forward) != len(request) {
		t.Fatalf("first query relay = (%d bytes, %#v), want accepted", len(forward), decision)
	}
	forward, decision := observer.ObserveClientRelayBytes(request)
	if len(forward) != 0 || decision == nil || decision.ErrorCode != observerErrorPendingLimit {
		t.Fatalf("second query relay = (%d bytes, %#v), want pending byte limit", len(forward), decision)
	}
}

func TestMySQLObserverCapsPreparedStatementAuditText(t *testing.T) {
	const limit = 128
	sink := &captureSink{}
	observer := &mysqlObserver{
		sink:                  sink,
		maxClientMessageBytes: limit,
	}
	sql := "select " + strings.Repeat("a", 90)
	establishMySQLPreparedStatement(t, observer, 1, sql, false)

	prepare := buildMySQLPacket(0, append([]byte{mysqlCommandStmtPrepare}, []byte(sql)...))
	if forward, decision := observer.ObserveClientRelayBytes(prepare); decision != nil || len(forward) != len(prepare) {
		t.Fatalf("second prepare request relay = (%d bytes, %#v), want accepted pending request", len(forward), decision)
	}
	response := buildMySQLPacket(1, mysqlPrepareOKPayload(2))
	forward, decision := observer.ObserveServerRelayBytes(response)
	if len(forward) != 0 || decision == nil || decision.Allowed {
		t.Fatalf("second prepare response relay = (%d bytes, %#v), want prepared-state rejection", len(forward), decision)
	}
	if len(sink.finished) != 2 ||
		sink.finished[0].Status != queryStatusSuccess ||
		sink.finished[1].Status != queryStatusError {
		t.Fatalf("prepare audit finishes = %#v, want one success and one rejection", sink.finished)
	}
}

func TestPostgresObserverCapsPreparedStatementAuditText(t *testing.T) {
	const limit = 128
	observer := &postgresObserver{
		sink:                  &captureSink{},
		startupDone:           true,
		maxClientMessageBytes: limit,
	}
	sql := "select " + strings.Repeat("a", 80)
	first := postgresMessage('P', append(append([]byte("first\x00"), []byte(sql)...), 0, 0, 0))
	if forward, decision := observer.ObserveClientRelayBytes(first); decision != nil || len(forward) != len(first) {
		t.Fatalf("first Parse relay = (%d bytes, %#v), want accepted", len(forward), decision)
	}

	second := postgresMessage('P', append(append([]byte("second\x00"), []byte(sql)...), 0, 0, 0))
	forward, decision := observer.ObserveClientRelayBytes(second)
	if len(forward) != 0 || decision == nil || decision.ErrorCode != observerErrorPendingLimit {
		t.Fatalf("second Parse relay = (%d bytes, %#v), want prepared-state byte limit", len(forward), decision)
	}
}

func TestLargeSQLProductionRecorderUsesBoundedAuditMetadata(t *testing.T) {
	const limit = 512 * 1024
	prefix := "select "
	sql := prefix + strings.Repeat("1,", (limit-5-len(prefix))/2)
	request := buildMySQLPacket(0, append([]byte{0x03}, []byte(sql)...))
	if len(request) > limit {
		t.Fatalf("test request = %d bytes, want at most %d", len(request), limit)
	}

	file, err := os.Create(filepath.Join(t.TempDir(), "queries.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	auditWriter := &queryCaptureAudit{}
	recorder := &connectionRecorder{
		ctx:                   context.Background(),
		id:                    "large-sql-connection",
		protocol:              "mysql",
		maxClientMessageBytes: limit,
		file:                  file,
		startedAt:             time.Now(),
		audit:                 auditWriter,
		auditSessionID:        "large-sql-session",
	}
	observer := newQueryObserverWithLimit("mysql", recorder, limit)
	forward, decision := observer.(relayClientQueryObserver).ObserveClientRelayBytes(request)
	if decision != nil || len(forward) != len(request) {
		t.Fatalf("large SQL request relay = (%d bytes, %#v), want accepted", len(forward), decision)
	}
	response := buildMySQLPacket(1, []byte{0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00})
	if _, decision = observer.(relayServerQueryObserver).ObserveServerRelayBytes(response); decision != nil {
		t.Fatalf("large SQL response decision = %#v", decision)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	queries := auditWriter.snapshot()
	if len(queries) != 1 {
		t.Fatalf("database audit rows = %d, want 1", len(queries))
	}
	query := queries[0]
	if len(query.SQLText) > limit ||
		query.OriginalSQLBytes != int64(len(sql)) ||
		!query.SQLTruncated {
		t.Fatalf("bounded production audit metadata = %#v, SQL bytes = %d", query, len(query.SQLText))
	}
}
