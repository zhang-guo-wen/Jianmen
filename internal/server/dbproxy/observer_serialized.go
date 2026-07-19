package dbproxy

import "sync"

type queryObserver interface {
	ObserveClientBytes(data []byte) *queryDecision
	ObserveServerBytes(data []byte) *queryDecision
	ErrorResponse(decision queryDecision) []byte
}

type relayClientQueryObserver interface {
	ObserveClientRelayBytes(data []byte) ([]byte, *queryDecision)
}

type relayServerQueryObserver interface {
	ObserveServerRelayBytes(data []byte) ([]byte, *queryDecision)
}

type queryObserverLifecycle interface {
	Abort(code string)
	HasPending() bool
}

type querySink interface {
	StartQuery(sql string, detail map[string]any) (queryRecord, queryDecision)
	FinishQuery(record queryRecord, finish queryFinish)
}

func newQueryObserver(protocol string, sink querySink) queryObserver {
	var observer queryObserver
	switch protocol {
	case "mysql":
		observer = &mysqlObserver{sink: sink}
	case "postgres":
		observer = &postgresObserver{sink: sink, startupDone: true}
	case "redis":
		observer = &redisObserver{sink: sink}
	default:
		return noopObserver{}
	}
	return serializeRelayObserver(observer)
}

type noopObserver struct{}

func (noopObserver) ObserveClientBytes(_ []byte) *queryDecision { return nil }
func (noopObserver) ObserveServerBytes(_ []byte) *queryDecision { return nil }
func (noopObserver) ErrorResponse(_ queryDecision) []byte       { return nil }

// serializedQueryObserver preserves request/response ordering when the relay's
// client and upstream goroutines observe the same protocol stream concurrently.
type serializedQueryObserver struct {
	mu       sync.Mutex
	observer queryObserver
}

func serializeRelayObserver(observer queryObserver) queryObserver {
	if _, ok := observer.(*serializedQueryObserver); ok {
		return observer
	}
	return &serializedQueryObserver{observer: observer}
}

func (o *serializedQueryObserver) ObserveClientBytes(data []byte) *queryDecision {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.observer.ObserveClientBytes(data)
}

func (o *serializedQueryObserver) ObserveClientRelayBytes(data []byte) ([]byte, *queryDecision) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if observer, ok := o.observer.(relayClientQueryObserver); ok {
		return observer.ObserveClientRelayBytes(data)
	}
	decision := o.observer.ObserveClientBytes(data)
	if decision != nil && !decision.Allowed {
		return nil, decision
	}
	return data, decision
}

func (o *serializedQueryObserver) ObserveServerBytes(data []byte) *queryDecision {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.observer.ObserveServerBytes(data)
}

func (o *serializedQueryObserver) ObserveServerRelayBytes(data []byte) ([]byte, *queryDecision) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if observer, ok := o.observer.(relayServerQueryObserver); ok {
		return observer.ObserveServerRelayBytes(data)
	}
	decision := o.observer.ObserveServerBytes(data)
	if decision != nil && !decision.Allowed {
		return nil, decision
	}
	return data, decision
}

func (o *serializedQueryObserver) ErrorResponse(decision queryDecision) []byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.observer.ErrorResponse(decision)
}

func (o *serializedQueryObserver) Abort(code string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if lifecycle, ok := o.observer.(queryObserverLifecycle); ok {
		lifecycle.Abort(code)
	}
}

func (o *serializedQueryObserver) HasPending() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if lifecycle, ok := o.observer.(queryObserverLifecycle); ok {
		return lifecycle.HasPending()
	}
	return false
}
