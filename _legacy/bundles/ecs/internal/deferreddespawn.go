package internal

import (
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// DeferredDespawn retires Entities without holding the wide lock: it queues each
// Despawn in a buffer of its own, and a drain applies the queue under
// write{*Entities} later. Migrating a System is one token in its signature and
// no call-site change —
//
//	func Lifetime(entities *ecs.WriteableEntities, q *ecs.Query[Expiring]) { … }
//	func Lifetime(despawn *ecs.DeferredDespawn, q *ecs.Query[Expiring]) { … }
//
// — which is why the method is Despawn rather than a Queue.
//
// It declares read{*Entities} and nothing besides, so a System holding it runs
// beside every Query and every accessor and is excluded only against writers. A
// despawn names no Component at all, which is what makes the second handle
// generic in nothing: folding Despawn onto a spawn handle would force a
// Component set type on Systems that never spawn.
//
// Queuing is not a Structural change; the drain is. Nothing anyone can see
// changes at the call: a queued Entity stays alive, every Accessor reaches it
// and every Query iterates it, the queuer's own included, until a System calls
// WriteableEntities.Drain. Then every System ordered after that drain System
// sees it gone.
//
// That is also what makes it safe to queue a Despawn of an Entity other than
// the one a Query is visiting, which is undefined for an immediate Despawn: the
// walk is not disturbed, because nothing has happened yet.
//
// Despawn is its one exported method, and there is no Len, Pending, Clear or
// Drain: nothing reports what is queued, capacity belongs to ShrinkCmd, and
// Drain stays on WriteableEntities so that a System which drains names the wide
// lock in its signature. A game that must mark an Entity doomed before the drain
// adds a Tag of its own through Set[T].UpdateFor, which is immediate and holds
// only the narrow write. See bundles/ecs/docs/specs/deferred.md § The two
// handles.
type DeferredDespawn struct {
	// queued is this handle's own buffer, in the order this handle queued. It is
	// reached by exactly one appender — a System never runs concurrently with
	// itself, because every System declares Exclusive — and by the drain, which
	// holds write{*Entities} and so can never overlap the appender. That is the
	// whole reason nothing here takes a lock or a mutex.
	//
	// Only a drain empties it, cutting it to length zero and keeping its
	// capacity: resolve does not, because resolve runs once per invocation and a
	// System invoked twice before a drain would silently lose its first queue.
	queued []Entity
}

// prepare declares the read, enrols the buffer with the despawn pass, and enrols
// its release with ShrinkCmd. It runs once, at registration.
func (d *DeferredDespawn) prepare(en *Entities, access kernel.ResourceAccess) {
	// This handle declares the read itself rather than inheriting the one every
	// System takes: a System holding only a despawn handle would otherwise name
	// nothing on the authority and could append to its buffer while the drain
	// walked it. A read serialises nothing — readers run together — and it is
	// what makes the lock-free buffer sound rather than lucky. The handle is
	// discarded, as systemcall.go's and hooks.go's are: what the read buys is the
	// exclusion, not a value.
	access.GetRead[*Entities]()
	// The authority the pass despawns through is bound here, at registration,
	// rather than read from the handle above. There is exactly one per Engine and
	// it is the value drain is a method on; reading a cell is valid only while
	// its own handler holds the lock, and the pass runs inside the drain System's
	// write rather than inside this handle's System's read.
	en.enrolDrainDespawn(func() { d.apply(en) })
	en.enrolScratch(d.release)
}

// Despawn queues e for the next drain, whichever System calls it and on whatever
// event.
//
// It returns nothing, and that is the house rule applied rather than an
// exception to it: an ECS operation that can miss reports bool, one that cannot
// returns nothing, and a queued Despawn cannot miss at the call because the
// drain decides. Reporting whether e was alive here would answer a different and
// misleading question — an Entity alive at the call can be gone by the drain.
//
// Nothing happens besides the append. e is still alive to this System and to
// every other until a drain, so a System that must skip it for the rest of its
// run keeps its own local note: there is no per-run filter on Queries.
func (d *DeferredDespawn) Despawn(e Entity) {
	d.queued = append(d.queued, e)
}

// apply is this handle's half of one drain's despawn pass: every queued Despawn,
// in the order this handle queued them, each doing exactly what
// WriteableEntities.Despawn does — every capture, then every Store's remove,
// then the generation bump, then the index onto the free list.
//
// A queued Despawn of an Entity that is not alive at the drain does nothing and
// records nothing, and one rule covers every way that happens: despawned
// immediately earlier in the frame, despawned by another handle earlier in the
// same drain, queued twice, or held from an earlier frame. A handle whose index
// has been recycled carries the old generation and so fails Alive, which is what
// makes queuing a Despawn safe at all. Nothing panics on any of it, in a
// validating build or any other: a stale Despawn is not always a bug, and the
// generation check is the designed answer rather than a missed error.
//
// The buffer is cut to zero length and keeps its capacity, so a steady-state
// drain allocates nothing. Giving that capacity back is ShrinkCmd's, through
// release.
func (d *DeferredDespawn) apply(en *Entities) {
	for _, e := range d.queued {
		en.despawn(e)
	}
	d.queued = d.queued[:0]
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
func (d *DeferredDespawn) release() uintptr {
	released := uintptr(cap(d.queued)) * unsafe.Sizeof(Entity(0))
	d.queued = nil
	return released
}
