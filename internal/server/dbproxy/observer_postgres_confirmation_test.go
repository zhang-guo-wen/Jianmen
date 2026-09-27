package dbproxy

import "testing"

func TestPostgresPreparedStateCommitsOnlyAfterServerConfirmation(t *testing.T) {
	observer := &postgresObserver{sink: &captureSink{}, startupDone: true}
	parse := postgresParseMessage("statement", "select from_confirmed_state")

	if decision := observer.ObserveClientBytes(parse); decision != nil {
		t.Fatalf("Parse denied: %#v", decision)
	}
	if _, exists := observer.preparedStatements["statement"]; exists {
		t.Fatal("Parse changed committed state before ParseComplete")
	}
	if decision := observer.ObserveServerBytes(postgresMessage('1', nil)); decision != nil {
		t.Fatalf("ParseComplete denied: %#v", decision)
	}
	if _, exists := observer.preparedStatements["statement"]; !exists {
		t.Fatal("ParseComplete did not commit the prepared statement")
	}

	bind := postgresBindNoParameters("portal", "statement")
	if decision := observer.ObserveClientBytes(bind); decision != nil {
		t.Fatalf("Bind denied: %#v", decision)
	}
	if _, exists := observer.portals["portal"]; exists {
		t.Fatal("Bind changed committed state before BindComplete")
	}
	if decision := observer.ObserveServerBytes(postgresMessage('2', nil)); decision != nil {
		t.Fatalf("BindComplete denied: %#v", decision)
	}
	if _, exists := observer.portals["portal"]; !exists {
		t.Fatal("BindComplete did not commit the portal")
	}

	closePortal := postgresMessage('C', []byte("Pportal\x00"))
	if decision := observer.ObserveClientBytes(closePortal); decision != nil {
		t.Fatalf("Close denied: %#v", decision)
	}
	if _, exists := observer.portals["portal"]; !exists {
		t.Fatal("Close removed committed portal before CloseComplete")
	}
	if decision := observer.ObserveServerBytes(postgresMessage('3', nil)); decision != nil {
		t.Fatalf("CloseComplete denied: %#v", decision)
	}
	if _, exists := observer.portals["portal"]; exists {
		t.Fatal("CloseComplete did not remove the committed portal")
	}
}

func TestPostgresRejectedDuplicateParseCannotMaskExecutedSQL(t *testing.T) {
	sink := &captureSink{}
	observer := &postgresObserver{sink: sink, startupDone: true}
	const (
		statement    = "reused_statement"
		dangerousSQL = "delete from protected_records"
		benignSQL    = "select harmless_column from harmless_table"
	)

	confirmPostgresParse(t, observer, statement, dangerousSQL)

	attack := append(
		postgresParseMessage(statement, benignSQL),
		postgresMessage('S', nil)...,
	)
	attack = append(
		attack,
		postgresBindNoParameters("portal", statement)...,
	)
	attack = append(attack, postgresMessage('E', []byte("portal\x00\x00\x00\x00\x00"))...)
	attack = append(attack, postgresMessage('S', nil)...)
	forward, decision := observer.ObserveClientRelayBytes(attack)
	if len(forward) != 0 ||
		decision == nil ||
		decision.ErrorCode != observerErrorProtocol {
		t.Fatalf(
			"duplicate Parse attack relay = (%d bytes, %#v), want fail-closed rejection",
			len(forward),
			decision,
		)
	}
	if len(sink.queries) != 0 {
		t.Fatalf("duplicate Parse attack created audit queries: %#v", sink.queries)
	}
}

