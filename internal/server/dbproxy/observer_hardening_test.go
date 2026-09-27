package dbproxy

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestMySQLRelayRejectsUnsupportedCommandsBeforeUpstream(t *testing.T) {
	tests := []struct {
		name    string
		command byte
	}{
		{name: "change user", command: 0x11},
		{name: "reset connection", command: 0x1f},
		{name: "binlog dump", command: 0x12},
		{name: "register slave", command: 0x15},
		{name: "shutdown", command: 0x08},
		{name: "process kill", command: 0x0c},
		{name: "debug", command: 0x0d},
		{name: "unknown", command: 0x7f},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer := newQueryObserver("mysql", &captureSink{})
			relay := observer.(relayClientQueryObserver)
			packet := buildMySQLPacket(0, []byte{tt.command})

			forward, decision := relay.ObserveClientRelayBytes(packet)

			if len(forward) != 0 {
				t.Fatalf("unsupported command forwarded upstream: %x", forward)
			}
			requireObserverDenied(t, decision)
		})
	}
}

func TestMySQLRelayAllowsRequiredCommandLifecycle(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{name: "quit", payload: []byte{0x01}},
		{name: "init db", payload: append([]byte{0x02}, []byte("app")...)},
		{name: "query", payload: append([]byte{0x03}, []byte("select 1")...)},
		{name: "ping", payload: []byte{0x0e}},
		{name: "prepare", payload: append([]byte{0x16}, []byte("select ?")...)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer := newQueryObserver("mysql", &captureSink{})
			relay := observer.(relayClientQueryObserver)
			packet := buildMySQLPacket(0, tt.payload)

			forward, decision := relay.ObserveClientRelayBytes(packet)

			if decision != nil {
				t.Fatalf("required command denied: %#v", decision)
			}
			if !bytes.Equal(forward, packet) {
				t.Fatalf("forwarded bytes = %x, want %x", forward, packet)
			}
		})
	}
}

func TestMySQLRelayForwardsOnlyCompleteValidatedFrames(t *testing.T) {
	observer := newQueryObserver("mysql", &captureSink{})
	relay := observer.(relayClientQueryObserver)
	query := buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))

	forward, decision := relay.ObserveClientRelayBytes(query[:3])
	if decision != nil || len(forward) != 0 {
		t.Fatalf("partial frame forwarded=%x decision=%#v", forward, decision)
	}

	forward, decision = relay.ObserveClientRelayBytes(query[3:])
	if decision != nil || !bytes.Equal(forward, query) {
		t.Fatalf("completed frame forwarded=%x decision=%#v", forward, decision)
	}
}

func TestMySQLRelayRejectsNonzeroClientCommandSequence(t *testing.T) {
	observer := newQueryObserver("mysql", &captureSink{})
	relay := observer.(relayClientQueryObserver)
	packet := buildMySQLPacket(7, []byte{0x0e})

	forward, decision := relay.ObserveClientRelayBytes(packet)

	if len(forward) != 0 {
		t.Fatalf("nonzero-sequence command forwarded: %x", forward)
	}
	requireObserverDenied(t, decision)
	response := observer.ErrorResponse(*decision)
	if len(response) < 4 || response[3] != 1 {
		t.Fatalf("terminal response sequence = %x, want protocol response sequence 1", response)
	}
}

func TestMySQLRelayRejectsUnexpectedServerSequence(t *testing.T) {
	t.Run("first response", func(t *testing.T) {
		sink := &captureSink{}
		observer := newQueryObserver("mysql", sink)
		clientRelay := observer.(relayClientQueryObserver)
		serverRelay := observer.(relayServerQueryObserver)
		query := buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))
		if _, decision := clientRelay.ObserveClientRelayBytes(query); decision != nil {
			t.Fatalf("query denied: %#v", decision)
		}

		forward, decision := serverRelay.ObserveServerRelayBytes(
			buildMySQLPacket(99, []byte{0x00, 0, 0, 0, 0, 0, 0}),
		)

		if len(forward) != 0 {
			t.Fatalf("unexpected first response forwarded: %x", forward)
		}
		requireObserverDenied(t, decision)
	})

	t.Run("continuation", func(t *testing.T) {
		sink := &captureSink{}
		observer := newQueryObserver("mysql", sink)
		clientRelay := observer.(relayClientQueryObserver)
		serverRelay := observer.(relayServerQueryObserver)
		query := buildMySQLPacket(0, append([]byte{0x03}, []byte("select value")...))
		if _, decision := clientRelay.ObserveClientRelayBytes(query); decision != nil {
			t.Fatalf("query denied: %#v", decision)
		}
		columnCount := buildMySQLPacket(1, []byte{0x01})
		wrongColumn := buildMySQLPacket(9, []byte("column-definition"))

		forward, decision := serverRelay.ObserveServerRelayBytes(append(columnCount, wrongColumn...))

		if !bytes.Equal(forward, columnCount) {
			t.Fatalf("safe response prefix = %x, want %x", forward, columnCount)
		}
		requireObserverDenied(t, decision)
	})
}

