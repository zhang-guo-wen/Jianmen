package dbproxy

import (
	"encoding/binary"
	"testing"
)

const testObserverPendingLimit = 32

func TestObserversRejectOversizedClientFramesPermanently(t *testing.T) {
	mysqlHeader := []byte{0xff, 0xff, 0xff, 0}
	postgresHeader := make([]byte, 5)
	postgresHeader[0] = 'Q'
	binary.BigEndian.PutUint32(postgresHeader[1:], 128*1024*1024)

	tests := []struct {
		name       string
		observer   queryObserver
		oversized  []byte
		validFrame []byte
	}{
		{
			name:       "MySQL",
			observer:   &mysqlObserver{sink: &captureSink{}},
			oversized:  mysqlHeader,
			validFrame: buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...)),
		},
		{
			name:       "PostgreSQL",
			observer:   &postgresObserver{sink: &captureSink{}, startupDone: true},
			oversized:  postgresHeader,
			validFrame: postgresMessage('Q', append([]byte("select 1"), 0)),
		},
		{
			name:       "Redis",
			observer:   &redisObserver{sink: &captureSink{}},
			oversized:  []byte("*1\r\n$536870912\r\n"),
			validFrame: []byte("*2\r\n$3\r\nSET\r\n$1\r\nx\r\n"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireObserverDenied(t, tt.observer.ObserveClientBytes(tt.oversized))
			requireObserverDenied(t, tt.observer.ObserveClientBytes(tt.validFrame))
		})
	}
}

func TestRedisObserverRejectsMalformedFramesPermanently(t *testing.T) {
	tests := []struct {
		name  string
		frame []byte
	}{
		{name: "non RESP", frame: []byte("PING\r\n")},
		{name: "invalid array length", frame: []byte("*nope\r\n")},
		{name: "array count limit", frame: []byte("*257\r\n")},
		{name: "non bulk argument", frame: []byte("*1\r\n+PING\r\n")},
		{name: "negative bulk", frame: []byte("*1\r\n$-1\r\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &captureSink{}
			observer := &redisObserver{sink: sink}

			requireObserverDenied(t, observer.ObserveClientBytes(tt.frame))
			requireObserverDenied(t, observer.ObserveClientBytes([]byte("*2\r\n$3\r\nSET\r\n$1\r\nx\r\n")))
			if len(sink.queries) != 0 {
				t.Fatalf("queries after malformed frame = %v, want none", sink.queries)
			}
		})
	}
}

func TestObserverPendingQueueLimitIsFailClosed(t *testing.T) {
	tests := []struct {
		name       string
		observer   queryObserver
		frame      []byte
		pendingLen func() int
		sink       *captureSink
	}{
		func() struct {
			name       string
			observer   queryObserver
			frame      []byte
			pendingLen func() int
			sink       *captureSink
		} {
			sink := &captureSink{}
			observer := &mysqlObserver{sink: sink}
			return struct {
				name       string
				observer   queryObserver
				frame      []byte
				pendingLen func() int
				sink       *captureSink
			}{
				name:       "MySQL",
				observer:   observer,
				frame:      buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...)),
				pendingLen: func() int { return len(observer.pending) },
				sink:       sink,
			}
		}(),
		func() struct {
			name       string
			observer   queryObserver
			frame      []byte
			pendingLen func() int
			sink       *captureSink
		} {
			sink := &captureSink{}
			observer := &postgresObserver{sink: sink, startupDone: true}
			return struct {
				name       string
				observer   queryObserver
				frame      []byte
				pendingLen func() int
				sink       *captureSink
			}{
				name:       "PostgreSQL",
				observer:   observer,
				frame:      postgresMessage('Q', append([]byte("select 1"), 0)),
				pendingLen: func() int { return len(observer.pending) },
				sink:       sink,
			}
		}(),
		func() struct {
			name       string
			observer   queryObserver
			frame      []byte
			pendingLen func() int
			sink       *captureSink
		} {
			sink := &captureSink{}
			observer := &redisObserver{sink: sink}
			return struct {
				name       string
				observer   queryObserver
				frame      []byte
				pendingLen func() int
				sink       *captureSink
			}{
				name:       "Redis",
				observer:   observer,
				frame:      []byte("*2\r\n$3\r\nSET\r\n$1\r\nx\r\n"),
				pendingLen: func() int { return len(observer.slots) },
				sink:       sink,
			}
		}(),
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < testObserverPendingLimit; i++ {
				if decision := tt.observer.ObserveClientBytes(tt.frame); decision != nil {
					t.Fatalf("request %d denied before pending limit: %#v", i+1, decision)
				}
			}
			requireObserverDenied(t, tt.observer.ObserveClientBytes(tt.frame))
			if got := tt.pendingLen(); got > testObserverPendingLimit {
				t.Fatalf("pending queue length = %d, want at most %d", got, testObserverPendingLimit)
			}
			if got := len(tt.sink.queries); got != testObserverPendingLimit {
				t.Fatalf("started query count = %d, want %d", got, testObserverPendingLimit)
			}
			requireObserverDenied(t, tt.observer.ObserveClientBytes(tt.frame))
		})
	}
}

func requireObserverDenied(t *testing.T, decision *queryDecision) {
	t.Helper()
	if decision == nil || decision.Allowed {
		t.Fatalf("observer decision = %#v, want fail-closed denial", decision)
	}
}
