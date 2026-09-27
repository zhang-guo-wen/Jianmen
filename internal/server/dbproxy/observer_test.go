package dbproxy

import (
	"encoding/binary"
	"sync"
	"testing"
	"time"
)

type captureSink struct {
	queries  []string
	details  []map[string]any
	finished []queryFinish
	deny     bool
}

func (s *captureSink) StartQuery(sql string, detail map[string]any) (queryRecord, queryDecision) {
	s.queries = append(s.queries, sql)
	s.details = append(s.details, detail)
	record := queryRecord{
		seq:       int64(len(s.queries)),
		protocol:  "test",
		sql:       sql,
		queryKind: classifyQueryKind(sql),
		detail:    detail,
		startedAt: time.Now(),
	}
	if s.deny {
		decision := queryDecision{
			Allowed:      false,
			Status:       queryStatusPolicyDenied,
			ErrorCode:    "POLICY_DENIED",
			ErrorMessage: "denied by test policy",
		}
		s.finished = append(s.finished, queryFinish{
			Status:       decision.Status,
			ErrorCode:    decision.ErrorCode,
			ErrorMessage: decision.ErrorMessage,
		})
		return record, decision
	}
	return record, allowQuery()
}

func (s *captureSink) FinishQuery(_ queryRecord, finish queryFinish) {
	s.finished = append(s.finished, finish)
}

type blockingPairSink struct {
	started  chan struct{}
	release  chan struct{}
	mu       sync.Mutex
	finished []queryFinish
}

func (s *blockingPairSink) StartQuery(sql string, detail map[string]any) (queryRecord, queryDecision) {
	close(s.started)
	<-s.release
	return queryRecord{seq: 1, protocol: "mysql", sql: sql, detail: detail}, allowQuery()
}

func (s *blockingPairSink) FinishQuery(_ queryRecord, finish queryFinish) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finished = append(s.finished, finish)
}

func (s *blockingPairSink) finishCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.finished)
}

func TestQueryObserverSerializesConcurrentRequestAndResponsePairing(t *testing.T) {
	sink := &blockingPairSink{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	observer := newQueryObserver("mysql", sink)
	request := buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))
	response := buildMySQLPacket(1, []byte{0x00, 0x00, 0x00})

	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		observer.ObserveClientBytes(request)
	}()
	<-sink.started

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		observer.ObserveServerBytes(response)
	}()

	select {
	case <-serverDone:
		close(sink.release)
	case <-time.After(100 * time.Millisecond):
		close(sink.release)
		<-serverDone
	}
	<-clientDone

	if got := sink.finishCount(); got != 1 {
		t.Fatalf("finished response count = %d, want 1 request/response pair", got)
	}
}

func TestMySQLObserverCapturesQueryAcrossChunks(t *testing.T) {
	sink := &captureSink{}
	observer := &mysqlObserver{sink: sink}
	packet := buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))

	if decision := observer.ObserveClientBytes(packet[:3]); decision != nil {
		t.Fatalf("unexpected decision for partial packet: %#v", decision)
	}
	if decision := observer.ObserveClientBytes(packet[3:]); decision != nil {
		t.Fatalf("unexpected decision: %#v", decision)
	}

	if len(sink.queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(sink.queries))
	}
	if sink.queries[0] != "select [REDACTED]" {
		t.Fatalf("unexpected query %q", sink.queries[0])
	}
	if sink.details[0]["command"] != "COM_QUERY" {
		t.Fatalf("unexpected command detail %#v", sink.details[0])
	}
}

func TestMySQLObserverReturnsErrorResponseWhenDenied(t *testing.T) {
	sink := &captureSink{deny: true}
	observer := &mysqlObserver{sink: sink}
	packet := buildMySQLPacket(0, append([]byte{0x03}, []byte("delete from users")...))

	decision := observer.ObserveClientBytes(packet)

	if decision == nil || decision.Allowed {
		t.Fatalf("expected deny decision, got %#v", decision)
	}
	if decision.Status != queryStatusPolicyDenied {
		t.Fatalf("unexpected decision status %#v", decision)
	}
	if len(sink.finished) != 1 || sink.finished[0].Status != queryStatusPolicyDenied {
		t.Fatalf("expected policy denied finish event, got %#v", sink.finished)
	}
	if response := observer.ErrorResponse(*decision); len(response) == 0 {
		t.Fatal("expected mysql error response")
	}
}

