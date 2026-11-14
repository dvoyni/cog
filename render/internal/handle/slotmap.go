package handle

// SlotMap is the slots of one handle type H, each holding an S. It hands a
// slot out under a handle, finds the slot by that handle while it is held,
// and once the slot is released hands it out again under a handle of the
// next generation, so a released handle finds nothing. It is not
// synchronized: its owner guards it. The zero SlotMap is empty and ready to
// use.
type SlotMap[H Handle, S any] struct {
	entries []slotEntry[S]
	// freeIndices are the released entries, handed out again before new ones,
	// the last released first.
	freeIndices []uint32
}

// slotEntry is one entry of a SlotMap: the slot, the generation it was last
// handed out under, and whether it is held.
type slotEntry[S any] struct {
	slot       S
	generation uint32
	held       bool
}

// Acquire hands out a released slot, or a new one when none is, and answers
// its handle and the slot, which holds what it held when released, or the
// zero S when new. The slot stays where it is until the next Acquire.
func (m *SlotMap[H, S]) Acquire() (H, *S) {
	var index uint32
	if count := len(m.freeIndices); count > 0 {
		index = m.freeIndices[count-1]
		m.freeIndices = m.freeIndices[:count-1]
	} else {
		index = uint32(len(m.entries))
		m.entries = append(m.entries, slotEntry[S]{})
	}
	entry := &m.entries[index]
	entry.generation = max(entry.generation+1, 1)
	entry.held = true
	return H{handle{index: index, generation: entry.generation}}, &entry.slot
}

// Find answers h's slot while h holds it; false when h is released or the
// zero handle.
func (m *SlotMap[H, S]) Find(h H) (*S, bool) {
	key := struct{ handle }(h).handle
	if key.index >= uint32(len(m.entries)) {
		return nil, false
	}
	entry := &m.entries[key.index]
	if !entry.held || entry.generation != key.generation {
		return nil, false
	}
	return &entry.slot, true
}

// Release hands h's slot out again, keeping what it holds for the next
// Acquire to reuse; a released handle or the zero handle is left as it is.
func (m *SlotMap[H, S]) Release(h H) {
	if _, ok := m.Find(h); !ok {
		return
	}
	index := struct{ handle }(h).index
	m.entries[index].held = false
	m.freeIndices = append(m.freeIndices, index)
}
