package dbproxy

import (
	"sync"
	"sync/atomic"
)

const defaultObserverGlobalMemoryBudgetBytes = 64 * 1024 * 1024

type observerMemoryBudget struct {
	limit int64
	used  atomic.Int64
}

func newObserverMemoryBudget(limit int64) *observerMemoryBudget {
	if limit <= 0 {
		limit = defaultObserverGlobalMemoryBudgetBytes
	}
	return &observerMemoryBudget{limit: limit}
}

func (b *observerMemoryBudget) tryAcquire(size int64) *observerMemoryLease {
	if b == nil || size <= 0 {
		return nil
	}
	for {
		used := b.used.Load()
		if size > b.limit-used {
			return nil
		}
		if b.used.CompareAndSwap(used, used+size) {
			return &observerMemoryLease{budget: b, size: size}
		}
	}
}

func (b *observerMemoryBudget) inUse() int64 {
	if b == nil {
		return 0
	}
	return b.used.Load()
}

type observerMemoryLease struct {
	budget *observerMemoryBudget
	size   int64
	once   sync.Once
}

func (l *observerMemoryLease) release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		if l.budget != nil && l.size > 0 {
			l.budget.used.Add(-l.size)
		}
	})
}
