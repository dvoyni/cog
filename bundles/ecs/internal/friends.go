package internal

import "reflect"

// The friend functions: what the ecs root and ecsimpl read from, or do to, an
// Entity's or the authority's unexported state. Only the root and ecsimpl can
// import this package, so these are not public API. Each is a field read or a
// direct call, and each inlines, so a probe, a spawn or a despawn pays nothing
// for going through one.

// NewEntities creates the authority for ecsimpl, reserving room for ids
// indices. See newEntities.
func NewEntities(ids uint32) *Entities { return newEntities(ids) }

// NewEntity builds a handle from its halves, for the root's tests.
func NewEntity(index, generation uint32) Entity { return newEntity(index, generation) }

// EntityIndex reads the index half of e for the root: the sparse slot a Store
// probes. It is Entity.idx spelled out rather than called, because it sits in
// the probe every Query field pays per Entity, and a call to a method that
// inlines still spends inlining budget the probe is measured against.
func EntityIndex(e Entity) uint32 { return uint32(e) }

// EntityGeneration reads the generation half of e for the root: what a Store's
// sparse slot is compared against. It is Entity.gen spelled out, for the reason
// EntityIndex is.
func EntityGeneration(e Entity) uint32 { return uint32(e >> 32) }

// EntitiesAlloc calls Entities.alloc for the root's Spawn.
func EntitiesAlloc(en *Entities) Entity { return en.alloc() }

// EntitiesDespawn calls Entities.despawn for the root's WriteableEntities.
func EntitiesDespawn(en *Entities, e Entity) bool { return en.despawn(e) }

// EntitiesEnrol calls Entities.enrol for the root's NewStore.
func EntitiesEnrol(en *Entities, remove func(e Entity) bool) { en.enrol(remove) }

// EntitiesIndexSpace reads len(Entities.gens) for the root's tests: how many
// indices the authority has ever handed out.
func EntitiesIndexSpace(en *Entities) int { return len(en.gens) }

// EntitiesDeclare calls Entities.declare for the root's RegisterComponent.
func EntitiesDeclare(en *Entities, componentType reflect.Type, class any) {
	en.declare(componentType, class)
}

// EntitiesClassOf calls Entities.classOf for the root's registration-time
// planning.
func EntitiesClassOf(en *Entities, componentType reflect.Type) any {
	return en.classOf(componentType)
}