func TestMySQLRelayPreservesSafePrefixBeforeFatalSuffix(t *testing.T) {
	observer := newQueryObserver("mysql", &captureSink{})
	relay := observer.(relayClientQueryObserver)
	query := buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))
	changeUser := buildMySQLPacket(0, []byte{0x11})

	forward, decision := relay.ObserveClientRelayBytes(append(query, changeUser...))

	if !bytes.Equal(forward, query) {
		t.Fatalf("safe prefix = %x, want %x", forward, query)
	}
	requireObserverDenied(t, decision)
}

func TestPostgresRelayForwardsOnlyCompleteValidatedFrames(t *testing.T) {
	observer := newQueryObserver("postgres", &captureSink{})
	relay := observer.(relayClientQueryObserver)
	query := postgresMessage('Q', append([]byte("select 1"), 0))

	forward, decision := relay.ObserveClientRelayBytes(query[:4])
	if decision != nil || len(forward) != 0 {
		t.Fatalf("partial frame forwarded=%x decision=%#v", forward, decision)
	}

	forward, decision = relay.ObserveClientRelayBytes(query[4:])
	if decision != nil || !bytes.Equal(forward, query) {
		t.Fatalf("completed frame forwarded=%x decision=%#v", forward, decision)
	}
}

func TestPostgresRelayPreservesSafePrefixBeforeFatalSuffix(t *testing.T) {
	observer := newQueryObserver("postgres", &captureSink{})
	relay := observer.(relayClientQueryObserver)
	query := postgresMessage('Q', append([]byte("select 1"), 0))
	malformed := []byte{'Q', 0, 0, 0, 3}

	forward, decision := relay.ObserveClientRelayBytes(append(query, malformed...))

	if !bytes.Equal(forward, query) {
		t.Fatalf("safe prefix = %x, want %x", forward, query)
	}
	requireObserverDenied(t, decision)
}

func TestServerRelaysBufferIncompleteMySQLAndPostgresFrames(t *testing.T) {
	t.Run("mysql", func(t *testing.T) {
		observer := newQueryObserver("mysql", &captureSink{})
		clientRelay := observer.(relayClientQueryObserver)
		serverRelay := observer.(relayServerQueryObserver)
		query := buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))
		if _, decision := clientRelay.ObserveClientRelayBytes(query); decision != nil {
			t.Fatalf("query denied: %#v", decision)
		}
		okPacket := buildMySQLPacket(1, []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})

		forward, decision := serverRelay.ObserveServerRelayBytes(okPacket[:2])
		if decision != nil || len(forward) != 0 {
			t.Fatalf("partial response forwarded=%x decision=%#v", forward, decision)
		}
		forward, decision = serverRelay.ObserveServerRelayBytes(okPacket[2:])
		if decision != nil || !bytes.Equal(forward, okPacket) {
			t.Fatalf("completed response forwarded=%x decision=%#v", forward, decision)
		}
	})

	t.Run("postgres", func(t *testing.T) {
		observer := newQueryObserver("postgres", &captureSink{})
		clientRelay := observer.(relayClientQueryObserver)
		serverRelay := observer.(relayServerQueryObserver)
		query := postgresMessage('Q', append([]byte("select 1"), 0))
		if _, decision := clientRelay.ObserveClientRelayBytes(query); decision != nil {
			t.Fatalf("query denied: %#v", decision)
		}
		complete := postgresMessage('C', append([]byte("SELECT 1"), 0))

		forward, decision := serverRelay.ObserveServerRelayBytes(complete[:3])
		if decision != nil || len(forward) != 0 {
			t.Fatalf("partial response forwarded=%x decision=%#v", forward, decision)
		}
		forward, decision = serverRelay.ObserveServerRelayBytes(complete[3:])
		if decision != nil || !bytes.Equal(forward, complete) {
			t.Fatalf("completed response forwarded=%x decision=%#v", forward, decision)
		}
	})
}

func TestMySQLErrorResponseUsesClientExpectedServerSequence(t *testing.T) {
	observer := &mysqlObserver{sink: &captureSink{}}
	query := buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))
	if decision := observer.ObserveClientBytes(query); decision != nil {
		t.Fatalf("query denied: %#v", decision)
	}
	malformedPayload := []byte{0xfc}
	decision := observer.ObserveServerBytes(buildMySQLPacket(7, malformedPayload))
	requireObserverDenied(t, decision)

	response := observer.ErrorResponse(*decision)
	if len(response) < 4 {
		t.Fatalf("short error response: %x", response)
	}
	if got := response[3]; got != 1 {
		t.Fatalf("error sequence = %d, want unforwarded response sequence 1", got)
	}
	payloadLength := int(response[0]) | int(response[1])<<8 | int(response[2])<<16
	if payloadLength != len(response)-4 {
		t.Fatalf("error payload length = %d, want %d", payloadLength, len(response)-4)
	}
}

