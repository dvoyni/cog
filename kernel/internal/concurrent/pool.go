package concurrent

import (
	"sync"
	"sync/atomic"
)

const (
	chunkShift = 8
	chunkSize  = 1 << chunkShift
	chunkMask  = chunkSize - 1
)

// None is the index of no slot.
const None uint32 = 0

// Pool is a growing store of T slots, addressed by index. Slots live in
// fixed-size chunks that never move, so an index, and a pointer At returns,
// stays valid for the pool's life however much it grows.
//
// Acquire and Release take the pool's mutex; AcquireMany and ReleaseMany
// take it once for a whole batch. At takes none, since it only reads chunks
// that are never changed once published. Only growing allocates. The zero
// Pool is empty and ready to use.
//
// T should hold no pointers, so the GC never scans the chunks.
type Pool[T any] struct {
	chunks atomic.Pointer[[]*[chunkSize]T]
	mutex  sync.Mutex
	// free holds the indexes released, reused most recent first while their
	// slots are still in cache.
	free []uint32
}

// NewPool returns a pool already grown to hold at least capacity slots, so
// that the first capacity Acquires allocate nothing.
func NewPool[T any](capacity int) *Pool[T] {
	p := &Pool[T]{}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	for p.capacity() < capacity {
		p.free = append(p.free, p.grow())
	}
	return p
}

// Acquire takes a free slot and returns its index. The slot holds whatever it
// held when it was released, or the zero T if it is new.
func (p *Pool[T]) Acquire() uint32 {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.take()
}

// AcquireMany is Acquire n times under one lock: it appends n slot indexes to
// into and returns it.
func (p *Pool[T]) AcquireMany(into []uint32, n int) []uint32 {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	for range n {
		into = append(into, p.take())
	}
	return into
}

// Release returns slot i, which must have come from Acquire, to the pool.
func (p *Pool[T]) Release(i uint32) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.free = append(p.free, i)
}

// ReleaseMany is Release for every index in indexes under one lock.
func (p *Pool[T]) ReleaseMany(indexes []uint32) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.free = append(p.free, indexes...)
}

// At returns the slot at i. The pointer stays valid for the pool's life.
func (p *Pool[T]) At(i uint32) *T {
	return &(*p.chunks.Load())[i>>chunkShift][i&chunkMask]
}

// take pops a free index, growing when there is none. The caller holds mutex.
func (p *Pool[T]) take() uint32 {
	n := len(p.free)
	if n == 0 {
		return p.grow()
	}
	i := p.free[n-1]
	p.free = p.free[:n-1]
	return i
}

func (p *Pool[T]) capacity() int {
	chunks := p.chunks.Load()
	if chunks == nil {
		return 0
	}
	return len(*chunks)*chunkSize - 1 // slot 0 is None
}

// grow adds one chunk, puts its slots on the free list but one, and returns
// that one. The caller holds mutex.
func (p *Pool[T]) grow() uint32 {
	var chunks []*[chunkSize]T
	if old := p.chunks.Load(); old != nil {
		chunks = *old
	}
	base := uint32(len(chunks)) << chunkShift
	// A fresh slice, not an append in place: At may be reading the old one.
	chunks = append(chunks[:len(chunks):len(chunks)], new([chunkSize]T))
	p.chunks.Store(&chunks)

	first := base
	if first == None {
		first++
	}
	// Highest first, so Acquires hand the chunk out in ascending order.
	for i := base + chunkSize - 1; i > first; i-- {
		p.free = append(p.free, i)
	}
	return first
}