func TestPostgresServerErrorsRollBackParseBindAndCloseCandidates(t *testing.T) {
	t.Run("Parse", func(t *testing.T) {
		observer := &postgresObserver{sink: &captureSink{}, startupDone: true}
		request := append(
			postgresParseMessage("candidate", "select candidate_value"),
			postgresMessage('S', nil)...,
		)
		if decision := observer.ObserveClientBytes(request); decision != nil {
			t.Fatalf("Parse candidate denied: %#v", decision)
		}
		if _, exists := observer.preparedStatements["candidate"]; exists {
			t.Fatal("Parse candidate committed before ParseComplete")
		}
		response := append(
			postgresTestErrorResponse("42601"),
			postgresMessage('Z', []byte{'I'})...,
		)
		if decision := observer.ObserveServerBytes(response); decision != nil {
			t.Fatalf("Parse rejection denied: %#v", decision)
		}
		if _, exists := observer.preparedStatements["candidate"]; exists {
			t.Fatal("rejected Parse candidate reached committed state")
		}
	})

	t.Run("Bind and Close", func(t *testing.T) {
		observer := &postgresObserver{sink: &captureSink{}, startupDone: true}
		confirmPostgresParse(t, observer, "statement", "select confirmed_value")

		bind := append(
			postgresBindNoParameters("candidate_portal", "statement"),
			postgresMessage('S', nil)...,
		)
		if decision := observer.ObserveClientBytes(bind); decision != nil {
			t.Fatalf("Bind candidate denied: %#v", decision)
		}
		if _, exists := observer.portals["candidate_portal"]; exists {
			t.Fatal("Bind candidate committed before BindComplete")
		}
		response := append(
			postgresTestErrorResponse("22023"),
			postgresMessage('Z', []byte{'I'})...,
		)
		if decision := observer.ObserveServerBytes(response); decision != nil {
			t.Fatalf("Bind rejection denied: %#v", decision)
		}
		if _, exists := observer.portals["candidate_portal"]; exists {
			t.Fatal("rejected Bind candidate reached committed state")
		}

		confirmPostgresBind(t, observer, "confirmed_portal", "statement")
		closeRequest := append(
			postgresMessage('C', []byte("Pconfirmed_portal\x00")),
			postgresMessage('S', nil)...,
		)
		if decision := observer.ObserveClientBytes(closeRequest); decision != nil {
			t.Fatalf("Close candidate denied: %#v", decision)
		}
		if _, exists := observer.portals["confirmed_portal"]; !exists {
			t.Fatal("Close candidate removed portal before CloseComplete")
		}
		closeResponse := append(
			postgresTestErrorResponse("34000"),
			postgresMessage('Z', []byte{'T'})...,
		)
		if decision := observer.ObserveServerBytes(closeResponse); decision != nil {
			t.Fatalf("Close rejection denied: %#v", decision)
		}
		if _, exists := observer.portals["confirmed_portal"]; !exists {
			t.Fatal("rejected Close candidate removed committed portal")
		}
	})
}

func TestPostgresDuplicateNamedBindCannotMaskExecutedSQL(t *testing.T) {
	sink := &captureSink{}
	observer := &postgresObserver{sink: sink, startupDone: true}
	const (
		dangerousStatement = "dangerous_statement"
		benignStatement    = "benign_statement"
		dangerousSQL       = "delete from protected_records"
		benignSQL          = "select harmless_column from harmless_table"
	)

	confirmPostgresParse(t, observer, dangerousStatement, dangerousSQL)
	confirmPostgresParse(t, observer, benignStatement, benignSQL)
	confirmPostgresBind(t, observer, "reused_portal", dangerousStatement)

	attack := append(
		postgresBindNoParameters("reused_portal", benignStatement),
		postgresMessage('S', nil)...,
	)
	attack = append(
		attack,
		postgresMessage('E', []byte("reused_portal\x00\x00\x00\x00\x00"))...,
	)
	attack = append(attack, postgresMessage('S', nil)...)
	forward, decision := observer.ObserveClientRelayBytes(attack)
	if len(forward) != 0 ||
		decision == nil ||
		decision.ErrorCode != observerErrorProtocol {
		t.Fatalf(
			"duplicate Bind attack relay = (%d bytes, %#v), want fail-closed rejection",
			len(forward),
			decision,
		)
	}
	if len(sink.queries) != 0 {
		t.Fatalf("duplicate Bind attack created audit queries: %#v", sink.queries)
	}
}

func confirmPostgresParse(
	t *testing.T,
	observer *postgresObserver,
	name string,
	sql string,
) {
	t.Helper()
	if decision := observer.ObserveClientBytes(postgresParseMessage(name, sql)); decision != nil {
		t.Fatalf("Parse %q denied: %#v", name, decision)
	}
	if decision := observer.ObserveServerBytes(postgresMessage('1', nil)); decision != nil {
		t.Fatalf("ParseComplete %q denied: %#v", name, decision)
	}
}

func confirmPostgresBind(
	t *testing.T,
	observer *postgresObserver,
	portal string,
	statement string,
) {
	t.Helper()
	if decision := observer.ObserveClientBytes(postgresBindNoParameters(portal, statement)); decision != nil {
		t.Fatalf("Bind %q denied: %#v", portal, decision)
	}
	if decision := observer.ObserveServerBytes(postgresMessage('2', nil)); decision != nil {
		t.Fatalf("BindComplete %q denied: %#v", portal, decision)
	}
}

func postgresParseMessage(name string, sql string) []byte {
	payload := append([]byte(name), 0)
	payload = append(payload, []byte(sql)...)
	payload = append(payload, 0, 0, 0)
	return postgresMessage('P', payload)
}

func postgresTestErrorResponse(code string) []byte {
	payload := []byte{'S'}
	payload = append(payload, []byte("ERROR")...)
	payload = append(payload, 0, 'C')
	payload = append(payload, []byte(code)...)
	payload = append(payload, 0, 'M')
	payload = append(payload, []byte("upstream rejected protocol operation")...)
	payload = append(payload, 0, 0)
	return postgresMessage('E', payload)
}