func TestMySQLErrorResponseUsesBlockedPacketSequenceWhenExpectedPacketIsFatal(t *testing.T) {
	observer := &mysqlObserver{sink: &captureSink{}}
	query := buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))
	requireNoDecision(t, observer.ObserveClientBytes(query))

	decision := observer.ObserveServerBytes(buildMySQLPacket(1, []byte{0xfc, 0x01}))
	requireObserverDenied(t, decision)

	response := observer.ErrorResponse(*decision)
	if len(response) < 4 || response[3] != 1 {
		t.Fatalf("terminal sequence = %x, want blocked packet sequence 1", response)
	}
}

func TestMySQLErrorResponseIgnoresAttackerSequence255(t *testing.T) {
	observer := &mysqlObserver{sink: &captureSink{}}
	query := buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))
	requireNoDecision(t, observer.ObserveClientBytes(query))
	decision := observer.ObserveServerBytes(buildMySQLPacket(255, []byte{0xfc}))
	requireObserverDenied(t, decision)

	response := observer.ErrorResponse(*decision)
	if len(response) < 4 || response[3] != 1 {
		t.Fatalf("terminal sequence = %x, want expected sequence 1", response)
	}
}

func TestMySQLObserverKeepsPingResponseAheadOfPipelinedQuery(t *testing.T) {
	sink := &captureSink{}
	observer := &mysqlObserver{sink: sink}
	pipeline := append(
		buildMySQLPacket(0, []byte{0x0e}),
		buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))...,
	)
	if decision := observer.ObserveClientBytes(pipeline); decision != nil {
		t.Fatalf("pipeline denied: %#v", decision)
	}
	if len(sink.queries) != 1 {
		t.Fatalf("audited queries = %d, want only COM_QUERY", len(sink.queries))
	}

	pong := buildMySQLPacket(1, []byte{0x00, 0, 0, 0, 0, 0, 0})
	queryError := buildMySQLPacket(1, []byte{0xff, 0x28, 0x04})
	if decision := observer.ObserveServerBytes(append(pong, queryError...)); decision != nil {
		t.Fatalf("pipeline response denied: %#v", decision)
	}

	if len(sink.finished) != 1 {
		t.Fatalf("finished audits = %d, want 1", len(sink.finished))
	}
	if finish := sink.finished[0]; finish.Status != queryStatusError || finish.ErrorCode != "1064" {
		t.Fatalf("query finish = %#v, want query error rather than PING success", finish)
	}
}

func TestServerRelaysPreserveValidatedPrefixBeforeMalformedSuffix(t *testing.T) {
	t.Run("mysql", func(t *testing.T) {
		observer := newQueryObserver("mysql", &captureSink{})
		clientRelay := observer.(relayClientQueryObserver)
		serverRelay := observer.(relayServerQueryObserver)
		query := buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))
		if _, decision := clientRelay.ObserveClientRelayBytes(query); decision != nil {
			t.Fatalf("query denied: %#v", decision)
		}
		okPacket := buildMySQLPacket(1, []byte{0x00, 0, 0, 0, 0, 0, 0})
		oversizedHeader := []byte{0xff, 0xff, 0xff, 9}

		forward, decision := serverRelay.ObserveServerRelayBytes(append(okPacket, oversizedHeader...))

		if !bytes.Equal(forward, okPacket) {
			t.Fatalf("safe server prefix = %x, want %x", forward, okPacket)
		}
		requireObserverDenied(t, decision)
		response := observer.ErrorResponse(*decision)
		if len(response) < 4 || response[3] != 2 {
			t.Fatalf("terminal sequence = %x, want next sequence after server packet 1", response)
		}
	})

	t.Run("postgres", func(t *testing.T) {
		observer := newQueryObserver("postgres", &captureSink{})
		clientRelay := observer.(relayClientQueryObserver)
		serverRelay := observer.(relayServerQueryObserver)
		query := postgresMessage('Q', append([]byte("select 1"), 0))
		if _, decision := clientRelay.ObserveClientRelayBytes(query); decision != nil {
			t.Fatalf("query denied: %#v", decision)
		}
		commandComplete := postgresMessage('C', append([]byte("SELECT 1"), 0))
		malformed := []byte{'Z', 0, 0, 0, 3}

		forward, decision := serverRelay.ObserveServerRelayBytes(append(commandComplete, malformed...))

		if !bytes.Equal(forward, commandComplete) {
			t.Fatalf("safe server prefix = %x, want %x", forward, commandComplete)
		}
		requireObserverDenied(t, decision)
	})
}

func TestPostgresMalformedLengthEncodingIsFatalBeforeForward(t *testing.T) {
	observer := newQueryObserver("postgres", &captureSink{})
	relay := observer.(relayClientQueryObserver)
	frame := make([]byte, 5)
	frame[0] = 'Q'
	binary.BigEndian.PutUint32(frame[1:], 3)

	forward, decision := relay.ObserveClientRelayBytes(frame)

	if len(forward) != 0 {
		t.Fatalf("malformed frame forwarded: %x", forward)
	}
	requireObserverDenied(t, decision)
}
