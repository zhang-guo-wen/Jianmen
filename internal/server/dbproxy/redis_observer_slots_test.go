package dbproxy

import (
	"bytes"
	"fmt"
	"testing"
)

func TestRedisObserverMatchesRecordedAndUnrecordedResponseSlots(t *testing.T) {
	sink := &redisSlotSink{}
	observer := &redisObserver{sink: sink}
	commands := bytes.Join([][]byte{
		redisObserverTestCommand("SET", "key", "value"),
		redisObserverTestCommand("PING"),
		redisObserverTestCommand("GET", "key"),
		redisObserverTestCommand("SELECT", "999"),
	}, nil)

	if decision := observer.ObserveClientBytes(commands); decision != nil {
		t.Fatalf("client pipeline denied: %#v", decision)
	}
	if got := sink.started; len(got) != 2 || got[0] != "SET key [REDACTED]" || got[1] != "GET key" {
		t.Fatalf("recorded commands = %v, want SET and GET", got)
	}

	if decision := observer.ObserveServerBytes([]byte("+O")); decision != nil {
		t.Fatalf("partial server response denied: %#v", decision)
	}
	if len(sink.finished) != 0 {
		t.Fatalf("finished partial responses = %d, want 0", len(sink.finished))
	}
	responses := []byte("K\r\n+PONG\r\n-WRONGTYPE get failed\r\n-ERR invalid DB index\r\n")
	if decision := observer.ObserveServerBytes(responses); decision != nil {
		t.Fatalf("merged server responses denied: %#v", decision)
	}

	if got := len(sink.finished); got != 2 {
		t.Fatalf("finished recorded commands = %d, want 2", got)
	}
	if sink.finished[0].sql != "SET key [REDACTED]" || sink.finished[0].finish.Status != queryStatusSuccess {
		t.Fatalf("SET finish = %#v", sink.finished[0])
	}
	if sink.finished[1].sql != "GET key" || sink.finished[1].finish.Status != queryStatusError {
		t.Fatalf("GET finish = %#v, want GET error response", sink.finished[1])
	}
	if sink.finished[1].finish.ErrorMessage != "redis upstream error" {
		t.Fatalf("GET error = %q, want fixed sanitized message", sink.finished[1].finish.ErrorMessage)
	}
	if got := len(observer.slots); got != 0 {
		t.Fatalf("response slots after complete pipeline = %d, want 0", got)
	}
}

func TestRedisObserverTreatsMessageArrayAsOrdinaryLRANGEResponse(t *testing.T) {
	sink := &redisSlotSink{}
	observer := &redisObserver{sink: sink}
	if decision := observer.ObserveClientBytes(redisObserverTestCommand("LRANGE", "events", "0", "2")); decision != nil {
		t.Fatalf("LRANGE denied: %#v", decision)
	}

	response := []byte("*3\r\n$7\r\nmessage\r\n$4\r\nchan\r\n$5\r\nvalue\r\n")
	split := len(response) / 2
	if decision := observer.ObserveServerBytes(response[:split]); decision != nil {
		t.Fatalf("partial array response denied: %#v", decision)
	}
	if decision := observer.ObserveServerBytes(response[split:]); decision != nil {
		t.Fatalf("complete array response denied: %#v", decision)
	}

	if got := len(sink.finished); got != 1 {
		t.Fatalf("finished commands = %d, want 1", got)
	}
	if sink.finished[0].sql != "LRANGE events [REDACTED]" ||
		sink.finished[0].finish.Status != queryStatusSuccess {
		t.Fatalf("LRANGE finish = %#v, want successful ordinary array response", sink.finished[0])
	}
	if got := len(observer.slots); got != 0 {
		t.Fatalf("response slots = %d, want 0", got)
	}
}

func TestRedisObserverResponseSlotLimitIncludesUnrecordedCommands(t *testing.T) {
	observer := &redisObserver{sink: &redisSlotSink{}}
	ping := redisObserverTestCommand("PING")

	for i := 0; i < maxObserverPendingQueries; i++ {
		if decision := observer.ObserveClientBytes(ping); decision != nil {
			t.Fatalf("PING %d denied before slot limit: %#v", i+1, decision)
		}
	}
	requireObserverDenied(t, observer.ObserveClientBytes(ping))
}

type redisSlotSink struct {
	started  []string
	finished []redisSlotFinish
}

type redisSlotFinish struct {
	sql    string
	finish queryFinish
}

func (s *redisSlotSink) StartQuery(sql string, _ map[string]any) (queryRecord, queryDecision) {
	s.started = append(s.started, sql)
	return queryRecord{seq: int64(len(s.started)), sql: sql}, allowQuery()
}

func (s *redisSlotSink) FinishQuery(record queryRecord, finish queryFinish) {
	s.finished = append(s.finished, redisSlotFinish{sql: record.sql, finish: finish})
}

func redisObserverTestCommand(parts ...string) []byte {
	var buffer bytes.Buffer
	_, _ = fmt.Fprintf(&buffer, "*%d\r\n", len(parts))
	for _, part := range parts {
		_, _ = fmt.Fprintf(&buffer, "$%d\r\n%s\r\n", len(part), part)
	}
	return buffer.Bytes()
}
