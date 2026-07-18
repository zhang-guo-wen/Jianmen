package dbproxy

import (
	"bytes"
	"testing"
)

func TestPostgresClientRelayRejectsUnknownAndMalformedCompleteMessages(t *testing.T) {
	tests := []struct {
		name    string
		setup   []byte
		message []byte
	}{
		{name: "unknown type", message: postgresMessage('Y', nil)},
		{name: "query missing terminator", message: postgresMessage('Q', []byte("select 1"))},
		{name: "parse missing parameter count", message: postgresMessage('P', []byte("stmt\x00select 1\x00"))},
		{
			name:    "bind truncated sections",
			setup:   postgresMessage('P', []byte("stmt\x00select 1\x00\x00\x00")),
			message: postgresMessage('B', []byte("portal\x00stmt\x00\x00\x00")),
		},
		{
			name:    "bind invalid parameter format",
			setup:   postgresMessage('P', []byte("stmt\x00select 1\x00\x00\x00")),
			message: postgresMessage('B', []byte("portal\x00stmt\x00\x00\x01\x00\x02\x00\x01\x00\x00\x00\x00\x00\x00")),
		},
		{
			name:    "bind parameter format count mismatch",
			setup:   postgresMessage('P', []byte("stmt\x00select 1\x00\x00\x00")),
			message: postgresMessage('B', []byte("portal\x00stmt\x00\x00\x02\x00\x00\x00\x01\x00\x01\x00\x00\x00\x00\x00\x00")),
		},
		{
			name:    "bind invalid result format",
			setup:   postgresMessage('P', []byte("stmt\x00select 1\x00\x00\x00")),
			message: postgresMessage('B', []byte("portal\x00stmt\x00\x00\x00\x00\x00\x00\x01\x00\x02")),
		},
		{
			name: "execute missing row limit",
			setup: append(
				postgresMessage('P', []byte("stmt\x00select 1\x00\x00\x00")),
				postgresBindNoParameters("portal", "stmt")...,
			),
			message: postgresMessage('E', []byte("portal\x00")),
		},
		{name: "close invalid target", message: postgresMessage('C', []byte("Xstmt\x00"))},
		{name: "describe missing terminator", message: postgresMessage('D', []byte("Sstmt"))},
		{name: "sync with payload", message: postgresMessage('S', []byte{0})},
		{name: "copy fail missing terminator", message: postgresMessage('f', []byte("failed"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer := newQueryObserver("postgres", &captureSink{})
			relay := observer.(relayClientQueryObserver)
			query := postgresMessage('Q', []byte("select 1\x00"))
			safePrefix := append(append([]byte(nil), query...), tt.setup...)

			forward, decision := relay.ObserveClientRelayBytes(append(safePrefix, tt.message...))

			if !bytes.Equal(forward, safePrefix) {
				t.Fatalf("safe prefix = %x, want %x", forward, safePrefix)
			}
			requireObserverDenied(t, decision)
		})
	}
}

func TestPostgresClientRelayAllowsStructurallyValidExtendedAndCopyMessages(t *testing.T) {
	observer := newQueryObserver("postgres", &captureSink{})
	relay := observer.(relayClientQueryObserver)
	messages := bytes.Join([][]byte{
		postgresMessage('P', []byte("stmt\x00select $1\x00\x00\x01\x00\x00\x00\x17")),
		postgresMessage('D', []byte("Sstmt\x00")),
		postgresMessage('B', []byte("portal\x00stmt\x00\x00\x01\x00\x00\x00\x01\x00\x00\x00\x03abc\x00\x01\x00\x01")),
		postgresMessage('E', []byte("portal\x00\x00\x00\x00\x00")),
		postgresMessage('H', nil),
		postgresMessage('S', nil),
		postgresMessage('d', []byte("copy payload")),
		postgresMessage('c', nil),
		postgresMessage('f', []byte("copy failed\x00")),
		postgresMessage('C', []byte("Pportal\x00")),
	}, nil)

	forward, decision := relay.ObserveClientRelayBytes(messages)

	if decision != nil {
		t.Fatalf("valid frontend messages denied: %#v", decision)
	}
	if !bytes.Equal(forward, messages) {
		t.Fatalf("forwarded valid messages = %x, want %x", forward, messages)
	}
}

func TestPostgresServerRelayRejectsUnknownAndMalformedCompleteMessages(t *testing.T) {
	tests := []struct {
		name    string
		message []byte
	}{
		{name: "unknown type", message: postgresMessage('Y', nil)},
		{name: "command missing terminator", message: postgresMessage('C', []byte("SELECT 1"))},
		{name: "ready missing status", message: postgresMessage('Z', nil)},
		{name: "error missing terminal zero", message: postgresMessage('E', []byte{'C', '4', '2', '0', '0', '0'})},
		{name: "data row truncated", message: postgresMessage('D', []byte{0, 1, 0, 0, 0, 4, 'a'})},
		{name: "row description truncated", message: postgresMessage('T', []byte{0, 1, 'x', 0})},
		{
			name: "row description invalid format",
			message: postgresMessage('T', []byte{
				0, 1,
				'x', 0,
				0, 0, 0, 0,
				0, 0,
				0, 0, 0, 23,
				0, 4,
				0xff, 0xff, 0xff, 0xff,
				0, 2,
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer := newQueryObserver("postgres", &captureSink{})
			clientRelay := observer.(relayClientQueryObserver)
			serverRelay := observer.(relayServerQueryObserver)
			query := postgresMessage('Q', []byte("select 1\x00"))
			if _, decision := clientRelay.ObserveClientRelayBytes(query); decision != nil {
				t.Fatalf("query denied: %#v", decision)
			}
			commandComplete := postgresMessage('C', []byte("SELECT 1\x00"))

			forward, decision := serverRelay.ObserveServerRelayBytes(append(commandComplete, tt.message...))

			if !bytes.Equal(forward, commandComplete) {
				t.Fatalf("safe server prefix = %x, want %x", forward, commandComplete)
			}
			requireObserverDenied(t, decision)
		})
	}
}
