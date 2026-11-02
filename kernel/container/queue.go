package container

import "sync"

// Queue is a FIFO of T, safe from any goroutine. Pop takes the oldest; Drain
// takes them all, oldest first.
//
// It can also be closed: Close takes what is queued and refuses every push
// from then on. That is how a job's successor list tells a late dependent that
// the job has already finished.
//
// Its buffer is a ring that only grows, so once it has held its peak a Queue
// allocates nothing. A slot is cleared as its item is taken, so the ring
// keeps nothing a pointer in T refers to alive. The zero Queue is empty and
// open.
type Queue[T any] struct {
	mutex  sync.Mutex
	ring   []T
	head   int // index in ring of the oldest
	length int
	closed bool
}

// Push appends item and reports whether it did. It fails only once the queue
// is closed.
func (q *Queue[T]) Push(item T) bool {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if q.closed {
		return false
	}
	if q.length == len(q.ring) {
		q.grow()
	}
	q.ring[(q.head+q.length)%len(q.ring)] = item
	q.length++
	return true
}

// Pop removes and returns the oldest item, or false if the queue is empty.
func (q *Queue[T]) Pop() (T, bool) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if q.length == 0 {
		var zero T
		return zero, false
	}
	item := q.ring[q.head]
	var zero T
	q.ring[q.head] = zero
	q.head = (q.head + 1) % len(q.ring)
	q.length--
	return item, true
}

// Drain appends every queued item to into, oldest first, empties the queue
// and returns into. The caller keeps into between calls, so draining allocates
// nothing once into has grown.
func (q *Queue[T]) Drain(into []T) []T {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	return q.take(into)
}

// Close is Drain that also refuses every push from then on. Closing a closed
// queue appends nothing.
func (q *Queue[T]) Close(into []T) []T {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.closed = true
	return q.take(into)
}

// Len answers how many items are queued. It may change as soon as it is read,
// so it is a hint: a caller that has just pushed, or is about to drain,
// relies on it only to decide whether to look again.
func (q *Queue[T]) Len() int {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	return q.length
}

// Closed reports whether the queue is closed.
func (q *Queue[T]) Closed() bool {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	return q.closed
}

// Reset empties the queue and reopens it, keeping its buffer, for a slot
// being reused.
func (q *Queue[T]) Reset() {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.head = 0
	q.length = 0
	q.closed = false
}

// take moves everything queued onto into, clearing the slots it took. The
// caller holds mutex.
func (q *Queue[T]) take(into []T) []T {
	if q.length == 0 {
		return into
	}
	end := q.head + q.length
	if end <= len(q.ring) {
		into = append(into, q.ring[q.head:end]...)
		clear(q.ring[q.head:end])
	} else {
		into = append(into, q.ring[q.head:]...)
		into = append(into, q.ring[:end-len(q.ring)]...)
		clear(q.ring[q.head:])
		clear(q.ring[:end-len(q.ring)])
	}
	q.head = 0
	q.length = 0
	return into
}

// grow doubles the ring, unwrapping it so the oldest is first. The caller
// holds mutex.
func (q *Queue[T]) grow() {
	length := q.length
	ring := q.take(make([]T, 0, max(2*len(q.ring), 16)))
	q.ring = ring[:cap(ring)]
	q.head = 0
	q.length = length
}
