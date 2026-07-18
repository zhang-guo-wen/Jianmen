package dbproxy

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"testing"
)

func TestDatabaseObserversStreamLargeResponsesWithoutChangingExecutionOutcome(t *testing.T) {
	t.Run("mysql row packet", func(t *testing.T) {
		sink := &captureSink{}
		observer := &mysqlObserver{sink: sink}
		query := buildMySQLPacket(0, append([]byte{0x03}, []byte("select audit_probe")...))
		forward, decision := observer.ObserveClientRelayBytes(query)
		requireMySQLRelayForward(t, forward, decision, query)

		columnCount := buildMySQLPacket(1, []byte{0x01})
		columnDefinition := buildMySQLPacket(2, []byte("audit_probe"))
		columnTerminator := buildMySQLPacket(3, []byte{0xfe, 0, 0, 0, 0})
		largeRow := buildMySQLPacket(4, bytes.Repeat([]byte{'x'}, maxMySQLObserverBufferBytes+1024))
		rowTerminator := buildMySQLPacket(5, []byte{0xfe, 0, 0, 0, 0})
		response := append(columnCount, columnDefinition...)
		response = append(response, columnTerminator...)
		response = append(response, largeRow...)
		response = append(response, rowTerminator...)

		forward, decision = feedObserverChunks(observer.ObserveServerRelayBytes, response, 32*1024)
		if decision != nil {
			t.Fatalf("large MySQL response denied after upstream execution: %#v", decision)
		}
		if !bytes.Equal(forward, response) {
			t.Fatalf("large MySQL response forwarded %d bytes, want %d", len(forward), len(response))
		}
		requireSingleSuccessfulFinish(t, sink)
	})

	t.Run("postgres data row", func(t *testing.T) {
		sink := &captureSink{}
		observer := &postgresObserver{sink: sink, startupDone: true}
		query := postgresMessage('Q', []byte("select audit_probe\x00"))
		forward, decision := observer.ObserveClientRelayBytes(query)
		requireMySQLRelayForward(t, forward, decision, query)

		value := bytes.Repeat([]byte{'x'}, maxPostgresObserverBufferBytes+1024)
		dataRowPayload := make([]byte, 6+len(value))
		binary.BigEndian.PutUint16(dataRowPayload[:2], 1)
		binary.BigEndian.PutUint32(dataRowPayload[2:6], uint32(len(value)))
		copy(dataRowPayload[6:], value)
		response := postgresMessage('D', dataRowPayload)
		response = append(response, postgresMessage('C', []byte("SELECT 1\x00"))...)
		response = append(response, postgresReadyForQuery()...)

		forward, decision = feedObserverChunks(observer.ObserveServerRelayBytes, response, 32*1024)
		if decision != nil {
			t.Fatalf("large PostgreSQL response denied after upstream execution: %#v", decision)
		}
		if !bytes.Equal(forward, response) {
			t.Fatalf("large PostgreSQL response forwarded %d bytes, want %d", len(forward), len(response))
		}
		requireSingleSuccessfulFinish(t, sink)
	})

	t.Run("redis bulk string", func(t *testing.T) {
		sink := &captureSink{}
		observer := &redisObserver{sink: sink}
		command := []byte("*2\r\n$3\r\nGET\r\n$11\r\naudit_probe\r\n")
		forward, decision := observer.ObserveClientRelayBytes(command)
		requireMySQLRelayForward(t, forward, decision, command)

		response := redisBulkWithExactLength(t, maxRedisObserverBufferBytes+1024)
		forward, decision = feedObserverChunks(observer.ObserveServerRelayBytes, response, 32*1024)
		if decision != nil {
			t.Fatalf("large Redis response denied after upstream execution: %#v", decision)
		}
		if !bytes.Equal(forward, response) {
			t.Fatalf("large Redis response forwarded %d bytes, want %d", len(forward), len(response))
		}
		requireSingleSuccessfulFinish(t, sink)
	})

	t.Run("redis nested bulk followed by pipelined response", func(t *testing.T) {
		sink := &captureSink{}
		observer := &redisObserver{sink: sink}
		first := []byte("*2\r\n$3\r\nGET\r\n$5\r\nfirst\r\n")
		second := []byte("*2\r\n$3\r\nGET\r\n$6\r\nsecond\r\n")
		commands := append(append([]byte(nil), first...), second...)
		forward, decision := observer.ObserveClientRelayBytes(commands)
		requireMySQLRelayForward(t, forward, decision, commands)

		value := bytes.Repeat([]byte{'x'}, maxRedisObserverBufferBytes+1024)
		response := []byte("*2\r\n$" + strconv.Itoa(len(value)) + "\r\n")
		response = append(response, value...)
		response = append(response, []byte("\r\n:1\r\n+OK\r\n")...)
		forward, decision = feedObserverChunks(observer.ObserveServerRelayBytes, response, 32*1024)
		if decision != nil {
			t.Fatalf("nested Redis response denied after upstream execution: %#v", decision)
		}
		if !bytes.Equal(forward, response) {
			t.Fatalf("nested Redis response forwarded %d bytes, want %d", len(forward), len(response))
		}
		if len(sink.finished) != 2 {
			t.Fatalf("finished audit entries = %d, want 2", len(sink.finished))
		}
		for index, finish := range sink.finished {
			if finish.Status != queryStatusSuccess {
				t.Fatalf("finished audit %d status = %q, want %q", index, finish.Status, queryStatusSuccess)
			}
		}
	})
}

func TestPostgresObserverStreamsLargeCopyDataAfterTheQueryWasAudited(t *testing.T) {
	sink := &captureSink{}
	observer := &postgresObserver{sink: sink, startupDone: true}
	query := postgresMessage('Q', []byte("copy audit_probe from stdin\x00"))
	forward, decision := observer.ObserveClientRelayBytes(query)
	requireMySQLRelayForward(t, forward, decision, query)
	if len(sink.queries) != 1 {
		t.Fatalf("audited queries = %d, want 1", len(sink.queries))
	}
	copyData := postgresMessage('d', bytes.Repeat([]byte{'x'}, maxPostgresObserverBufferBytes+1024))
	syncMessage := postgresMessage('S', nil)
	input := append(append([]byte(nil), copyData...), syncMessage...)

	forward, decision = feedObserverChunks(observer.ObserveClientRelayBytes, input, 32*1024)
	if decision != nil {
		t.Fatalf("large PostgreSQL CopyData denied: %#v", decision)
	}
	if !bytes.Equal(forward, input) {
		t.Fatalf("large PostgreSQL CopyData forwarded %d bytes, want %d", len(forward), len(input))
	}
}

func feedObserverChunks(
	relay func([]byte) ([]byte, *queryDecision),
	data []byte,
	chunkSize int,
) ([]byte, *queryDecision) {
	var forward []byte
	for len(data) > 0 {
		size := chunkSize
		if size > len(data) {
			size = len(data)
		}
		chunk, decision := relay(data[:size])
		forward = append(forward, chunk...)
		data = data[size:]
		if decision != nil {
			return forward, decision
		}
	}
	return forward, nil
}

func requireSingleSuccessfulFinish(t *testing.T, sink *captureSink) {
	t.Helper()
	if len(sink.finished) != 1 {
		t.Fatalf("finished audit entries = %d, want 1", len(sink.finished))
	}
	if sink.finished[0].Status != queryStatusSuccess {
		t.Fatalf("finished audit status = %q, want %q", sink.finished[0].Status, queryStatusSuccess)
	}
}
