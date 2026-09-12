// Package ecs is the throwaway prototype behind cog#243, "The zero-allocation
// proof". It is not the ecs plugin: it is the smallest thing that runs the
// decided design on a real kernel engine so the allocator can be asked whether
// the design holds. It is deleted once its numbers are recorded.
//
// The design it embodies is the one the map already locked:
//   - cog#237 Entity is an opaque uint64; a Component is a pointer-free struct.
//   - cog#239 a Store is a flat sparse index of 8-byte slots with the generation
//     folded in, a packed dense array and an owners list. The Driver is picked
//     per run by scanning Store lengths.
//   - cog#240 a Query walks its driver backwards, removal is swap-remove, and
//     Entities holds a reference to every Store so one lock covers a Despawn.
//   - cog#238 a Query is a struct whose field pointer-ness is its access mode,
//     and a System is a plain func turned into a (Lock, Observe) pair at
//     registration.
package ecs

import "unsafe"

// Entity is the opaque handle. The index/generation split is private (cog#237);
// it is spelled out here only because the Store probe consults both halves.
type Entity uint64

func mkEntity(idx, gen uint32) Entity { return Entity(uint64(gen)<<32 | uint64(idx)) }

func (e Entity) idx() uint32 { return uint32(e) }
func (e Entity) gen() uint32 { return uint32(e >> 32) }

// absentSlot is the sparse slot of an entity this Store does not hold. The
// generation half is all-ones, which no live generation ever reaches, so the
// membership test is one load and one compare and needs no tombstone branch.
const absentSlot = uint64(0xFFFFFFFF) << 32

// storeCore is the type-erased half of a Store: everything iteration and
// Despawn need, and nothing that mentions T. Query holds *storeCore so the hot
// path costs no generic dispatch and no interface call, and Entities holds one
// per Store so a Despawn can empty them all under a single write{*Entities}.
//
// It is embedded first in Store[T], so *Store[T] and *storeCore are the same
// address and the conversion is free.
type storeCore struct {
	sparse []uint64 // entity index -> gen<<32 | dense index
	owners []Entity // dense index -> entity; its length is the Store's length
	dense  unsafe.Pointer
	stride uintptr
	size   uintptr
}

// probe reports e's dense index. The compare that finds it is the compare that
// rejects a stale handle, so liveness is not an extra cost (cog#239).
func (c *storeCore) probe(e Entity) (uint32, bool) {
	v := c.sparse[e.idx()]
	return uint32(v), uint32(v>>32) == e.gen()
}

func (c *storeCore) at(j uint32) unsafe.Pointer {
	return unsafe.Add(c.dense, uintptr(j)*c.stride)
}

// remove is swap-remove, done without knowing T: the last row's bytes are moved
// into the hole. Iteration order is therefore unspecified and no dense index
// survives a mutation (cog#239).
func (c *storeCore) remove(e Entity) bool {
	j, ok := c.probe(e)
	if !ok {
		return false
	}
	last := uint32(len(c.owners) - 1)
	if j != last {
		copyN(c.at(j), c.at(last), c.size)
		moved := c.owners[last]
		c.owners[j] = moved
		c.sparse[moved.idx()] = uint64(moved.gen())<<32 | uint64(j)
	}
	c.owners = c.owners[:last]
	c.sparse[e.idx()] = absentSlot
	return true
}

// Store is one Component type's storage, and one lock unit. It must be reached
// as *Store[T]: cog#234 proved a value store makes Write[T].Get() return a copy
// and silently discard every mutation.
type Store[T any] struct {
	storeCore
	data []T // backing array for dense; never shrinks, so a respawn reuses a row
}

// NewStore sizes the flat index for ids distinct entity indices. The index is
// flat, not paged: paging costs 93% more per probe and saves nothing once
// recycling scatters indices (cog#239).
func NewStore[T any](ids uint32) *Store[T] {
	var zero T
	s := &Store[T]{}
	s.sparse = make([]uint64, ids)
	for i := range s.sparse {
		s.sparse[i] = absentSlot
	}
	s.owners = make([]Entity, 0, ids)
	s.data = make([]T, 0, ids)
	s.stride = unsafe.Sizeof(zero)
	s.size = unsafe.Sizeof(zero)
	s.dense = unsafe.Pointer(unsafe.SliceData(s.data))
	return s
}

func (s *Store[T]) Len() int { return len(s.owners) }

// OwnerAt is for tests only: the dense order is unspecified, so nothing in a
// System may depend on it.
func (s *Store[T]) OwnerAt(j int) Entity { return s.owners[j] }

// RowAt is for tests only, like OwnerAt: it reads the dense array positionally
// so a measurement can walk the rows without going through a Query.
func (s *Store[T]) RowAt(j int) *T { return &s.data[j] }

func (s *Store[T]) Add(e Entity, v T) {
	j := len(s.owners)
	if j == len(s.data) {
		s.data = append(s.data, v)
		// append may have moved the backing array, so dense is re-derived rather
		// than assumed. Once the Store has reached its high-water mark this
		// branch is never taken again and a respawn reuses a row.
		s.dense = unsafe.Pointer(unsafe.SliceData(s.data))
	} else {
		s.data[j] = v
	}
	s.owners = append(s.owners, e)
	s.sparse[e.idx()] = uint64(e.gen())<<32 | uint64(j)
}

func (s *Store[T]) Get(e Entity) (*T, bool) {
	j, ok := s.probe(e)
	if !ok {
		return nil, false
	}
	return &s.data[j], true
}

func (s *Store[T]) Remove(e Entity) bool { return s.storeCore.remove(e) }

// copyN moves one component's bytes. The sized cases exist because cog#238
// measured the offset copy as free at these widths; the default is the fallback
// for a Component of an unusual size.
func copyN(dst, src unsafe.Pointer, n uintptr) {
	switch n {
	case 4:
		*(*[4]byte)(dst) = *(*[4]byte)(src)
	case 8:
		*(*[8]byte)(dst) = *(*[8]byte)(src)
	case 16:
		*(*[16]byte)(dst) = *(*[16]byte)(src)
	case 32:
		*(*[32]byte)(dst) = *(*[32]byte)(src)
	default:
		copy(unsafe.Slice((*byte)(dst), n), unsafe.Slice((*byte)(src), n))
	}
}
