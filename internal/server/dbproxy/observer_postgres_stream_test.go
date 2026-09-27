package dbproxy

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestPostgresObserverStreamsLargeFramesOneByteAtATime(t *testing.T) {
	largeSQL := "SELECT '" + strings.Repeat("x", maxPostgresObserverBufferBytes) + "'"
	parsePayload := append([]byte("statement\x00"), []byte(largeSQL)...)
	parsePayload = append(parsePayload, 0, 0, 0)
	largeBind := postgresBindOneTextParameter(
		"portal",
		"statement",
		bytes.Repeat([]byte{'x'}, maxPostgresObserverBufferBytes),
	)
	tests := []struct {
		name    string
		request []byte
	}{
		{name: "Query", request: postgresMessage('Q', append([]byte(largeSQL), 0))},
		{name: "Parse", request: postgresMessage('P', parsePayload)},
		{name: "Bind", request: largeBind},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observer := &postgresObserver{
				startupDone: true, maxClientMessageBytes: 2 * 1024 * 1024,
			}
			forward, decision := feedObserverChunks(
				observer.ObserveClientRelayBytes,
				test.request,
				1,
			)
			if decision != nil || !bytes.Equal(forward, test.request) {
				t.Fatalf(
					"one-byte %s relay = (%d bytes, %#v), want %d bytes",
					test.name,
					len(forward),
					decision,
					len(test.request),
				)
			}
		})
	}
}

func TestPostgresObserverStreamsLargeQueryWithBoundedAuditSummary(t *testing.T) {
	sink := &captureSink{}
	observer := &postgresObserver{
		sink: sink, startupDone: true, maxClientMessageBytes: 2 * 1024 * 1024,
	}
	secret := strings.Repeat("streamed-secret-", 32*1024)
	sql := "INSERT INTO audit_probe(payload) VALUES ('" + secret + "')"
	request := postgresMessage('Q', append([]byte(sql), 0))

	forward, decision := feedObserverChunks(observer.ObserveClientRelayBytes, request, 7919)
	if decision != nil {
		t.Fatalf("large Query decision = %#v", decision)
	}
	if !bytes.Equal(forward, request) {
		t.Fatalf("large Query forwarded %d bytes, want %d", len(forward), len(request))
	}
	assertStreamedPostgresAudit(t, sink, sql, secret)
}

func TestPostgresObserverStreamsLargeParseAndRetainsPreparedSummary(t *testing.T) {
	sink := &captureSink{}
	observer := &postgresObserver{
		sink: sink, startupDone: true, maxClientMessageBytes: 2 * 1024 * 1024,
	}
	secret := strings.Repeat("prepared-secret-", 32*1024)
	sql := "INSERT INTO audit_probe(payload) VALUES ('" + secret + "')"
	parsePayload := append([]byte("large_statement\x00"), []byte(sql)...)
	parsePayload = append(parsePayload, 0, 0, 0)
	parse := postgresMessage('P', parsePayload)

	forward, decision := feedObserverChunks(observer.ObserveClientRelayBytes, parse, 4093)
	if decision != nil || !bytes.Equal(forward, parse) {
		t.Fatalf("large Parse relay = (%d bytes, %#v), want %d bytes", len(forward), decision, len(parse))
	}
	bind := postgresBindNoParameters("large_portal", "large_statement")
	execute := postgresMessage('E', []byte("large_portal\x00\x00\x00\x00\x00"))
	for _, message := range [][]byte{bind, execute, postgresMessage('S', nil)} {
		if _, decision := observer.ObserveClientRelayBytes(message); decision != nil {
			t.Fatalf("prepared execution decision = %#v", decision)
		}
	}
	assertStreamedPostgresAudit(t, sink, sql, secret)
}

func TestPostgresObserverStreamsLargeBindWithoutAuditingParameterValue(t *testing.T) {
	sink := &captureSink{}
	observer := &postgresObserver{
		sink: sink, startupDone: true, maxClientMessageBytes: 2 * 1024 * 1024,
	}
	parse := postgresMessage(
		'P',
		append([]byte("bind_statement\x00INSERT INTO audit_probe(payload) VALUES ($1)\x00"), 0, 1, 0, 0, 0, 25),
	)
	if _, decision := observer.ObserveClientRelayBytes(parse); decision != nil {
		t.Fatalf("Bind test Parse decision = %#v", decision)
	}

	parameter := bytes.Repeat([]byte("parameter-secret-"), 32*1024)
	bind := postgresBindOneTextParameter("bind_portal", "bind_statement", parameter)
	forward, decision := feedObserverChunks(observer.ObserveClientRelayBytes, bind, 6151)
	if decision != nil || !bytes.Equal(forward, bind) {
		t.Fatalf("large Bind relay = (%d bytes, %#v), want %d bytes", len(forward), decision, len(bind))
	}
	execute := postgresMessage('E', []byte("bind_portal\x00\x00\x00\x00\x00"))
	if _, decision := observer.ObserveClientRelayBytes(execute); decision != nil {
		t.Fatalf("large Bind Execute decision = %#v", decision)
	}
	if len(sink.queries) != 1 || strings.Contains(sink.queries[0], "parameter-secret") {
		t.Fatalf("large Bind audit = %#v, want SQL only", sink.queries)
	}
}