func TestPostgresObserverCapturesSimpleQuery(t *testing.T) {
	sink := &captureSink{}
	observer := &postgresObserver{sink: sink}

	startup := make([]byte, 8)
	binary.BigEndian.PutUint32(startup[0:4], 8)
	binary.BigEndian.PutUint32(startup[4:8], 196608)
	query := postgresMessage('Q', append([]byte("select now()"), 0))

	if decision := observer.ObserveClientBytes(append(startup, query...)); decision != nil {
		t.Fatalf("unexpected decision: %#v", decision)
	}

	if len(sink.queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(sink.queries))
	}
	if sink.queries[0] != "select now()" {
		t.Fatalf("unexpected query %q", sink.queries[0])
	}
	if sink.details[0]["message"] != "Query" {
		t.Fatalf("unexpected message detail %#v", sink.details[0])
	}
}

func TestNewPostgresQueryObserverCapturesPostAuthQuery(t *testing.T) {
	sink := &captureSink{}
	observer := newQueryObserver("postgres", sink)
	query := postgresMessage('Q', append([]byte("select 1"), 0))

	if decision := observer.ObserveClientBytes(query); decision != nil {
		t.Fatalf("unexpected decision: %#v", decision)
	}

	if len(sink.queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(sink.queries))
	}
	if sink.queries[0] != "select [REDACTED]" {
		t.Fatalf("unexpected query %q", sink.queries[0])
	}
}

func TestRedisObserverBoundsIncompleteDeclaredBulk(t *testing.T) {
	sink := &captureSink{}
	observer := &redisObserver{sink: sink}
	header := []byte("*1\r\n$536870912\r\n")
	observer.ObserveClientBytes(header)

	chunk := make([]byte, 4096)
	for i := 0; i < 128; i++ {
		observer.ObserveClientBytes(chunk)
		if len(observer.buf) > 64*1024 {
			t.Fatalf("Redis observer buffer = %d bytes after chunk %d, want at most 64KiB", len(observer.buf), i+1)
		}
	}

	observer.ObserveClientBytes([]byte("*2\r\n$3\r\nSET\r\n$1\r\nx\r\n"))
	if len(sink.queries) != 0 {
		t.Fatalf("observer resumed after an oversized incomplete frame: queries=%v", sink.queries)
	}
}

func TestPostgresObserverBoundsIncompleteClientMessage(t *testing.T) {
	sink := &captureSink{}
	observer := &postgresObserver{sink: sink, startupDone: true}
	header := make([]byte, 5)
	header[0] = 'Q'
	binary.BigEndian.PutUint32(header[1:], 128*1024*1024)
	observer.ObserveClientBytes(header)

	chunk := make([]byte, 4096)
	for i := 0; i < 128; i++ {
		observer.ObserveClientBytes(chunk)
		if len(observer.clientBuf) > 256*1024 {
			t.Fatalf("PostgreSQL observer client buffer = %d bytes after chunk %d, want at most 256KiB", len(observer.clientBuf), i+1)
		}
	}

	observer.ObserveClientBytes(postgresMessage('Q', append([]byte("select 1"), 0)))
	if len(sink.queries) != 0 {
		t.Fatalf("observer resumed after an oversized incomplete frame: queries=%v", sink.queries)
	}
}

func TestMySQLObserverBoundsIncompleteClientAndServerPackets(t *testing.T) {
	for _, observe := range []struct {
		name string
		call func(*mysqlObserver, []byte)
		buf  func(*mysqlObserver) []byte
	}{
		{name: "client", call: func(observer *mysqlObserver, data []byte) {
			observer.ObserveClientBytes(data)
		}, buf: func(observer *mysqlObserver) []byte { return observer.clientBuf }},
		{name: "server", call: func(observer *mysqlObserver, data []byte) {
			observer.ObserveServerBytes(data)
		}, buf: func(observer *mysqlObserver) []byte { return observer.serverBuf }},
	} {
		t.Run(observe.name, func(t *testing.T) {
			observer := &mysqlObserver{sink: &captureSink{}}
			header := []byte{0xff, 0xff, 0xff, 0}
			observe.call(observer, header)

			chunk := make([]byte, 4096)
			for i := 0; i < 128; i++ {
				observe.call(observer, chunk)
				if got := len(observe.buf(observer)); got > 256*1024 {
					t.Fatalf("MySQL observer %s buffer = %d bytes after chunk %d, want at most 256KiB", observe.name, got, i+1)
				}
			}
		})
	}
}

func buildMySQLPacket(seq byte, payload []byte) []byte {
	packet := make([]byte, 4+len(payload))
	packet[0] = byte(len(payload))
	packet[1] = byte(len(payload) >> 8)
	packet[2] = byte(len(payload) >> 16)
	packet[3] = seq
	copy(packet[4:], payload)
	return packet
}

func postgresMessage(typ byte, payload []byte) []byte {
	msg := make([]byte, 1+4+len(payload))
	msg[0] = typ
	binary.BigEndian.PutUint32(msg[1:5], uint32(4+len(payload)))
	copy(msg[5:], payload)
	return msg
}
