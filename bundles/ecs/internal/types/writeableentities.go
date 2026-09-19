package types

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
}

// prepare declares the write. It runs once, at registration.
func (w *WriteableEntities) prepare(_ *Entities, access kernel.ResourceAccess) {
	w.entities = access.GetWrite[*Entities]()
}

// Despawn retires an Entity and reports whether it was alive to begin with. It
// is total and eager: every Store is emptied of e at once and the index returns
// to the free list immediately, so no Store ever holds a dead Entity and nothing
// stale is left for a later call to trip over.
//
// Despawning the Entity a Query is currently visiting is safe, which is what the
// backwards walk buys. Despawning any other Entity in the driver's Store is
// undefined for that walk.
func (w *WriteableEntities) Despawn(e Entity) bool {
	return w.entities.Get().despawn(e)
}
