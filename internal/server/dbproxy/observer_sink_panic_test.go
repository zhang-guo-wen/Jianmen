package dbproxy

import (
	"sync"
	"testing"
	"time"

	"jianmen/internal/model"
)

const testObserverAuditFailure = "OBSERVER_AUDIT_FAILURE"

type permanentPanicSink struct {
	mu          sync.Mutex
	startCalls  int
	finishCalls int
	panicStart  bool
}

func (s *permanentPanicSink) StartQuery(sql string, detail map[string]any) (queryRecord, queryDecision) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startCalls++
	if s.panicStart {
		panic("start sink secret")
	}
	return queryRecord{
		seq:       int64(s.startCalls),
		protocol:  "test",
		sql:       sql,
		queryKind: classifyQueryKind(sql),
		detail:    detail,
		startedAt: time.Now(),
	}, allowQuery()
}

func (s *permanentPanicSink) FinishQuery(queryRecord, queryFinish) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finishCalls++
	panic("finish sink secret")
}

func (s *permanentPanicSink) finishes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finishCalls
}

func TestObserversFailClosedWithoutRetryingPermanentFinishPanic(t *testing.T) {
	tests := []struct {
		name  string
		build func(*permanentPanicSink) (queryObserverLifecycle, func() *queryDecision)
	}{
		{
			name: "mysql",
			build: func(sink *permanentPanicSink) (queryObserverLifecycle, func() *queryDecision) {
				observer := &mysqlObserver{sink: sink}
				requireNoDecision(t, observer.ObserveClientBytes(buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))))
				return observer, func() *queryDecision {
					return observer.ObserveServerBytes(buildMySQLPacket(1, []byte{0x00, 0, 0, 0, 0, 0, 0}))
				}
			},
		},
		{
			name: "postgres",
			build: func(sink *permanentPanicSink) (queryObserverLifecycle, func() *queryDecision) {
				observer := &postgresObserver{sink: sink, startupDone: true}
				requireNoDecision(t, observer.ObserveClientBytes(postgresMessage('Q', append([]byte("select 1"), 0))))
				response := append(
					postgresMessage('C', append([]byte("SELECT 1"), 0)),
					postgresReadyForQuery()...,
				)
				return observer, func() *queryDecision {
					return observer.ObserveServerBytes(response)
				}
			},
		},
		{
			name: "redis",
			build: func(sink *permanentPanicSink) (queryObserverLifecycle, func() *queryDecision) {
				observer := &redisObserver{sink: sink}
				requireNoDecision(t, observer.ObserveClientBytes(redisObserverTestCommand("GET", "key")))
				return observer, func() *queryDecision {
					return observer.ObserveServerBytes([]byte("+OK\r\n"))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &permanentPanicSink{}
			lifecycle, finish := tt.build(sink)

			decision, panicValue := invokeObserverDecision(finish)

			if panicValue != nil {
				t.Fatalf("finish panic escaped observer: %v", panicValue)
			}
			requireObserverDenied(t, decision)
			if decision.ErrorCode != testObserverAuditFailure {
				t.Fatalf("error code = %q, want %q", decision.ErrorCode, testObserverAuditFailure)
			}
			if got := sink.finishes(); got != 1 {
				t.Fatalf("finish attempts = %d, want exactly 1", got)
			}
			if lifecycle.HasPending() {
				t.Fatal("terminal observer retained active pending work")
			}

			_, panicValue = invokeObserverVoid(func() { lifecycle.Abort(observerErrorRelay) })
			if panicValue != nil {
				t.Fatalf("abort after sink panic escaped: %v", panicValue)
			}
			if got := sink.finishes(); got != 1 {
				t.Fatalf("finish attempts after abort = %d, want no retry", got)
			}
		})
	}
}

func TestObserversFailClosedWhenStartSinkPanics(t *testing.T) {
	tests := []struct {
		name    string
		observe func(querySink) *queryDecision
	}{
		{
			name: "mysql",
			observe: func(sink querySink) *queryDecision {
				return (&mysqlObserver{sink: sink}).ObserveClientBytes(
					buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...)),
				)
			},
		},
		{
			name: "postgres",
			observe: func(sink querySink) *queryDecision {
				return (&postgresObserver{sink: sink, startupDone: true}).ObserveClientBytes(
					postgresMessage('Q', append([]byte("select 1"), 0)),
				)
			},
		},
		{
			name: "redis",
			observe: func(sink querySink) *queryDecision {
				return (&redisObserver{sink: sink}).ObserveClientBytes(
					redisObserverTestCommand("GET", "key"),
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &permanentPanicSink{panicStart: true}

			decision, panicValue := invokeObserverDecision(func() *queryDecision {
				return tt.observe(sink)
			})

			if panicValue != nil {
				t.Fatalf("start panic escaped observer: %v", panicValue)
			}
			requireObserverDenied(t, decision)
			if decision.ErrorCode != testObserverAuditFailure {
				t.Fatalf("error code = %q, want %q", decision.ErrorCode, testObserverAuditFailure)
			}
		})
	}
}

