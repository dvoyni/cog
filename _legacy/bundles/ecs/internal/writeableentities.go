package internal

import "github.com/dvoyni/cog/kernel"

// WriteableEntities is the write-locked promotion of the id authority, and the
// only thing that can retire an Entity. Every System holds *Entities for read,
// so an exported mutator on it would let a read-locked handler change which
// Entities exist; the authority arrives here instead, and is visible in the
// System's signature and nowhere else.
//
// It declares write{*Entities} and nothing besides — a despawn names no
// Component at all, because Entities reaches every Store itself.
type WriteableEntities struct {
	entities kernel.Write[*Entities]
	// resolved is entities' value for the current invocation. See resolver.
	resolved *Entities
}

// prepare declares the write. It runs once, at registration.
func (w *WriteableEntities) prepare(_ *Entities, access kernel.ResourceAccess) {
	w.entities = access.GetWrite[*Entities]()
}

// resolve reads the authority out of its cell for this invocation.
func (w *WriteableEntities) resolve() { w.resolved = w.entities.Get() }

// Despawn retires an Entity and reports whether it was alive to begin with. It
// is total and eager: every Store is emptied of e at once and the index returns
// to the free list immediately, so no Store ever holds a dead Entity and nothing
// stale is left for a later call to trip over.
//
// Despawning the Entity a Query is currently visiting is safe, which is what the
// backwards walk buys. Despawning any other Entity in the driver's Store is
// undefined for that walk.
func (w *WriteableEntities) Despawn(e Entity) bool {
	return w.resolved.despawn(e)
}

// Drain applies everything the deferring handles have queued in this Engine —
// whatever System and whatever event queued it — under the wide lock this
// parameter already declares, and nothing else: it is the spawn pass, the free
// list settling, and the despawn pass, in that order.
//
// It is a call the app makes rather than a point the engine picks. A drain
// System is an ordinary System of one parameter, scheduled where it belongs:
//
//	func Drain(entities *ecs.WriteableEntities) { entities.Drain() }
//
// An app writes as many as it needs. The ECS subscribes one itself, on
// app.UpdateEvent in the Last phase as ecs.DrainOnUpdate, always, so a queue is
// never left undrained; an app drain System is how a change is made visible
// earlier than that, within the publication that queued it.
//
// Every drained change is recorded here, under this parameter's write, with the
// same Hooks records an immediate Spawn or Despawn would have written: a reader
// cannot tell the two apart, and a deferring handle records nothing at the call.
// A Store's log follows the order the drain applies changes in — handles in
// enrolment order, each buffer in queue order, the spawn pass before the despawn
// pass — and one drain counts as one writer run on each Store it appends to,
// however many passes it makes. See deferred.md § Hooks and systempace.go.
//
// A drain over empty buffers is a length check per enrolled buffer: there is no
// empty-drain skip. Nothing else drains — not ShrinkCmd and not the read
// Commands, though both hold write{*Entities} too. See
// bundles/ecs/docs/specs/deferred.md § The drain.
func (w *WriteableEntities) Drain() {
	w.resolved.drain()
}
