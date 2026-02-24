package engine

import (
	"context"
	"errors"
	"sync"
)

var errQueueClosed = errors.New("engine queue closed")

// Queue is a bounded in-memory queue for transition tasks.
type Queue struct {
	mu     sync.RWMutex
	closed bool
	ch     chan queuedTask
}

func newQueue(capacity int) *Queue {
	if capacity <= 0 {
		capacity = 1
	}
	return &Queue{ch: make(chan queuedTask, capacity)}
}

func (q *Queue) enqueue(ctx context.Context, item queuedTask) error {
	ch, closed := q.channel()
	if closed {
		return errQueueClosed
	}

	select {
	case ch <- item:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *Queue) dequeue(ctx context.Context) (queuedTask, error) {
	ch, _ := q.channel()

	select {
	case item, ok := <-ch:
		if !ok {
			return queuedTask{}, errQueueClosed
		}
		return item, nil
	case <-ctx.Done():
		return queuedTask{}, ctx.Err()
	}
}

func (q *Queue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return
	}
	q.closed = true
	close(q.ch)
}

func (q *Queue) channel() (chan queuedTask, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.ch, q.closed
}
