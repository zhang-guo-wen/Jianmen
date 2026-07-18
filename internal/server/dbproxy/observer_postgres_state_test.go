package dbproxy

import "testing"

func TestPostgresObserverTracksParseBindExecuteThroughReadyForQuery(t *testing.T) {
	sink := &captureSink{}
	observer := &postgresObserver{sink: sink, startupDone: true}
	parse := postgresMessage('P', append(append([]byte("statement\x00"), []byte("select $1\x00")...), 0, 0))

	if decision := observer.ObserveClientBytes(parse); decision != nil {
		t.Fatalf("parse decision = %#v", decision)
	}
	if len(sink.queries) != 0 {
		t.Fatalf("Parse started audit queries: %v", sink.queries)
	}
	observer.ObserveServerBytes(postgresMessage('1', nil))
	if len(sink.finished) != 0 {
		t.Fatalf("ParseComplete finished an execution: %#v", sink.finished)
	}

	bind := postgresBindNoParameters("portal", "statement")
	execute := postgresMessage('E', append([]byte("portal\x00"), 0, 0, 0, 0))
	syncMessage := postgresMessage('S', nil)
	if decision := observer.ObserveClientBytes(append(append(bind, execute...), syncMessage...)); decision != nil {
		t.Fatalf("bind/execute/sync decision = %#v", decision)
	}
	if len(sink.queries) != 1 || sink.queries[0] != "select $1" {
		t.Fatalf("executed queries = %v, want prepared SQL at Execute", sink.queries)
	}

	observer.ObserveServerBytes(postgresMessage('2', nil))
	observer.ObserveServerBytes(postgresMessage('C', []byte("SELECT 1\x00")))
	if len(sink.finished) != 0 {
		t.Fatalf("extended query finished before ReadyForQuery: %#v", sink.finished)
	}
	observer.ObserveServerBytes(postgresMessage('Z', []byte{'I'}))
	if len(sink.finished) != 1 || sink.finished[0].Status != queryStatusSuccess {
		t.Fatalf("extended query finish = %#v, want one success", sink.finished)
	}
}

func postgresBindNoParameters(portal, statement string) []byte {
	payload := append([]byte(portal), 0)
	payload = append(payload, []byte(statement)...)
	payload = append(payload, 0)
	payload = append(payload,
		0, 0, // parameter format-code count
		0, 0, // parameter value count
		0, 0, // result format-code count
	)
	return postgresMessage('B', payload)
}

func TestPostgresObserverWaitsForSimpleQueryReadyBoundaryAcrossMultipleStatements(t *testing.T) {
	sink := &captureSink{}
	observer := &postgresObserver{sink: sink, startupDone: true}
	observer.ObserveClientBytes(postgresMessage('Q', []byte("update first; update second\x00")))

	observer.ObserveServerBytes(postgresMessage('C', []byte("UPDATE 1\x00")))
	observer.ObserveServerBytes(postgresMessage('C', []byte("UPDATE 2\x00")))
	if len(sink.finished) != 0 {
		t.Fatalf("simple query finished before ReadyForQuery: %#v", sink.finished)
	}
	observer.ObserveServerBytes(postgresMessage('Z', []byte{'I'}))

	if len(sink.finished) != 1 || sink.finished[0].Status != queryStatusSuccess {
		t.Fatalf("simple query finish = %#v, want one success", sink.finished)
	}
	if sink.finished[0].RowsAffected == nil || *sink.finished[0].RowsAffected != 3 {
		t.Fatalf("aggregated rows affected = %v, want 3", sink.finished[0].RowsAffected)
	}
}

func TestPostgresObserverKeepsPipelinedSimpleQueryErrorsPaired(t *testing.T) {
	sink := &captureSink{}
	observer := &postgresObserver{sink: sink, startupDone: true}
	first := postgresMessage('Q', []byte("select 1\x00"))
	second := postgresMessage('Q', []byte("select secret_from_missing_table\x00"))
	observer.ObserveClientBytes(append(first, second...))

	firstResponse := append(postgresMessage('C', []byte("SELECT 1\x00")), postgresMessage('Z', []byte{'I'})...)
	errorPayload := []byte{'S', 'E', 'R', 'R', 'O', 'R', 0, 'C', '4', '2', 'P', '0', '1', 0, 'M'}
	errorPayload = append(errorPayload, []byte("server echoed secret-value")...)
	errorPayload = append(errorPayload, 0, 0)
	secondResponse := append(postgresMessage('E', errorPayload), postgresMessage('Z', []byte{'I'})...)
	observer.ObserveServerBytes(append(firstResponse, secondResponse...))

	if len(sink.finished) != 2 {
		t.Fatalf("pipelined finishes = %#v, want two", sink.finished)
	}
	if sink.finished[0].Status != queryStatusSuccess || sink.finished[1].Status != queryStatusError {
		t.Fatalf("pipeline statuses = %#v, want success then error", sink.finished)
	}
	if sink.finished[1].ErrorCode != "42P01" {
		t.Fatalf("second error code = %q, want 42P01", sink.finished[1].ErrorCode)
	}
	if sink.finished[1].ErrorMessage != "postgres upstream error" {
		t.Fatalf("second error message = %q, want fixed sanitized message", sink.finished[1].ErrorMessage)
	}
}

func TestPostgresObserverAllowsEmptyPreparedStatement(t *testing.T) {
	sink := &captureSink{}
	observer := &postgresObserver{sink: sink, startupDone: true}
	request := append(
		postgresMessage('P', []byte("empty\x00\x00\x00\x00")),
		postgresBindNoParameters("portal", "empty")...,
	)
	request = append(request, postgresMessage('E', []byte("portal\x00\x00\x00\x00\x00"))...)
	request = append(request, postgresMessage('S', nil)...)

	if decision := observer.ObserveClientBytes(request); decision != nil {
		t.Fatalf("empty prepared statement denied: %#v", decision)
	}
	if len(sink.queries) != 1 || sink.queries[0] != "" {
		t.Fatalf("executed audit entries = %#v, want one empty statement", sink.queries)
	}

	response := append(postgresMessage('I', nil), postgresMessage('Z', []byte{'I'})...)
	if decision := observer.ObserveServerBytes(response); decision != nil {
		t.Fatalf("empty prepared response denied: %#v", decision)
	}
	if len(sink.finished) != 1 || sink.finished[0].Status != queryStatusSuccess {
		t.Fatalf("empty prepared finish = %#v, want success", sink.finished)
	}
}
