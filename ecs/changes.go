package ecs

import (
	"bytes"
	"iter"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// ChangeKind is what happened to an entity's component in an observation
// window.
type ChangeKind uint8

const (
	ChangeAdded ChangeKind = iota + 1
	ChangeChanged
	ChangeRemoved
)

// Change is one entity's component added, changed or removed since the last
// observation reset. Value is the current value, or the last one for a
// removal; Previous is the value as of the reset, zero for an addition.
type Change[T any] struct {
	Kind            ChangeKind
	Value, Previous T
}

// Changes reads what changed in T's store in world W in this observation
// window: since the last observation reset, by systems that ran before it.
// Taking it registers an observer of the store, which observes from the
// world's first observation reset; until then it reports nothing. A store
// nobody observes keeps no shadow and pays nothing. It holds a read on the
// store and on the world.
type Changes[W WorldID, T any] struct {
	store *store
}

// Declare declares a read on T's store and on world W, and registers an
// observer of the store.
func (Changes[W, T]) Declare(access *kernel.Access) Changes[W, T] {
	access.Read[*world[W]]()
	s := &access.Read[*storeOf[W, T]]().Get().store
	s.observerRegistered = true
	return Changes[W, T]{store: s}
}

// All yields every change of the window, in row order, which depends only on
// what was done. A row written back to its value as of the reset is no
// change; the comparison is bytewise, so -0.0 and 0.0 differ.
func (c Changes[W, T]) All() iter.Seq2[Entity, Change[T]] {
	return func(yield func(Entity, Change[T]) bool) {
		s := c.store
		if !s.observed {
			return
		}
		size := s.component.Size
		for row := range s.owners.Len() {
			var change Change[T]
			switch *s.rowStates.At(row) {
			case rowAdded:
				change.Kind = ChangeAdded
			case rowChanged:
				if bytes.Equal(unsafe.Slice((*byte)(s.address(row)), size), unsafe.Slice((*byte)(s.shadow(row)), size)) {
					continue
				}
				change.Kind = ChangeChanged
				change.Previous = *(*T)(s.shadow(row))
			case rowRemoved:
				change.Kind = ChangeRemoved
				change.Previous = *(*T)(s.shadow(row))
			default:
				continue
			}
			change.Value = *(*T)(s.address(row))
			if !yield(*s.owners.At(row), change) {
				return
			}
		}
	}
}

// ObservationReset is the system that ends world W's observation window,
// registered with RegisterTask1(ecs.ObservationReset[Game]) and scheduled
// where the frame needs it: first, last, after the network send, after the
// renderer has read its changes. It takes the barrier. Every observed
// store's shadows take their rows' values and its removed rows are dropped;
// the window's despawned indices, and reservations never spawned, go back
// to the free list.
//
// Its first run turns observation on, and every row present then counts as
// added in the first window. A world it never runs in observes nothing and
// keeps nothing for observers: a despawned index is reused at once.
func ObservationReset[W WorldID](k *kernel.Kernel, _ kernel.NoArg, world World[W]) kernel.NoResult {
	world.world.resetObservation()
	return kernel.NoResult{}
}
