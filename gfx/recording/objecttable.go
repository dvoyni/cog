package recording

import "fmt"

// objectTable is where the recording device keeps one type's objects, each at
// its handle's Index, as a real backend keeps its native objects. It checks
// what gfx owes a backend: no create at an index still holding an object, and
// no release of an index holding none. The instance's mutex guards it.
type objectTable[T any] struct {
	entries []objectEntry[T]
}

// store keeps object at index; an error when index already holds one.
func (table *objectTable[T]) store(index uint32, object T) error {
	if index >= uint32(len(table.entries)) {
		table.entries = append(table.entries, make([]objectEntry[T], index+1-uint32(len(table.entries)))...)
	}
	if table.entries[index].present {
		return fmt.Errorf("recording: index %d already holds an object", index)
	}
	table.entries[index] = objectEntry[T]{object: object, present: true}
	return nil
}

// remove drops the object at index; an error when index holds none.
func (table *objectTable[T]) remove(index uint32) error {
	if index >= uint32(len(table.entries)) || !table.entries[index].present {
		return fmt.Errorf("recording: index %d holds no object", index)
	}
	table.entries[index] = objectEntry[T]{}
	return nil
}

// objectEntry is one index of an objectTable: its object, if present.
type objectEntry[T any] struct {
	object  T
	present bool
}
