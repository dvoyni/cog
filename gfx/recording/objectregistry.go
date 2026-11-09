package recording

import (
	"fmt"

	"github.com/dvoyni/cog/kernel/container"
)

// objectRegistry is where the recording device keeps one type's objects,
// each at its handle's Index, as a real backend keeps its native objects. It
// checks what gfx owes a backend: no create at an index still holding an
// object, and no release or use of an index holding none. The instance's
// mutex guards it.
type objectRegistry[T any] struct {
	entries container.ObjectTable[objectEntry[T]]
}

// store keeps object at index; an error when index already holds one.
func (registry *objectRegistry[T]) store(index uint32, object T) error {
	entry := registry.entries.Grow(index)
	if entry.present {
		return fmt.Errorf("recording: index %d already holds an object", index)
	}
	*entry = objectEntry[T]{object: object, present: true}
	return nil
}

// object answers the object at index; an error when index holds none.
func (registry *objectRegistry[T]) object(index uint32) (T, error) {
	entry := registry.entries.At(index)
	if entry == nil || !entry.present {
		var zero T
		return zero, fmt.Errorf("recording: index %d holds no object", index)
	}
	return entry.object, nil
}

// remove drops the object at index; an error when index holds none.
func (registry *objectRegistry[T]) remove(index uint32) error {
	if _, err := registry.object(index); err != nil {
		return err
	}
	*registry.entries.At(index) = objectEntry[T]{}
	return nil
}

// requireObject answers the object handle names in registry, the zero object
// for the zero handle, which names none; an error, for role, when handle's
// index holds none.
func requireObject[H interface {
	comparable
	Index() uint32
}, T any](registry *objectRegistry[T], handle H, role string) (T, error) {
	var none H
	if handle == none {
		var zero T
		return zero, nil
	}
	object, err := registry.object(handle.Index())
	if err != nil {
		return object, fmt.Errorf("recording: %s: %w", role, err)
	}
	return object, nil
}

// objectEntry is one index of an objectRegistry: its object, if present.
type objectEntry[T any] struct {
	object  T
	present bool
}
