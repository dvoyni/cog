package ecs

// absentSlot is the sparse slot of an entity a Store holds nothing for. Its
// generation half is all-ones, which no live generation reaches, so membership
// is one load and one compare with no tombstone branch to predict.
const absentSlot = uint64(absentGeneration) << 32

// storeCore is the type-erased half of a Store, and it carries exactly one
// method on purpose. A despawn has to empty every Store while naming no
// Component type, and that is the whole of what type erasure is for here: the
// 9% an interface call costs is paid once per Store per despawn, never per
// entity. Everything a Query needs — the population, the owners, the rows —
// it reaches through the typed *Store[T] instead.
type storeCore interface {
	remove(e Entity) bool
}

// Store is the holding of every value of one Component type, one per registered
// type, and the unit a lock is taken on. It is three arrays:
//
//	sparse []uint64   // entity index -> generation<<32 | dense index
//	owners []Entity   // dense index  -> the full entity id
//	dense  []T        // packed component data
//
// len(owners) is the population. There is no separate count, because removal is
// swap-remove and the packed arrays therefore never contain holes.
//
// A Store must be reached as *Store[T]. A value store makes a kernel write
// handle's Get return a copy, so mutations through it are silently discarded.
type Store[T any] struct {
	sparse []uint64
	owners []Entity
	dense  []T
}

// NewStore creates a Store and enrols it with the authority, which is what lets
// a despawn empty it; a Store the authority cannot reach would keep rows for
// entities that no longer exist. ids is the peak population the app expects,
// reserving the three arrays so the fill costs no further allocation — it is a
// hint and not a cap, and nothing here ever shrinks.
//
// Component registration is the sanctioned caller. It is what checks the
// pointer-free rule and hands the Store to the kernel as a resource.
func NewStore[T any](en *Entities, ids uint32) *Store[T] {
	if en == nil {
		panic("ecs: NewStore needs the Entities the Store belongs to, so a despawn can empty it")
	}
	s := &Store[T]{
		sparse: make([]uint64, ids),
		owners: make([]Entity, 0, ids),
		dense:  make([]T, 0, ids),
	}
	for i := range s.sparse {
		s.sparse[i] = absentSlot
	}
	en.enrol(s)
	return s
}

// Len is the population: how many entities this Store holds a value for. It is
// what a Query consults to choose its driver, and it is exact because removal
// leaves no dead row behind.
func (s *Store[T]) Len() int { return len(s.owners) }

// Has reports whether e has this Component. It is the probe and nothing else:
// one load of the sparse slot, one compare of the generation half. A stale
// handle fails it for the same reason an absent one does.
func (s *Store[T]) Has(e Entity) bool {
	_, ok := s.probe(e)
	return ok
}

// Get returns a copy of e's value. A read yields a copy because a read yielding
// a pointer would be a data race against concurrent readers, and Go has no
// pointer-to-const.
func (s *Store[T]) Get(e Entity) (T, bool) {
	row, ok := s.probe(e)
	if !ok {
		var zero T
		return zero, false
	}
	return s.dense[row], true
}

// Ref returns a pointer to e's stored value, for a caller holding the write
// lock. The pointer is valid only until this Store next changes structurally:
// swap-remove relocates rows, so a retained pointer addresses whatever took the
// slot.
func (s *Store[T]) Ref(e Entity) (*T, bool) {
	row, ok := s.probe(e)
	if !ok {
		return nil, false
	}
	return &s.dense[row], true
}

// Set gives e this Component, replacing the value if it already has one. A new
// row is appended, so nothing already in the Store moves.
func (s *Store[T]) Set(e Entity, value T) {
	if row, ok := s.probe(e); ok {
		s.dense[row] = value
		return
	}
	index := int(e.idx())
	for len(s.sparse) <= index {
		s.sparse = append(s.sparse, absentSlot)
	}
	row := len(s.owners)
	s.owners = append(s.owners, e)
	s.dense = append(s.dense, value)
	s.sparse[index] = uint64(e.gen())<<32 | uint64(row)
}

// Remove takes this Component away from e and reports whether it had one.
func (s *Store[T]) Remove(e Entity) bool { return s.remove(e) }

// probe reports the dense row e's value is in. The compare that finds the row
// is the compare that rejects a stale handle, so liveness is not an extra cost;
// it is the probe. owners is not touched at all — a probed Store reads two
// arrays, not three.
func (s *Store[T]) probe(e Entity) (uint32, bool) {
	index := e.idx()
	if int(index) >= len(s.sparse) {
		return 0, false
	}
	slot := s.sparse[index]
	return uint32(slot), uint32(slot>>32) == e.gen()
}

// remove is swap-remove: the last row moves into the hole. That is what keeps
// the packed arrays dense and len(owners) exact, and it is why no dense index
// may be held across a mutation and why iteration order is unspecified.
func (s *Store[T]) remove(e Entity) bool {
	row, ok := s.probe(e)
	if !ok {
		return false
	}
	last := uint32(len(s.owners) - 1)
	if row != last {
		moved := s.owners[last]
		s.owners[row] = moved
		s.dense[row] = s.dense[last]
		s.sparse[moved.idx()] = uint64(moved.gen())<<32 | uint64(row)
	}
	// The arrays are re-sliced, never handed back: a later Set reuses the row.
	// The vacated row keeps a copy of the value until then, which costs nothing
	// because a Component holds no pointer for it to keep alive.
	s.owners = s.owners[:last]
	s.dense = s.dense[:last]
	s.sparse[e.idx()] = absentSlot
	return true
}
