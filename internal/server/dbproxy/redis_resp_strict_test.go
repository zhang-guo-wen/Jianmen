package dbproxy

import (
	"bytes"
	"strings"
	"testing"
)

func TestRedisObserverRejectsMalformedResponseLines(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{name: "LF only", response: "+OK\n"},
		{name: "bare CR", response: "+OK\rX\r\n"},
		{name: "nested LF only", response: "*1\r\n+OK\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer := &redisObserver{
				sink:  &captureSink{},
				slots: []redisResponseSlot{{recorded: true}},
			}
			requireObserverDenied(t, observer.ObserveServerBytes([]byte(tt.response)))
		})
	}
}

func TestRedisObserverRejectsEmptyCommandArray(t *testing.T) {
	observer := &redisObserver{sink: &captureSink{}}
	requireObserverDenied(t, observer.ObserveClientBytes([]byte("*0\r\n")))
}

func TestRedisObserverRejectsResponseLineOverLimit(t *testing.T) {
	observer := &redisObserver{
		sink:  &captureSink{},
		slots: []redisResponseSlot{{recorded: true}},
	}
	response := "+" + strings.Repeat("x", maxRESPAuthLineBytes) + "\r\n"
	requireObserverDenied(t, observer.ObserveServerBytes([]byte(response)))
}

func TestRedisRESPRejectsExcessiveNesting(t *testing.T) {
	response := strings.Repeat("*1\r\n", maxRedisObserverNestingDepth+2) + ":1\r\n"
	if _, status := redisRESPFrameLength([]byte(response)); status != redisRESPLimitExceeded {
		t.Fatalf("nested response status = %v, want limit exceeded", status)
	}
}

func TestRedisObserverConsumesEmptyArrayAsOrdinaryResponse(t *testing.T) {
	sink := &captureSink{}
	observer := &redisObserver{
		sink: sink,
		slots: []redisResponseSlot{{
			record:   queryRecord{seq: 1},
			recorded: true,
		}},
	}

	if decision := observer.ObserveServerBytes([]byte("*0\r\n")); decision != nil {
		t.Fatalf("empty array response denied: %#v", decision)
	}
	if got := len(observer.slots); got != 0 {
		t.Fatalf("response slots = %d, want 0", got)
	}
	if got := len(sink.finished); got != 1 || sink.finished[0].Status != queryStatusSuccess {
		t.Fatalf("finished empty array responses = %#v, want one success", sink.finished)
	}
}

func TestRedisAuthenticationRejectsNonCanonicalArrayAndBulkLengths(t *testing.T) {
	tests := []string{
		"*+2\r\n$4\r\nAUTH\r\n$1\r\nx\r\n",
		"*02\r\n$4\r\nAUTH\r\n$1\r\nx\r\n",
		"*2\r\n$+4\r\nAUTH\r\n$1\r\nx\r\n",
		"*2\r\n$04\r\nAUTH\r\n$1\r\nx\r\n",
		"*2\r\n$4\r\nAUTH\r\n$-0\r\n\r\n",
	}
	for _, input := range tests {
		t.Run(strings.ReplaceAll(input[:strings.Index(input, "\r\n")], "*", "array_"), func(t *testing.T) {
			if _, _, _, err := readRESPCommand(newObserverCopyConn([]byte(input))); err == nil {
				t.Fatalf("authentication parser accepted non-canonical lengths %q", input)
			}
		})
	}
}

func TestRedisRequestObserverRejectsNonCanonicalArrayAndBulkLengths(t *testing.T) {
	tests := [][]byte{
		[]byte("*+2\r\n$3\r\nGET\r\n$1\r\nk\r\n"),
		[]byte("*02\r\n$3\r\nGET\r\n$1\r\nk\r\n"),
		[]byte("*2\r\n$+3\r\nGET\r\n$1\r\nk\r\n"),
		[]byte("*2\r\n$03\r\nGET\r\n$1\r\nk\r\n"),
		[]byte("*2\r\n$3\r\nGET\r\n$-0\r\n\r\n"),
	}
	for _, input := range tests {
		observer := &redisObserver{sink: &captureSink{}}
		requireObserverDenied(t, observer.ObserveClientBytes(input))
	}
}

func TestRedisResponseObserverRejectsNonCanonicalLengthsButAcceptsExactNull(t *testing.T) {
	rejected := [][]byte{
		[]byte("*+1\r\n+OK\r\n"),
		[]byte("*01\r\n+OK\r\n"),
		[]byte("$+1\r\nx\r\n"),
		[]byte("$01\r\nx\r\n"),
		[]byte("$-0\r\n"),
	}
	for _, response := range rejected {
		observer := &redisObserver{
			sink:  &captureSink{},
			slots: []redisResponseSlot{{recorded: true}},
		}
		requireObserverDenied(t, observer.ObserveServerBytes(response))
	}

	for _, response := range [][]byte{[]byte("$-1\r\n"), []byte("*-1\r\n")} {
		sink := &captureSink{}
		observer := &redisObserver{
			sink:  sink,
			slots: []redisResponseSlot{{record: queryRecord{seq: 1}, recorded: true}},
		}
		if decision := observer.ObserveServerBytes(response); decision != nil {
			t.Fatalf("exact RESP null %q denied: %#v", response, decision)
		}
		if len(sink.finished) != 1 {
			t.Fatalf("exact RESP null %q did not complete its slot", response)
		}
	}

	if bytes.Equal([]byte("$-1\r\n"), []byte("$-0\r\n")) {
		t.Fatal("test setup unexpectedly equated canonical null and negative zero")
	}
}
