package dbproxy

import "sync"

const defaultPendingHandshakeLimit = 256

type pendingHandshakeLimiter struct {
	slots chan struct{}
}

type pendingHandshakeLease struct {
	limiter *pendingHandshakeLimiter
	once    sync.Once
}

func newPendingHandshakeLimiter(limit int) *pendingHandshakeLimiter {
	if limit <= 0 {
		panic("pending database handshake limit must be positive")
	}
	return &pendingHandshakeLimiter{slots: make(chan struct{}, limit)}
}

func (limiter *pendingHandshakeLimiter) tryAcquire() (*pendingHandshakeLease, bool) {
	if limiter == nil {
		return &pendingHandshakeLease{}, true
	}
	select {
	case limiter.slots <- struct{}{}:
		return &pendingHandshakeLease{limiter: limiter}, true
	default:
		return nil, false
	}
}

func (lease *pendingHandshakeLease) release() {
	if lease == nil {
		return
	}
	lease.once.Do(func() {
		if lease.limiter != nil {
			<-lease.limiter.slots
		}
	})
}

func (g *Gateway) tryAcquirePendingHandshake() (*pendingHandshakeLease, bool) {
	if g == nil {
		return nil, false
	}
	return g.pendingHandshakes.tryAcquire()
}
