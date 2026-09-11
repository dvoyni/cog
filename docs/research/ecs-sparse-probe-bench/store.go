// Package sbench measures what a sparse-set probe costs, in the four shapes
// cog#239 has to choose between, plus the driver-selection, tag-representation
// and growth questions that ticket also asks.
//
// An Entity is an opaque uint64: index in the low 32 bits, generation in the
// high 32. That split is private to the ECS (cog#237); it is spelled out here
// only because the probe shapes differ in how much of it they consult.
package sbench

type Entity uint64

func mkEntity(idx, gen uint32) Entity { return Entity(uint64(gen)<<32 | uint64(idx)) }

func (e Entity) idx() uint32 { return uint32(e) }
func (e Entity) gen() uint32 { return uint32(e >> 32) }

type Body struct{ X, Y, VX, VY float64 }

type Collider struct {
	R    float64
	Mask uint64
}

// ---------------------------------------------------------------------------
// A: flat index, two loads. The probe as cog#239's body states it: sparse[e]
// for the dense index, then owners[d] == e to reject a stale handle.
// ---------------------------------------------------------------------------

type storeA[T any] struct {
	sparse []int32 // entity index -> dense index, -1 absent
	owners []Entity
	dense  []T
}

func newA[T any](ids uint32) *storeA[T] {
	s := &storeA[T]{sparse: make([]int32, ids)}
	for i := range s.sparse {
		s.sparse[i] = -1
	}
	return s
}

func (s *storeA[T]) add(e Entity, v T) {
	s.sparse[e.idx()] = int32(len(s.dense))
	s.owners = append(s.owners, e)
	s.dense = append(s.dense, v)
}

func (s *storeA[T]) get(e Entity) (*T, bool) {
	d := s.sparse[e.idx()]
	if d < 0 || s.owners[d] != e {
		return nil, false
	}
	return &s.dense[d], true
}

// ---------------------------------------------------------------------------
// B: flat index, one load, 8 bytes per slot. skypjack's ECS back and forth
// part 13: fold the generation into the sparse slot, so membership is one load
// and one compare, with no trip through owners and no tombstone branch.
// ---------------------------------------------------------------------------

const absentGen64 = uint64(0xFFFFFFFF) << 32

type storeB[T any] struct {
	sparse []uint64 // gen<<32 | denseIndex
	owners []Entity
	dense  []T
}

func newB[T any](ids uint32) *storeB[T] {
	s := &storeB[T]{sparse: make([]uint64, ids)}
	for i := range s.sparse {
		s.sparse[i] = absentGen64
	}
	return s
}

func (s *storeB[T]) add(e Entity, v T) {
	s.sparse[e.idx()] = uint64(e.gen())<<32 | uint64(len(s.dense))
	s.owners = append(s.owners, e)
	s.dense = append(s.dense, v)
}

func (s *storeB[T]) get(e Entity) (*T, bool) {
	v := s.sparse[e.idx()]
	if uint32(v>>32) != e.gen() {
		return nil, false
	}
	return &s.dense[uint32(v)], true
}

// ---------------------------------------------------------------------------
// C: flat index, one load, 4 bytes per slot. Same trick as B, packed into a
// uint32: 8 generation bits over 24 dense-index bits, so the index array costs
// exactly what A's costs. 0xFF is the absent generation, so a live generation
// cycles 0..254.
// ---------------------------------------------------------------------------

const (
	genShift32 = 24
	idxMask32  = uint32(1)<<genShift32 - 1
	absentGen  = uint32(0xFF)
)

type storeC[T any] struct {
	sparse []uint32
	owners []Entity
	dense  []T
}

func newC[T any](ids uint32) *storeC[T] {
	s := &storeC[T]{sparse: make([]uint32, ids)}
	for i := range s.sparse {
		s.sparse[i] = absentGen << genShift32
	}
	return s
}

func (s *storeC[T]) add(e Entity, v T) {
	s.sparse[e.idx()] = (e.gen()&0xFF)<<genShift32 | uint32(len(s.dense))
	s.owners = append(s.owners, e)
	s.dense = append(s.dense, v)
}

func (s *storeC[T]) get(e Entity) (*T, bool) {
	v := s.sparse[e.idx()]
	if v>>genShift32 != e.gen()&0xFF {
		return nil, false
	}
	return &s.dense[v&idxMask32], true
}

// ---------------------------------------------------------------------------
// D: paged index, one load plus one indirection. C's slot, in pages allocated
// on demand. EnTT pages for exactly this reason; the documented cost is the
// indirection to reach the page.
// ---------------------------------------------------------------------------

const (
	pageShift = 12
	pageSize  = 1 << pageShift
	pageMask  = pageSize - 1
)

type storeD[T any] struct {
	pages  [][]uint32
	owners []Entity
	dense  []T
}

func newD[T any](ids uint32) *storeD[T] {
	return &storeD[T]{pages: make([][]uint32, (ids+pageMask)>>pageShift)}
}

func (s *storeD[T]) add(e Entity, v T) {
	i := e.idx()
	p := s.pages[i>>pageShift]
	if p == nil {
		p = make([]uint32, pageSize)
		for k := range p {
			p[k] = absentGen << genShift32
		}
		s.pages[i>>pageShift] = p
	}
	p[i&pageMask] = (e.gen()&0xFF)<<genShift32 | uint32(len(s.dense))
	s.owners = append(s.owners, e)
	s.dense = append(s.dense, v)
}

func (s *storeD[T]) get(e Entity) (*T, bool) {
	i := e.idx()
	p := s.pages[i>>pageShift]
	if p == nil {
		return nil, false
	}
	v := p[i&pageMask]
	if v>>genShift32 != e.gen()&0xFF {
		return nil, false
	}
	return &s.dense[v&idxMask32], true
}

// pagesResident reports how many pages the store actually allocated, which is
// the whole question paging is meant to answer.
func (s *storeD[T]) pagesResident() int {
	n := 0
	for _, p := range s.pages {
		if p != nil {
			n++
		}
	}
	return n
}
