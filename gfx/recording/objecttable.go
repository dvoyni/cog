package recording

import "fmt"

// objectTable is where the recording device keeps one type's objects, each at
// its handle's Index, as a real backend keeps its native objects. It checks
// what gfx owes a backend: no create at an index still holding an object, and
// no release or use of an index holding none. The instance's mutex guards it.
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

// object answers the object at index; an error when index holds none.
func (table *objectTable[T]) object(index uint32) (T, error) {
	if index >= uint32(len(table.entries)) || !table.entries[index].present {
		var zero T
		return zero, fmt.Errorf("recording: index %d holds no object", index)
	}
	return table.entries[index].object, nil
}

// remove drops the object at index; an error when index holds none.
func (table *objectTable[T]) remove(index uint32) error {
	if _, err := table.object(index); err != nil {
		return err
	}
	table.entries[index] = objectEntry[T]{}
	return nil
}

// requireObject answers the object handle names in table, the zero object
// for the zero handle, which names none; an error, for role, when handle's
// index holds none.
func requireObject[H interface {
	comparable
	Index() uint32
}, T any](table *objectTable[T], handle H, role string) (T, error) {
	var none H
	if handle == none {
		var zero T
		return zero, nil
	}
	object, err := table.object(handle.Index())
	if err != nil {
		return object, fmt.Errorf("recording: %s: %w", role, err)
	}
	return object, nil
}

// objectEntry is one index of an objectTable: its object, if present.
type objectEntry[T any] struct {
	object  T
	present bool
}
