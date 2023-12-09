package unbounded

import (
	"sync"
)

// Type Channel implements an unbounded channel
type Channel[T any] struct {
	// Ch triggers whenever the channel becomes non-empty
	Ch chan struct{}

	mu    sync.Mutex
	queue []T
}

// New creates a new unbounded channel
func New[T any]() *Channel[T] {
	return &Channel[T]{
		Ch: make(chan struct{}, 1),
	}
}

// Put inserts a new element into ch.
// If ch was previously empty, it triggers ch.Ch.
func (ch *Channel[T]) Put(v T) {
	ch.mu.Lock()
	empty := len(ch.queue) == 0
	ch.queue = append(ch.queue, v)
	ch.mu.Unlock()

	if empty {
		select {
		case ch.Ch <- struct{}{}:
		default:
		}
	}
}

// Get removes all the elements of ch.
// It is usually called when ch.Ch triggers, but may be called at any time.
func (ch *Channel[T]) Get() []T {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	queue := ch.queue
	ch.queue = nil
	return queue
}