func TestPostgresStreamAuditFallsBackWhenGlobalBudgetIsBusy(t *testing.T) {
	budget := newObserverMemoryBudget(postgresStreamAuditReservationBytes)
	held := budget.tryAcquire(postgresStreamAuditReservationBytes)
	if held == nil {
		t.Fatal("reserve test observer budget")
	}
	defer held.release()

	sink := &captureSink{}
	observer := &postgresObserver{
		sink:                  sink,
		startupDone:           true,
		maxClientMessageBytes: 2 * 1024 * 1024,
		memoryBudget:          budget,
	}
	secret := strings.Repeat("budget-secret-", 32*1024)
	sql := "SELECT '" + secret + "'"
	request := postgresMessage('Q', append([]byte(sql), 0))

	forward, decision := feedObserverChunks(observer.ObserveClientRelayBytes, request, 8191)
	if decision != nil || !bytes.Equal(forward, request) {
		t.Fatalf("budget fallback relay = (%d bytes, %#v), want %d bytes", len(forward), decision, len(request))
	}
	if budget.inUse() != postgresStreamAuditReservationBytes {
		t.Fatalf("observer budget in use = %d, want only held reservation", budget.inUse())
	}
	if len(sink.queries) != 1 || !strings.Contains(sink.queries[0], "preview omitted") {
		t.Fatalf("budget fallback audit = %#v", sink.queries)
	}
	if strings.Contains(sink.queries[0], "budget-secret") {
		t.Fatalf("budget fallback exposed SQL data: %q", sink.queries[0])
	}
}

func TestPostgresStreamReleasesGlobalBudgetOnAbort(t *testing.T) {
	budget := newObserverMemoryBudget(postgresStreamAuditReservationBytes)
	observer := &postgresObserver{
		startupDone:           true,
		maxClientMessageBytes: 2 * 1024 * 1024,
		memoryBudget:          budget,
	}
	request := postgresMessage('Q', append(bytes.Repeat([]byte{'x'}, 512*1024), 0))

	if _, decision := observer.ObserveClientRelayBytes(request[:32*1024]); decision != nil {
		t.Fatalf("partial stream decision = %#v", decision)
	}
	if budget.inUse() != postgresStreamAuditReservationBytes {
		t.Fatalf("partial stream budget = %d", budget.inUse())
	}
	observer.CloseObserver()
	if budget.inUse() != 0 {
		t.Fatalf("released stream budget = %d, want 0", budget.inUse())
	}
}

func TestPostgresLimitErrorUsesProgramLimitSQLState(t *testing.T) {
	observer := &postgresObserver{startupDone: true, maxClientMessageBytes: 64}
	request := buildPostgresQueryWithFrameSize(t, 65)
	_, decision := feedObserverChunks(observer.ObserveClientRelayBytes, request, 7)
	if decision == nil || decision.ErrorCode != observerErrorClientMessageLimit {
		t.Fatalf("oversized Query decision = %#v", decision)
	}
	response := observer.ErrorResponse(*decision)
	fields := postgresErrorResponseFields(t, response)
	if fields['S'] != "ERROR" || fields['C'] != "54000" {
		t.Fatalf("PostgreSQL limit error fields = %#v", fields)
	}
	if fields['M'] != "database proxy client message exceeds the configured limit" {
		t.Fatalf("PostgreSQL limit message = %q", fields['M'])
	}
	if !strings.Contains(fields['H'], "COPY") {
		t.Fatalf("PostgreSQL limit hint = %q", fields['H'])
	}
}

func assertStreamedPostgresAudit(t *testing.T, sink *captureSink, sql, secret string) {
	t.Helper()
	if len(sink.queries) != 1 || len(sink.details) != 1 {
		t.Fatalf("streamed audit entries = %d/%d, want 1/1", len(sink.queries), len(sink.details))
	}
	if len(sink.queries[0]) > postgresStreamAuditPreviewBytes {
		t.Fatalf("streamed audit summary = %d bytes", len(sink.queries[0]))
	}
	secretMarker := secret
	if len(secretMarker) > 32 {
		secretMarker = secretMarker[:32]
	}
	if strings.Contains(sink.queries[0], secretMarker) || !strings.Contains(sink.queries[0], sqlAuditRedacted) {
		t.Fatalf("streamed audit summary was not safely redacted: %q", sink.queries[0])
	}
	if got, ok := sink.details[0]["sql_original_bytes"].(int64); !ok || got != int64(len(sql)) {
		t.Fatalf("streamed original SQL bytes = %#v, want %d", sink.details[0]["sql_original_bytes"], len(sql))
	}
	if truncated, ok := sink.details[0]["sql_truncated"].(bool); !ok || !truncated {
		t.Fatalf("streamed SQL truncated = %#v, want true", sink.details[0]["sql_truncated"])
	}
}

func postgresBindOneTextParameter(portal, statement string, parameter []byte) []byte {
	payload := append([]byte(portal), 0)
	payload = append(payload, []byte(statement)...)
	payload = append(payload, 0)
	payload = append(payload, 0, 1, 0, 0)
	payload = append(payload, 0, 1)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(parameter)))
	payload = append(payload, length[:]...)
	payload = append(payload, parameter...)
	payload = append(payload, 0, 0)
	return postgresMessage('B', payload)
}

func postgresErrorResponseFields(t *testing.T, response []byte) map[byte]string {
	t.Helper()
	if len(response) < 6 || response[0] != 'E' {
		t.Fatalf("invalid PostgreSQL ErrorResponse: %x", response)
	}
	length := int(binary.BigEndian.Uint32(response[1:5]))
	if length < 5 || 1+length > len(response) {
		t.Fatalf("invalid PostgreSQL ErrorResponse length %d/%d", length, len(response))
	}
	payload := response[5 : 1+length]
	fields := make(map[byte]string)
	for len(payload) > 0 && payload[0] != 0 {
		kind := payload[0]
		payload = payload[1:]
		index := bytes.IndexByte(payload, 0)
		if index < 0 {
			t.Fatalf("unterminated PostgreSQL error field %q", kind)
		}
		fields[kind] = string(payload[:index])
		payload = payload[index+1:]
	}
	return fields
}
