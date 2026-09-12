package ecs

import (
	"fmt"
	"reflect"

	"github.com/dvoyni/cog/kernel"
)

// An Accessor is a System's means of reaching one Component of an Entity it did
// not iterate to. A Query reaches only what it drives over, so a missile's
// target — an Entity stored in a Component, a Reference — is followed with one
// of the three handles here.
//
// Following a Reference is not a second-class path: it is the same one-load
// probe the Driver already pays per candidate, and the random access costs what
// the Driver's own probe costs.
//
// The handle around it is not free, and the number is worth carrying because it
// is the one thing here the spec inferred rather than measured. Reaching the
// Store is a type assertion out of the kernel's any-typed resource cell, about
// 1.9 ns, and an accessor pays it per call where a Query pays it once per run
// when it binds. So a probe that is 0.68 ns reached directly is 2.63 ns reached
// through Of. It costs no allocation, it is nowhere near a Query's path, and
// the remedy if it ever matters is to resolve the Store once per tick in the
// handler builder rather than once per call — which is a change to the
// parameter seam and not to anything here.
//
// Get is the read half and has no Ref, which is what stops a read handle being
// a write in disguise: a read yields a copy because a read yielding a pointer
// would be a data race against concurrent readers and Go has no
// pointer-to-const.
//
//	func home(q *ecs.Query[HomingQ], bodies *ecs.Get[Body]) {
//	    for _, it := range q.All() {
//	        target, ok := bodies.Of(it.Homing.Target)
//	        if !ok {
//	            continue            // the target is gone, or never had a Body
//	        }
//	        it.Velocity.V = target.Pos.Sub(it.Body.Pos).Normalized()
//	    }
//	}
//
// It declares read{*Store[T]} and read{*Entities}.
type Get[T any] struct {
	store kernel.Read[*Store[T]]
}

// prepare declares the locks and binds the Store. It runs once, at registration.
func (g *Get[T]) prepare(en *Entities, access kernel.ResourceAccess) {
	_ = declareComponent[T](en, access, "Get")
	g.store = access.GetRead[*Store[T]]()
}

// Of reports e's Component, and whether e has one.
//
// A Reference to a despawned Entity simply misses, and it does so with no
// liveness check of its own: a despawn empties every Store eagerly, and the
// generation folded into the sparse slot is what makes a recycled index fail to
// match a stale handle. The compare that finds the row is the compare that
// rejects the handle, so asking "does e still have T" costs exactly one probe
// under a lock the System already holds — which is almost always the question
// wanted, where Entities.Alive asks the rarer one and needs a wider lock.
func (g *Get[T]) Of(e Entity) (T, bool) { return g.store.Get().Get(e) }

// Set is the write half of reaching another Entity: it reads the same way Get
// does, hands out a pointer to the stored value, and inserts one where the
// Entity has none.
//
// It declares write{*Store[T]} and read{*Entities}, and it is the one accessor
// that keeps the authority rather than only declaring it — see UpdateFor.
type Set[T any] struct {
	store kernel.Write[*Store[T]]
	// entities is read for one question, on one path: whether the Entity an
	// insertion names still exists. See UpdateFor for why that question has to be
	// asked here and cannot be left to the System.
	entities kernel.Read[*Entities]
}

// prepare declares the locks and binds the Store. It runs once, at registration.
func (s *Set[T]) prepare(en *Entities, access kernel.ResourceAccess) {
	s.entities = declareComponent[T](en, access, "Set")
	s.store = access.GetWrite[*Store[T]]()
}

// Of reports e's Component, and whether e has one — the same copy Get yields,
// available here because a write authorises a read.
func (s *Set[T]) Of(e Entity) (T, bool) { return s.store.Get().Get(e) }

// Ref is the stored value itself, for a caller that would otherwise read, edit
// and write back.
//
// The pointer is invalidated by a structural change to this Store, and that is
// contract rather than caution: removal is swap-remove, so a row that was the
// last one lands in the hole left by another, and an insertion that grows the
// dense array leaves the old one behind entirely. Either way a retained pointer
// addresses a slot that is no longer the Entity's. Use it and drop it; nothing
// may be held across an UpdateFor, a From, a spawn or a despawn.
func (s *Set[T]) Ref(e Entity) (*T, bool) { return s.store.Get().Ref(e) }

