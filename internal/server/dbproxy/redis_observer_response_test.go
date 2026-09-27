package dbproxy

import (
	"strings"
	"testing"
)

func TestRedisObserverWaitsForCompleteSplitServerResponse(t *testing.T) {
	sink := &captureSink{}
	observer := &redisObserver{
		sink:  sink,
		slots: []redisResponseSlot{{record: queryRecord{seq: 1}, recorded: true}},
	}

	observer.ObserveServerBytes([]byte("+O"))
	if got := len(observer.slots); got != 1 {
		t.Fatalf("slots after partial response = %d, want 1", got)
	}
	if got := len(sink.finished); got != 0 {
		t.Fatalf("finished after partial response = %d, want 0", got)
	}

	observer.ObserveServerBytes([]byte("K\r\n"))
	if got := len(observer.slots); got != 0 {
		t.Fatalf("slots after complete response = %d, want 0", got)
	}
	if got := len(sink.finished); got != 1 {
		t.Fatalf("finished after complete response = %d, want 1", got)
	}
	if sink.finished[0].Status != queryStatusSuccess {
		t.Fatalf("response status = %q, want success", sink.finished[0].Status)
	}
}

func TestRedisObserverConsumesMultipleServerResponsesFromOneChunk(t *testing.T) {
	sink := &captureSink{}
	observer := &redisObserver{
		sink: sink,
		slots: []redisResponseSlot{
			{record: queryRecord{seq: 1}, recorded: true},
			{record: queryRecord{seq: 2}, recorded: true},
		},
	}

	observer.ObserveServerBytes([]byte("+OK\r\n:1\r\n"))

	if got := len(observer.slots); got != 0 {
		t.Fatalf("slots after merged responses = %d, want 0", got)
	}
	if got := len(sink.finished); got != 2 {
		t.Fatalf("finished responses = %d, want 2", got)
	}
	for i, finish := range sink.finished {
		if finish.Status != queryStatusSuccess {
			t.Fatalf("response %d status = %q, want success", i+1, finish.Status)
		}
	}
}

func TestRedisObserverSanitizesUpstreamErrorBeforeAudit(t *testing.T) {
	const secret = "upstream-echoed-secret"
	sink := &captureSink{}
	observer := &redisObserver{
		sink:  sink,
		slots: []redisResponseSlot{{record: queryRecord{seq: 1}, recorded: true}},
	}

	if decision := observer.ObserveServerBytes([]byte("-ERR script returned " + secret + "\r\n")); decision != nil {
		t.Fatalf("unexpected observer decision: %#v", decision)
	}
	if len(sink.finished) != 1 {
		t.Fatalf("finished responses = %d, want 1", len(sink.finished))
	}
	finish := sink.finished[0]
	if finish.Status != queryStatusError || finish.ErrorCode != "ERR" {
		t.Fatalf("finish = %#v, want sanitized ERR category", finish)
	}
	if finish.ErrorMessage != "redis upstream error" {
		t.Fatalf("error message = %q, want fixed message", finish.ErrorMessage)
	}
	if strings.Contains(finish.ErrorMessage, secret) {
		t.Fatalf("sanitized finish exposed secret %q", secret)
	}
}
