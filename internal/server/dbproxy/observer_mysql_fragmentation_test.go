package dbproxy

import (
	"bytes"
	"testing"
)

func TestMySQLObserverTreatsPhysicalContinuationsAsOneRowPacket(t *testing.T) {
	firstFragment := bytes.Repeat(
		[]byte{'x'},
		maxMySQLPhysicalPacketPayloadBytes,
	)
	tests := []struct {
		name         string
		continuation []byte
	}{
		{
			name:         "continuation beginning with error marker",
			continuation: []byte{0xff, 0x15, 0x04},
		},
		{
			name:         "short continuation beginning with EOF marker",
			continuation: []byte{0xfe, 0, 0, 0, 0},
		},
		{
			name:         "exact multiple has zero length terminator",
			continuation: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &captureSink{}
			observer := &mysqlObserver{sink: sink}
			startMySQLFragmentedRowResponse(t, observer)

			relayMySQLPhysicalPacket(t, observer, 4, firstFragment)
			if len(sink.finished) != 0 || !observer.HasPending() {
				t.Fatal("first maximum-size fragment completed the logical row")
			}
			relayMySQLPhysicalPacket(t, observer, 5, tt.continuation)
			if len(sink.finished) != 0 || !observer.HasPending() {
				t.Fatal("row continuation was interpreted as an independent response packet")
			}

			terminator := buildMySQLPacket(6, []byte{0xfe, 0, 0, 0, 0})
			forward, decision := observer.ObserveServerRelayBytes(terminator)
			requireMySQLRelayForward(t, forward, decision, terminator)
			requireSingleSuccessfulFinish(t, sink)
			assertMySQLObserverAcceptsNextQuery(t, observer, sink)
		})
	}
}

func TestMySQLObserverDefersFragmentedTerminalPacketUntilLogicalEnd(t *testing.T) {
	tests := []struct {
		name       string
		prefix     []byte
		wantStatus string
	}{
		{
			name:       "fragmented ERR packet",
			prefix:     []byte{0xff, 0x28, 0x04},
			wantStatus: queryStatusError,
		},
		{
			name:       "fragmented OK packet",
			prefix:     []byte{0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00},
			wantStatus: queryStatusSuccess,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &captureSink{}
			observer := &mysqlObserver{sink: sink}
			query := buildMySQLPacket(0, append([]byte{0x03}, []byte("select terminal")...))
			forward, decision := observer.ObserveClientRelayBytes(query)
			requireMySQLRelayForward(t, forward, decision, query)

			firstFragment := bytes.Repeat(
				[]byte{'z'},
				maxMySQLPhysicalPacketPayloadBytes,
			)
			copy(firstFragment, tt.prefix)
			relayMySQLPhysicalPacket(t, observer, 1, firstFragment)
			if len(sink.finished) != 0 || !observer.HasPending() {
				t.Fatal("maximum-size terminal fragment completed before its continuation")
			}

			relayMySQLPhysicalPacket(t, observer, 2, []byte("tail"))
			if len(sink.finished) != 1 || sink.finished[0].Status != tt.wantStatus {
				t.Fatalf("terminal finish = %#v, want status %q", sink.finished, tt.wantStatus)
			}
			assertMySQLObserverAcceptsNextQuery(t, observer, sink)
		})
	}
}

func startMySQLFragmentedRowResponse(t *testing.T, observer *mysqlObserver) {
	t.Helper()
	query := buildMySQLPacket(0, append([]byte{0x03}, []byte("select fragmented")...))
	forward, decision := observer.ObserveClientRelayBytes(query)
	requireMySQLRelayForward(t, forward, decision, query)

	columnCount := buildMySQLPacket(1, []byte{0x01})
	columnDefinition := buildMySQLPacket(2, []byte("fragmented"))
	columnTerminator := buildMySQLPacket(3, []byte{0xfe, 0, 0, 0, 0})
	metadata := append(append(columnCount, columnDefinition...), columnTerminator...)
	forward, decision = observer.ObserveServerRelayBytes(metadata)
	requireMySQLRelayForward(t, forward, decision, metadata)
}

func relayMySQLPhysicalPacket(
	t *testing.T,
	observer *mysqlObserver,
	sequence byte,
	payload []byte,
) {
	t.Helper()
	if len(payload)+4 <= maxMySQLObserverBufferBytes {
		packet := buildMySQLPacket(sequence, payload)
		forward, decision := observer.ObserveServerRelayBytes(packet)
		requireMySQLRelayForward(t, forward, decision, packet)
		return
	}
	header := []byte{
		byte(len(payload)),
		byte(len(payload) >> 8),
		byte(len(payload) >> 16),
		sequence,
	}
	forward, decision := observer.ObserveServerRelayBytes(header)
	requireMySQLRelayForward(t, forward, decision, header)

	const chunkSize = 64 * 1024
	for len(payload) > 0 {
		size := chunkSize
		if size > len(payload) {
			size = len(payload)
		}
		chunk := payload[:size]
		forward, decision = observer.ObserveServerRelayBytes(chunk)
		requireMySQLRelayForward(t, forward, decision, chunk)
		payload = payload[size:]
	}
}

func assertMySQLObserverAcceptsNextQuery(
	t *testing.T,
	observer *mysqlObserver,
	sink *captureSink,
) {
	t.Helper()
	previousFinishes := len(sink.finished)
	query := buildMySQLPacket(0, append([]byte{0x03}, []byte("select next")...))
	forward, decision := observer.ObserveClientRelayBytes(query)
	requireMySQLRelayForward(t, forward, decision, query)
	okPacket := buildMySQLPacket(1, []byte{0x00, 0, 0, 2, 0, 0, 0})
	forward, decision = observer.ObserveServerRelayBytes(okPacket)
	requireMySQLRelayForward(t, forward, decision, okPacket)
	if len(sink.finished) != previousFinishes+1 ||
		sink.finished[len(sink.finished)-1].Status != queryStatusSuccess {
		t.Fatalf("next query finish = %#v", sink.finished)
	}
}
