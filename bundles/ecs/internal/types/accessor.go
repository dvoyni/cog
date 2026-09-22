package types

import (
	"fmt"
	"reflect"

	"github.com/dvoyni/cog/kernel"
)

// An Accessor is a System's means of reaching one Component of an Entity it did
// not iterate to. A Query reaches only what it drives over, so a missile's
// target — an Entity stored in a Component, a Reference — is followed with one
// of the three handles: Get and Remove here, Set in set.go.
//
// Following a Reference is not a second-class path: it is the same one-load
// probe the Driver already pays per candidate, and the random access costs what
// the Driver's own probe costs.
//
// Reaching the Store is a type assertion out of the kernel's any-typed resource
// cell, and the accessor pays it once per invocation of its System rather than
// once per call: resolve reads the Store into a plain field just before the
// System's func runs, which is the resolver seam in systemcall.go, and Of uses
// that field. Measured interleaved, a probe that is 0.75 ns reached directly was
// 2.83 ns through Of when the assertion was per call and is 2.01 ns now, and the
// homing frame's per-Entity slope fell from 5.18 ns to 4.46 against a
// hand-written 2.11. It costs no allocation and is nowhere near a Query's path.
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
	// resolved is store's value for the current invocation. See resolver.
	resolved *Store[T]
}

// prepare declares the locks and binds the Store. It runs once, at registration.
func (g *Get[T]) prepare(en *Entities, access kernel.ResourceAccess) {
	_ = declareComponent[T](en, access, "Get")
	g.store = access.GetRead[*Store[T]]()
}

// resolve reads the Store out of its cell for this invocation.
func (g *Get[T]) resolve() { g.resolved = g.store.Get() }

// Of reports e's Component, and whether e has one.
//
// A Reference to a despawned Entity simply misses, and it does so with no
// liveness check of its own: a despawn empties every Store eagerly, and the
// generation folded into the sparse slot is what makes a recycled index fail to
// match a stale handle. The compare that finds the row is the compare that
// rejects the handle, so asking "does e still have T" costs exactly one probe
// under a lock the System already holds — which is almost always the question
// wanted, where Entities.Alive asks the rarer one and needs a wider lock.
func (g *Get[T]) Of(e Entity) (T, bool) {
	store := g.resolved
	if validate {
		store.stampFor(e, modeRead)
	}
	return store.Get(e)
}

// Remove takes a Component away from an Entity and is the inverse of
// Set.UpdateFor. It is a separate handle because a System that only strips a
// Component should not have to name a value type it never supplies.
//
// It declares write{*Store[T]} and read{*Entities}.
type Remove[T any] struct {
	store kernel.Write[*Store[T]]
	// resolved is store's value for the current invocation. See resolver.
	resolved *Store[T]
	// hooks is whether this run's removals are recorded: on when a Hooks reader
	// watches the Store for any kind but Despawned.
	hooks hookGate
}

// prepare declares the locks and binds the Store. It runs once, at registration.
func (r *Remove[T]) prepare(en *Entities, access kernel.ResourceAccess) {
	_ = declareComponent[T](en, access, "Remove")
	r.store = access.GetWrite[*Store[T]]()
	r.hooks = gateFor[T](en, recordsRemoval)
}

func (r *Remove[T]) gate() *hookGate { return &r.hooks }

// resolve reads the Store out of its cell for this invocation.
func (r *Remove[T]) resolve() { r.resolved = r.store.Get() }

func (r *Remove[T]) removes(en *Entities) *storeHeader {
	return en.classOf(reflect.TypeFor[T]()).header
}

// From takes this Component away from e and reports whether e had one. It is
// swap-remove, so it relocates whichever Entity owned the last row.
//
// Removing the Component a Query is driving on, for the Entity that Query is
// currently visiting, is safe — the backwards walk is what buys that. Doing it
// to any other Entity of that driver is undefined.
//
// On a Store a Hooks reader watches, the removal is recorded with T's value as
// it stood.
func (r *Remove[T]) From(e Entity) bool {
	store := r.resolved
	if validate && len(store.lists) > 0 {
		releaseLists(store.erase(), e)
	}
	if r.hooks.on {
		return store.removeRecorded(e)
	}
	return store.remove(e)
}

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
		panic(fmt.Sprintf("ecs: %s[%s] names unregistered Component %s", accessor, kernel.TypeName(componentType), kernel.TypeName(componentType)))
	}
	return entities
}

// gateFor is a writer handle's check of T's watched kinds, bound to the Store
// at registration and armed on its System's first run. declareComponent has
// already refused an unregistered T.
func gateFor[T any](en *Entities, mask hookKind) hookGate {
	store := en.classOf(reflect.TypeFor[T]()).store.(*Store[T])
	return hookGate{watch: &store.watch, mask: mask}
}