// UpdateFor gives e this Component, replacing the value if it already has one.
// Inserting is legal here rather than needing a handle of its own, because the
// handle already holds write{*Store[T]} and that is the whole of what adding a
// Component takes: nothing else records which Entities have what.
//
// So UpdateFor is how a Component is added and Remove is how one is taken away,
// and both are immediate. There is no command buffer, because a type-erased one
// costs an allocation per queued command.
//
// It is safe on the Entity a Query is currently visiting, and for the driver of
// that Query it is safe on any Entity: a new row is appended, and the backwards
// walk never reaches one. What it is not safe for is a pointer already taken out
// of this Store — see Ref.
//
// An insertion naming an Entity that no longer exists does nothing, and that
// refusal is load-bearing rather than defensive. A Reference kept across frames
// is safe to *read* because a despawn emptied every Store; inserting through a
// dangling one would put a row back that no later despawn can reach, because the
// despawn that would have reached it has already happened. The Store's
// population would stop being exact, the driver scan reads it anyway, and a
// one-Component Query would yield an Entity that does not exist.
//
// The System cannot make the check itself: "does this Entity still exist" is the
// authority's question, and no System is handed the authority. The accessor can,
// because it declares read{*Entities} — which is the second job that declaration
// does, beside closing the despawn traversal's invariant. It is a load and a
// compare, on the insertion path only, and a replacement pays nothing.
//
// Nothing is reported, for the same reason nothing is reported anywhere else on
// this path: every accessor already answers a dangling Reference with absence —
// Of and Ref miss, From reports false — and an insertion that does nothing is
// that same answer.
func (s *Set[T]) UpdateFor(e Entity, value T) {
	store := s.store.Get()
	if store.update(e, value) {
		return
	}
	if !s.entities.Get().Alive(e) {
		return
	}
	store.add(e, value)
}

// Remove takes a Component away from an Entity and is the inverse of
// Set.UpdateFor. It is a separate handle because a System that only strips a
// Component should not have to name a value type it never supplies.
//
// It declares write{*Store[T]} and read{*Entities}.
type Remove[T any] struct {
	store kernel.Write[*Store[T]]
}

// prepare declares the locks and binds the Store. It runs once, at registration.
func (r *Remove[T]) prepare(en *Entities, access kernel.ResourceAccess) {
	_ = declareComponent[T](en, access, "Remove")
	r.store = access.GetWrite[*Store[T]]()
}

// From takes this Component away from e and reports whether e had one. It is
// swap-remove, so it relocates whichever Entity owned the last row.
//
// Removing the Component a Query is driving on, for the Entity that Query is
// currently visiting, is safe — the backwards walk is what buys that. Doing it
// to any other Entity of that driver is undefined.
func (r *Remove[T]) From(e Entity) bool { return r.store.Get().Remove(e) }

// declareComponent is the half of every accessor's prepare that is the same for
// all three: the unconditional read of the authority, and the check that turns
// an unregistered Component into a composition failure naming the Component and
// the accessor rather than a missing resource naming a store type the user never
// wrote.
//
// Declaring the read is the point, and it is unconditional: every handler that
// touches any Store declares read{*Entities}, which is what a despawn's
// traversal of every Store rests on, and closing that in the accessor itself is
// what stops the invariant resting on the accident that a System reaching a
// Store happens also to carry a Query.
//
// The handle is returned rather than kept, and only Set keeps it. Get and Remove
// drop it, which is what makes "with no liveness check of its own" a property of
// those types rather than a promise about their code: they physically cannot
// consult the authority, so a dangling Reference can only ever be answered by
// the Store probe. Set keeps it for the one question an insertion has to ask.
func declareComponent[T any](en *Entities, access kernel.ResourceAccess, accessor string) kernel.Read[*Entities] {
	entities := access.GetRead[*Entities]()
	componentType := reflect.TypeFor[T]()
	if en.classOf(componentType) == nil {
		panic(fmt.Sprintf("ecs: %s[%s] names unregistered Component %s", accessor, componentType, componentType))
	}
	return entities
}
