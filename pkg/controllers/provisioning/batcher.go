package provisioning

import (
	"context"
	"sync"
	"time"
)

// DefaultBatchIdleDuration is how long the batcher waits for new
// triggers before declaring the batch complete.
const DefaultBatchIdleDuration = 1 * time.Second

// DefaultBatchMaxDuration caps the total time a batch may grow even if
// triggers keep arriving — prevents indefinite starvation.
const DefaultBatchMaxDuration = 10 * time.Second

// Batcher accumulates Trigger events. Wait blocks until the idle window
// has elapsed since the last trigger or the max window has elapsed
// total. Returns true if any triggers were observed.
type Batcher[T comparable] struct {
	mu       sync.Mutex
	pending  map[T]struct{}
	triggers chan struct{}
	idle     time.Duration
	max      time.Duration
}

// NewBatcher constructs a Batcher with the given windows.
func NewBatcher[T comparable](idle, max time.Duration) *Batcher[T] {
	return &Batcher[T]{
		pending:  map[T]struct{}{},
		triggers: make(chan struct{}, 1),
		idle:     idle,
		max:      max,
	}
}

// Trigger marks `t` as pending and wakes any sleeping Wait.
func (b *Batcher[T]) Trigger(t T) {
	b.mu.Lock()
	b.pending[t] = struct{}{}
	b.mu.Unlock()
	select {
	case b.triggers <- struct{}{}:
	default:
	}
}

// Wait blocks until idle/max elapses after at least one trigger has
// arrived. Returns true if the wait completed naturally, false if ctx
// was canceled before any trigger.
func (b *Batcher[T]) Wait(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-b.triggers:
	}
	deadline := time.NewTimer(b.max)
	idleTimer := time.NewTimer(b.idle)
	defer deadline.Stop()
	defer idleTimer.Stop()
	for {
		select {
		case <-ctx.Done():
			return true
		case <-deadline.C:
			return true
		case <-idleTimer.C:
			return true
		case <-b.triggers:
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(b.idle)
		}
	}
}

// Drain returns the deduped pending set and clears it.
func (b *Batcher[T]) Drain() []T {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]T, 0, len(b.pending))
	for k := range b.pending {
		out = append(out, k)
	}
	b.pending = map[T]struct{}{}
	return out
}
