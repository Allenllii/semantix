package event

import "sync"

// Bus is the in-process event bus. Handlers are invoked synchronously in
// subscription order (FIFO within one goroutine). Emit never blocks on slow
// handlers beyond the handler itself; fan-out is sequential by design.
type Bus interface {
	Emit(Event)
	Subscribe(handler func(Event)) (unsubscribe func())
}

// SyncBus is the default Bus implementation: a mutex-guarded handler list.
// Emit snapshots the handlers under the lock and runs them outside the lock,
// so a handler may safely Subscribe/Unsubscribe without deadlocking.
type SyncBus struct {
	mu       sync.Mutex
	handlers []func(Event)
}

// NewSyncBus returns a ready-to-use SyncBus.
func NewSyncBus() *SyncBus {
	return &SyncBus{}
}

// Emit delivers e to all currently-subscribed handlers in subscription order.
func (b *SyncBus) Emit(e Event) {
	b.mu.Lock()
	snapshot := make([]func(Event), len(b.handlers))
	copy(snapshot, b.handlers)
	b.mu.Unlock()
	for _, h := range snapshot {
		if h != nil {
			h(e)
		}
	}
}

// Subscribe appends handler and returns an unsubscribe function. Calling
// unsubscribe more than once is safe (second call is a no-op).
func (b *SyncBus) Subscribe(handler func(Event)) (unsubscribe func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, handler)
	idx := len(b.handlers) - 1
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.handlers[idx] = nil
		})
	}
}
