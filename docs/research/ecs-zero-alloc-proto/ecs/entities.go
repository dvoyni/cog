package ecs

import (
	"reflect"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// Entities is the entity-id authority and, because it holds a reference to
// every Store, the one lock that covers structural change (cog#240). Every
// handler that touches any Store declares read{*Entities}; Spawn and Despawn
// take it for write. That is what makes the generated Lock a single
// unconditional GetRead/GetWrite and so trivially straight-line.
type Entities struct {
	gens   []uint32
	freed  []uint32
	next   uint32
	stores []*storeCore

	// ops is consulted at registration only. It is the answer to the one thing
	// reflection cannot do: cog#234 proved a generic cannot be instantiated
	// from a reflect.Type, so the type-specific work is baked here by
	// RegisterComponent, which *is* generic, and looked up by type later.
	ops map[reflect.Type]componentOps
}

// componentOps is the per-Component-type work that has to be compiled rather
// than reflected. Each field is a closure created inside RegisterComponent[C],
// where C is a real type parameter.
type componentOps struct {
	// declareRead and declareWrite run inside a Lock. They declare the lock on
	// *Store[C] and return a resolver that produces the Store's type-erased
	// core, callable only while the handler holds that lock.
	declareRead  func(a kernel.ResourceAccess) func() *storeCore
	declareWrite func(a kernel.ResourceAccess) func() *storeCore
	// addRaw appends one component, read from src as a C. Used by Spawn, which
	// knows only the bundle's field offsets.
	addRaw func(e Entity, src unsafe.Pointer)
	size   uintptr
}

func NewEntities(ids uint32) *Entities {
	return &Entities{
		gens: make([]uint32, ids),
		ops:  map[reflect.Type]componentOps{},
	}
}

// RegisterComponent creates C's Store, hands it to the kernel as a resource
// owned by the declaring plugin (cog#240), and bakes the type-specific closures
// the registration-time reflection will need.
func RegisterComponent[C any](r *kernel.Registrar, en *Entities, ids uint32) *Store[C] {
	// The pointer-free rule is enforced here because registration is the only
	// moment the type is named and nothing has been stored yet, so the failure
	// lands on the plugin that declared the Component rather than on whatever
	// later tried to copy it (cog#246).
	if err := PointerFree(reflect.TypeFor[C]()); err != nil {
		panic("ecs: " + err.Error())
	}
	s := NewStore[C](ids)
	r.InitResource[*Store[C]](s)
	en.stores = append(en.stores, &s.storeCore)
	var zero C
	en.ops[reflect.TypeFor[C]()] = componentOps{
		declareRead: func(a kernel.ResourceAccess) func() *storeCore {
			h := a.GetRead[*Store[C]]()
			return func() *storeCore { return &h.Get().storeCore }
		},
		declareWrite: func(a kernel.ResourceAccess) func() *storeCore {
			h := a.GetWrite[*Store[C]]()
			return func() *storeCore { return &h.Get().storeCore }
		},
		addRaw: func(e Entity, src unsafe.Pointer) { s.Add(e, *(*C)(src)) },
		size:   unsafe.Sizeof(zero),
	}
	return s
}

// Alloc hands out an id, recycling a freed index so the flat sparse index stays
// bounded by peak concurrent entities rather than by total spawns (cog#239).
func (en *Entities) Alloc() Entity {
	if n := len(en.freed); n > 0 {
		idx := en.freed[n-1]
		en.freed = en.freed[:n-1]
		return mkEntity(idx, en.gens[idx])
	}
	idx := en.next
	en.next++
	return mkEntity(idx, en.gens[idx])
}

// Despawn empties every Store of e and retires the handle. Eager, because
// cog#240 measured eager at 10.9us against lazy's 19.5us a tick at nox's scale,
// and because it leaves no dead row anywhere: len(owners) stays exact for
// driver selection and there is no reclamation mechanism at all.
func (en *Entities) Despawn(e Entity) bool {
	idx := e.idx()
	if idx >= uint32(len(en.gens)) || en.gens[idx] != e.gen() {
		return false
	}
	for _, c := range en.stores {
		c.remove(e)
	}
	en.gens[idx]++
	en.freed = append(en.freed, idx)
	return true
}

func (en *Entities) Live() int { return int(en.next) - len(en.freed) }

// Without is a Query filter. It is a blank field in the Query struct and yields
// nothing, but it is not free: evaluating it loads Store[T]'s index while
// another System may hold write{T}, so it contributes a read (cog#239's
// correction to cog#238).
type Without[T any] struct{}

func (Without[T]) filtered() reflect.Type { return reflect.TypeFor[T]() }

type filterField interface{ filtered() reflect.Type }
