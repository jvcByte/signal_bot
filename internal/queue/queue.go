package queue

import (
	"context"
	"sync"

	"signal-bot/pkg/models"
)

type Queue struct {
	ch       chan *models.Signal
	capacity int
	mu       sync.RWMutex
	closed   bool
}

func New(capacity int) *Queue {
	return &Queue{
		ch:       make(chan *models.Signal, capacity),
		capacity: capacity,
	}
}

func (q *Queue) Push(signal *models.Signal) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.closed {
		return ErrQueueClosed
	}

	select {
	case q.ch <- signal:
		return nil
	default:
		return ErrQueueFull
	}
}

func (q *Queue) Pop(ctx context.Context) (*models.Signal, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case signal, ok := <-q.ch:
		if !ok {
			return nil, ErrQueueClosed
		}
		return signal, nil
	}
}

func (q *Queue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.closed {
		q.closed = true
		close(q.ch)
	}
}

func (q *Queue) Len() int {
	return len(q.ch)
}

var (
	ErrQueueFull   = &QueueError{"queue is full"}
	ErrQueueClosed = &QueueError{"queue is closed"}
)

type QueueError struct {
	msg string
}

func (e *QueueError) Error() string {
	return e.msg
}
