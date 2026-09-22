package types

import (
	"reflect"

	"github.com/dvoyni/cog/kernel"
)

// The write half of the three accessors. What all three share — the shape, the
// cost of reaching a Store through one, and why the read half has no Ref — is in
// accessor.go.

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
	// resolved and alive are store's and entities' values for the current
	// invocation. See resolver.
	resolved *Store[T]
	alive    *Entities
	// hooks is whether this run's additions are recorded: on when a Hooks
	// reader watches the Store for additions or changes.
	hooks hookGate
	// changes is the System's copy of the rows Ref and a replacing UpdateFor
	// hand it, compared at its run end when a Hooks reader watches the Store for
	// Changed. See rowcopy.go.
	changes *rowCopy
}

// prepare declares the locks and binds the Store. It runs once, at registration.
func (s *Set[T]) prepare(en *Entities, access kernel.ResourceAccess) {
	s.entities = declareComponent[T](en, access, "Set")
	s.store = access.GetWrite[*Store[T]]()
	s.hooks = gateFor[T](en, recordsAddition)
	s.changes = newRowCopy(en.classOf(reflect.TypeFor[T]()))
}

func (s *Set[T]) gate() *hookGate { return &s.hooks }

// resolve reads the Store and the authority out of their cells for this
// invocation.
func (s *Set[T]) resolve() { s.resolved, s.alive = s.store.Get(), s.entities.Get() }

func (s *Set[T]) rowCopies(each func(slot **rowCopy)) { each(&s.changes) }

// Of reports e's Component, and whether e has one — the same copy Get yields,
// available here because a write authorises a read.
//
// It is a read route and not a write route. The copy shares the stored List
// backing arrays but not the row, so a List.Set through it would never reach
// the stored Component's bytes; validation mode panics on one and names Ref,
// which is the handle to write through.
func (s *Set[T]) Of(e Entity) (T, bool) {
	store := s.resolved
	if validate {
		store.stampFor(e, modeSetOf)
	}
	return store.Get(e)
}

// Ref is the stored value itself, for a caller that would otherwise read, edit
// and write back.
//
// The pointer is invalidated by a structural change to this Store, and that is
// contract rather than caution: removal is swap-remove, so a row that was the
// last one lands in the hole left by another, and an insertion that grows the
// dense array leaves the old one behind entirely. Either way a retained pointer
// addresses a slot that is no longer the Entity's. Use it and drop it; nothing
// may be held across an UpdateFor, a From, a spawn or a despawn.
//
// On a Store watched for Changed, the row is copied before the pointer is handed
// out, once per run, and compared when the System's run ends.
func (s *Set[T]) Ref(e Entity) (*T, bool) {
	store := s.resolved
	if validate {
		store.stampFor(e, modeWrite)
	}
	row, ok := store.probe(e)
	if !ok {
		return nil, false
	}
	if s.changes.gate.on {
		s.changes.take(e, row)
	}
	return &store.dense[row], true
}

// MarkChanged records a Changed for e at this System's run end, whether or not
// e's bytes differ then. It is for the write a byte compare cannot see: a Set on
// a List nested in another List's element, which goes through At's copy and
// leaves the stored row's bytes as they were.
//
// It is deduplicated with the byte compare, so a run yields at most one Changed
// for e however often it marks or writes e. Every other Changed rule holds: the
// System's own Hooks readers skip it, and a removal of T later in the run leaves
// only the removal.
//
// It does nothing on a Store no reader watches for Changed, for a Component with
// no fields, or on an Entity that does not hold T, a dead one included. It
// declares nothing beyond what Set already holds.
func (s *Set[T]) MarkChanged(e Entity) {
	if s.changes.gate.on {
		s.changes.mark(e)
	}
}

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
//
// A replacement is not an addition. On a Store watched for Changed, the row is
// copied before it is replaced, once per run, and records Changed only if its
// bytes differ when the System's run ends.
func (s *Set[T]) UpdateFor(e Entity, value T) {
	store := s.resolved
	if s.changes.gate.on {
		if row, ok := store.probe(e); ok {
			s.changes.take(e, row)
		}
	}
	if store.update(e, value) {
		return
	}
	if !s.alive.Alive(e) {
		return
	}
	store.add(e, value)
	if s.hooks.on {
		store.hooks.added(e, kindAdded|kindChanged)
	}
}
