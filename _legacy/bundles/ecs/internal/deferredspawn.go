package internal

import (
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// queuedSpawn is one queued Spawn: the Reserved Entity the call handed back and
// the Component set it is to come to life with.
//
// They are one struct rather than two slices because the two are inseparable —
// the Entity is fixed at the call, so a queue that could get out of step
// between them is a bug waiting to be written — and because a pair is one
// append and one bounds check where two slices are two.
type queuedSpawn[S any] struct {
	e   Entity
	set S
}

// DeferredSpawn creates Entities without holding the wide lock: it queues each
// Spawn in a buffer of its own, and a drain applies the queue under
// write{*Entities} later. Migrating a System is one token in its signature and
// no call-site change —
//
//	func Cast(spawn *ecs.Spawn[Projectile], q *ecs.Query[Casters]) { … }
//	func Cast(spawn *ecs.DeferredSpawn[Projectile], q *ecs.Query[Casters]) { … }
//
// — which is why the method is New rather than a Queue.
//
// It declares read{*Entities}, and read{*Store[F]} per Component set field
// where the immediate handle declares the write. The read is what keeps the
// lock narrow: a write conflicts with every Query and every accessor of F, a
// read only with writers of F, which already serialise against each other. It
// is no weaker as an ownership declaration — the kernel's coupling check walks
// the read set exactly as it walks the write set, so a plugin still cannot
// fabricate another plugin's Components without declaring a dependency on their
// owner.
//
// Queuing is not a Structural change; the drain is. Nothing anyone can see
// changes at the call: the Entity New returns is not alive, no Store holds
// anything for it, and no Query yields it, the queuer's own included, until a
// System calls WriteableEntities.Drain. Then every System ordered after that
// drain System sees it, carrying exactly the Component set it was queued with
// and nothing else.
//
// New is its one exported method, and there is no Len, Pending, Clear or Drain,
// for the reasons DeferredDespawn gives. See
// bundles/ecs/docs/specs/deferred.md § The two handles.
type DeferredSpawn[S any] struct {
	// queued is this handle's own buffer, in the order this handle queued. It is
	// reached by exactly one appender — a System never runs concurrently with
	// itself, because every System declares Exclusive — and by the drain, which
	// holds write{*Entities} and so can never overlap the appender. That is the
	// whole reason nothing here takes a lock or a mutex.
	//
	// Only a drain empties it, cutting it to length zero and keeping its
	// capacity: resolve does not, because resolve runs once per invocation and a
	// System invoked twice before a drain would silently lose its first queue.
	queued []queuedSpawn[S]
	// entities is the read-locked authority the reservation cursor is taken
	// through. It is the handle rather than the registration-time value because
	// a handle is what the kernel guards: the cell it reads is the one the lock
	// covers, and New runs inside this System's own read.
	entities kernel.Read[*Entities]
	// resolved is entities' value for the current invocation. See resolver.
	resolved *Entities
	// fields is the Component set's field table as registration left it, with
	// each Store bound at registration rather than resolved per invocation:
	// unlike Spawn's, this table is read by the drain, which runs inside the
	// drain System's write rather than inside this handle's System's read.
	// Reflection runs exactly once, in prepare, and never again.
	fields []spawnField
	// hooks is whether the spawns this handle applies are recorded on any Store
	// the Component set carries: on when a Hooks reader watches one of them for
	// Spawns, additions or changes.
	//
	// It is deliberately not offered through spawnGated, which is how Spawn's
	// gate is collected and checked. systemCall.lock collects a spawn gate from
	// any parameter offering one, and enrolPace then marks that System "can
	// append to every Store's log" for Validation mode's pace count. A queuing
	// System appends to nothing: the writing and the records happen at the
	// drain, which holds write{*Entities} and is the Structural change. Carrying
	// the gate here would charge the queuer for a drain it never makes, and one
	// drain is one writer run per Store would stop being true.
	//
	// So the gate is checked at the first drain instead, which is what armed is
	// for. See bundles/ecs/docs/specs/deferred.md § Hooks.
	hooks spawnGate
	// armed says the gate has been checked. A Store's watched kinds are written
	// in one place, Hooks.prepare, and every reader registers before any System
	// runs, so what the check computes is fixed by the time the first drain
	// makes it and is the same answer every drain after.
	armed bool
}

// prepare plans the Component set against the world, declares the reads, enrols
// the buffer with the spawn pass, and enrols its release with ShrinkCmd. It
// runs once, at registration.
//
// It panics when a field names a Component no plugin registered, naming the
// Component and the Component set; the plugin boundary turns that into a
// composition failure naming the plugin.
func (s *DeferredSpawn[S]) prepare(en *Entities, access kernel.ResourceAccess) {
	// The read of the authority, which this handle declares itself rather than
	// inheriting the one every System takes, for the reason DeferredDespawn
	// gives: a System holding only this handle would otherwise name nothing on
	// the authority and could append to its buffer while the drain walked it.
	s.entities = access.GetRead[*Entities]()
	s.fields = planSet[S](en, access, func(class *componentClass) spawnField {
		planned := class.declareDeferredSet(access)
		planned.store = planned.get()
		return planned
	})
	s.hooks = spawnGate{fields: s.fields}
	// The authority the pass spawns through is bound here, at registration,
	// rather than read from the handle above. There is exactly one per Engine;
	// reading a cell is valid only while its own handler holds the lock, and the
	// pass runs inside the drain System's write rather than inside this handle's
	// System's read.
	en.enrolDrainSpawn(func() { s.apply(en) })
	en.enrolScratch(s.release)
}

// resolve reads the authority out of its cell for this invocation. The Stores
// are not resolved here: they are bound once at registration, because the drain
// is what writes through them. See fields.
func (s *DeferredSpawn[S]) resolve() { s.resolved = s.entities.Get() }

// New reserves an Entity, queues it with the Component set it is to carry, and
// returns it. The Entity it returns is not alive: its id and generation are
// fixed here and that is all — it has no Components, Alive is false for it, and
// an immediate Despawn, Set[T].UpdateFor or Remove[T].From on it misses,
// exactly as on any Entity that is not alive. It comes to life at the next
// drain, carrying exactly this Component set and nothing else.
//
// That is the price of keeping the name New, where Spawn[S].New's Entity is
// complete when New returns, and the type in the signature is the only thing
// that says so.
//
// What a Reserved Entity is for is linking what was spawned — a missile's
// target, a projectile's owner — in the same run: it can be stored as a
// Reference in any Component, written immediately or queued through another
// deferred New, and the Reference resolves to nothing until the drain, as a
// Reference to any Entity that is not alive does.
//
// The reservation is one atomic add on the authority's cursor, and it is the
// only atomic in the design. Parallel deferring Systems contend on that one
// word; none is serialised, and no lock set is widened.
func (s *DeferredSpawn[S]) New(components S) Entity {
	e := s.resolved.reserve()
	s.queued = append(s.queued, queuedSpawn[S]{e: e, set: components})
	return e
}

// apply is this handle's half of one drain's spawn pass: every queued Spawn, in
// the order this handle queued them, each bringing its Reserved Entity to life
// and writing its Components, recording what an immediate Spawn records.
//
// Every handle's spawns are applied before any handle's despawns, which is what
// lets a queued Despawn reach an Entity queued for Spawn in the same drain. The
// two are not collapsed: the Entity is spawned here and despawned in the
// despawn pass, and both are recorded, because collapsing would save a few
// hundred nanoseconds and lie to every index reading the log.
//
// The buffer is cut to zero length and keeps its capacity, so a steady-state
// drain allocates nothing. Giving that capacity back is ShrinkCmd's, through
// release.
func (s *DeferredSpawn[S]) apply(en *Entities) {
	if !s.armed {
		s.hooks.check()
		s.armed = true
	}
	for i := range s.queued {
		queued := &s.queued[i]
		en.spawnReserved(queued.e)
		// The Component set is written out of the buffer, which is where the call
		// copied it: the address handed to the per-field closures is into the
		// slice's backing array and never a stack one, so nothing escapes here
		// for the reason Spawn's staging buffer exists.
		buffer := unsafe.Pointer(&queued.set)
		if s.hooks.on {
			s.hooks.spawn(queued.e, buffer)
			continue
		}
		for j := range s.fields {
			field := &s.fields[j]
			field.set(field.store, queued.e, unsafe.Add(buffer, field.offset))
		}
	}
	s.queued = s.queued[:0]
}

// release gives the buffer back and reports the bytes let go. It is Scratch, the
// same area a Query's walk and a writer's row copies are, so ShrinkCmd's
// KeepScratch opts it out and ShrinkResponse.Scratch counts it: a deferral
// buffer is a per-System buffer of exactly that kind, and needs no area of its
// own.
//
// A System that queues without bound grows its buffer without bound, exactly as
// the free list does. That is the app's to release through a Command, never a
// heuristic's to guess.
func (s *DeferredSpawn[S]) release() uintptr {
	released := uintptr(cap(s.queued)) * unsafe.Sizeof(queuedSpawn[S]{})
	s.queued = nil
	return released
}
