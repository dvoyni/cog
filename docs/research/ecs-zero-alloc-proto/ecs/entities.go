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

	// conv holds the Bundle-field conversions RegisterConversion baked, keyed by
	// the type written in the Bundle rather than by the Component it becomes.
	conv map[reflect.Type]conversion

	// side holds the per-Entity stores that are not Components, so that a
	// Despawn empties them too.
	side []SideStore
}

// SideStore is per-Entity data that cannot be a Component because it is not
// pointer-free -- a byte buffer, a decoded mesh, anything variable-length.
//
// It is the answer to the one thing a hash cannot do. Hashing a name is free to
// clean up because nothing accumulates: the consumer's table holds a fixed
// manifest. Hashing *runtime data* is not, because the table would then have to
// hold the data itself, would grow with every distinct value the game ever
// made, and could only be emptied by counting who still refers to each entry.
//
// The way out is to stop content-addressing it. Keyed by Entity there is
// exactly one owner, so there is nothing to count: the buffer dies with its
// Entity, under the Despawn that already empties every Store (cog#240).
// Sharing is what costs; ownership is free.
type SideStore interface {
	// Remove drops e's row, and reports whether there was one.
	Remove(e Entity) bool
}

// RegisterSideStore enrols a store in Despawn. Call it at registration, beside
// the Components the plugin declares.
//
// The store stays the plugin's own: the ECS never reads it, does not know its
// element type, and imposes nothing on it except that a Despawn empties it.
// Its lock is the plugin's own resource lock, declared by the Systems that
// touch it like any other.
func (en *Entities) RegisterSideStore(s SideStore) { en.side = append(en.side, s) }

// conversion turns one Bundle field into one Component at Spawn time. It exists
// because a Bundle is *not* a Component set -- cog#237 defines it as describing
// one act of creation -- so a Bundle field is free to be a string even though
// the Component it becomes may not be. That is what keeps spawning declarative:
// the author writes the model's name, and the pointer-free handle is derived
// where the lock is already held.
type conversion struct {
	to reflect.Type
	// apply reads the Bundle field at src and returns a pointer to the
	// Component it produced. The result points into a cell the closure owns,
	// allocated once at registration, so a spawn allocates nothing.
	apply func(src unsafe.Pointer) unsafe.Pointer
}

// RegisterConversion declares that a Bundle field of type From stands for the
// Component To, and how to get from one to the other. The convert func runs
// once per spawned entity, inside the Spawn that already holds write{To}.
//
// It is the escape from the one real ergonomic cost of the pointer-free rule.
// Without it, naming a model in a Bundle means interning the path somewhere
// else and threading the id to every spawn site; with it, the Bundle still
// reads as the declarative thing it was.
func RegisterConversion[From any, To any](en *Entities, convert func(From) To) {
	from := reflect.TypeFor[From]()
	if _, taken := en.ops[from]; taken {
		panic("ecs: " + from.String() + " is a registered Component, so it cannot also be a conversion source")
	}
	// cell is allocated once, here, and reused by every spawn. Taking its
	// address inside apply would heap-allocate per call -- the same hazard
	// cog#243 found in Spawn.New's bundle staging.
	cell := new(To)
	en.conv[from] = conversion{
		to: reflect.TypeFor[To](),
		apply: func(src unsafe.Pointer) unsafe.Pointer {
			*cell = convert(*(*From)(src))
			return unsafe.Pointer(cell)
		},
	}
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
		conv: map[reflect.Type]conversion{},
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
	// Side stores are emptied by the same Despawn and under the same lock. That
	// is what makes per-entity variable-length data cleanable at all: it is
	// owned by one Entity, so it dies when that Entity does, and nothing has to
	// count references to decide.
	for _, s := range en.side {
		s.Remove(e)
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