func TestObserverAbortContainsPermanentFinishPanic(t *testing.T) {
	tests := []struct {
		name  string
		build func(*permanentPanicSink) queryObserverLifecycle
	}{
		{
			name: "mysql",
			build: func(sink *permanentPanicSink) queryObserverLifecycle {
				observer := &mysqlObserver{sink: sink}
				requireNoDecision(t, observer.ObserveClientBytes(buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))))
				return observer
			},
		},
		{
			name: "postgres",
			build: func(sink *permanentPanicSink) queryObserverLifecycle {
				observer := &postgresObserver{sink: sink, startupDone: true}
				requireNoDecision(t, observer.ObserveClientBytes(postgresMessage('Q', append([]byte("select 1"), 0))))
				return observer
			},
		},
		{
			name: "redis",
			build: func(sink *permanentPanicSink) queryObserverLifecycle {
				observer := &redisObserver{sink: sink}
				requireNoDecision(t, observer.ObserveClientBytes(redisObserverTestCommand("GET", "key")))
				return observer
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &permanentPanicSink{}
			lifecycle := tt.build(sink)

			_, panicValue := invokeObserverVoid(func() {
				lifecycle.Abort(observerErrorRelay)
			})

			if panicValue != nil {
				t.Fatalf("abort panic escaped observer: %v", panicValue)
			}
			if got := sink.finishes(); got != 1 {
				t.Fatalf("abort finish attempts = %d, want exactly 1", got)
			}
			if lifecycle.HasPending() {
				t.Fatal("aborted observer retained active pending work")
			}
		})
	}
}

func TestObserversCommitPendingRemovalAfterFinishReturns(t *testing.T) {
	t.Run("mysql", func(t *testing.T) {
		var observer *mysqlObserver
		sink := &assertPendingSink{t: t, assert: func(t *testing.T) {
			if got := len(observer.pending); got != 1 {
				t.Fatalf("pending during FinishQuery = %d, want 1", got)
			}
		}}
		observer = &mysqlObserver{sink: sink}
		requireNoDecision(t, observer.ObserveClientBytes(buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))))
		requireNoDecision(t, observer.ObserveServerBytes(buildMySQLPacket(1, []byte{0x00, 0, 0, 0, 0, 0, 0})))
		if len(observer.pending) != 0 {
			t.Fatalf("pending after FinishQuery = %d, want 0", len(observer.pending))
		}
	})

	t.Run("postgres", func(t *testing.T) {
		var observer *postgresObserver
		sink := &assertPendingSink{t: t, assert: func(t *testing.T) {
			if got := len(observer.pending); got != 1 {
				t.Fatalf("pending during FinishQuery = %d, want 1", got)
			}
		}}
		observer = &postgresObserver{sink: sink, startupDone: true}
		requireNoDecision(t, observer.ObserveClientBytes(postgresMessage('Q', append([]byte("select 1"), 0))))
		response := append(postgresMessage('C', append([]byte("SELECT 1"), 0)), postgresReadyForQuery()...)
		requireNoDecision(t, observer.ObserveServerBytes(response))
		if len(observer.pending) != 0 {
			t.Fatalf("pending after FinishQuery = %d, want 0", len(observer.pending))
		}
	})

	t.Run("redis", func(t *testing.T) {
		var observer *redisObserver
		sink := &assertPendingSink{t: t, assert: func(t *testing.T) {
			if got := len(observer.slots); got != 1 {
				t.Fatalf("slots during FinishQuery = %d, want 1", got)
			}
		}}
		observer = &redisObserver{sink: sink}
		requireNoDecision(t, observer.ObserveClientBytes(redisObserverTestCommand("GET", "key")))
		requireNoDecision(t, observer.ObserveServerBytes([]byte("+OK\r\n")))
		if len(observer.slots) != 0 {
			t.Fatalf("slots after FinishQuery = %d, want 0", len(observer.slots))
		}
	})
}

func TestConnectionRecorderAuditPanicTerminatesObserver(t *testing.T) {
	recorder := &connectionRecorder{
		protocol:       "mysql",
		audit:          panicAuditWriter{},
		auditSessionID: "session-1",
	}
	observer := &mysqlObserver{sink: recorder}
	requireNoDecision(t, observer.ObserveClientBytes(buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))))

	decision, panicValue := invokeObserverDecision(func() *queryDecision {
		return observer.ObserveServerBytes(buildMySQLPacket(1, []byte{0x00, 0, 0, 0, 0, 0, 0}))
	})

	if panicValue != nil {
		t.Fatalf("database audit panic escaped observer: %v", panicValue)
	}
	requireObserverDenied(t, decision)
	if decision.ErrorCode != testObserverAuditFailure {
		t.Fatalf("error code = %q, want %q", decision.ErrorCode, testObserverAuditFailure)
	}
}

type assertPendingSink struct {
	t      *testing.T
	assert func(*testing.T)
}

func (s *assertPendingSink) StartQuery(sql string, detail map[string]any) (queryRecord, queryDecision) {
	return queryRecord{seq: 1, protocol: "test", sql: sql, detail: detail, startedAt: time.Now()}, allowQuery()
}

func (s *assertPendingSink) FinishQuery(queryRecord, queryFinish) {
	s.assert(s.t)
}

type panicAuditWriter struct{}

func (panicAuditWriter) CreateAuditSession(*model.AuditSession) error { return nil }
func (panicAuditWriter) EndAuditSession(string) error                 { return nil }
func (panicAuditWriter) CreateAuditDBQuery(*model.AuditDBQuery) error {
	panic("database sink secret")
}

func invokeObserverDecision(call func() *queryDecision) (decision *queryDecision, panicValue any) {
	defer func() {
		panicValue = recover()
	}()
	decision = call()
	return decision, nil
}

func invokeObserverVoid(call func()) (completed bool, panicValue any) {
	defer func() {
		panicValue = recover()
	}()
	call()
	return true, nil
}

func requireNoDecision(t *testing.T, decision *queryDecision) {
	t.Helper()
	if decision != nil {
		t.Fatalf("unexpected observer decision: %#v", decision)
	}
}
