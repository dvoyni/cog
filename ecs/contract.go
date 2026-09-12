// Package ecs describes things in the world as Entities carrying Components.
//
// A Component is a plain value an Entity either has or has not, addressed by
// its Go type and held in a Store of its own. A System is a plain Go func whose
// parameter types say what it touches: a Query over the Components it iterates,
// whose field pointer-ness is its access mode, narrowed by Without and With
// filters that yield nothing and still declare a read; a Spawn over the Bundle
// it creates; the WriteableEntities that can retire one; the Get, Set and
// Remove accessors that reach one Component of an Entity it did not iterate to;
// the Read and Write handles that name another plugin's resource; the In that
// carries a value projected out of the event; and, for a command, the Resp it
// answers through. ToHandler turns one into the factory an ordinary cog
// subscription takes and ToExecute into the factory a command takes, so the ECS
// contributes no scheduler, no ordering and no registration API of its own.
//
// A Component names an engine-side thing — a model, a clip, a node, a pass tag
// — by the 64-bit hash of its name, because a name is a string and a Component
// holds no pointers. HashOf produces one and needs nothing to do it, so a
// System renames what an Entity points at holding only the lock it already had.
// Names is the reverse half and belongs to the plugin that resolves names, not
// to anything central: there is deliberately no process-wide interner.
//
// Nothing here is a Command. A structural change is a direct call on a handle
// the System already holds, and the exclusion it needs was arranged before the
// frame started: Spawn and WriteableEntities declare write{*Entities}, which is
// a total barrier because Entities holds a reference to every Store.
//
// There is no binding mechanism, and that is the decision. A plugin that is not
// the ECS attaches to the world by being an ordinary plugin: it registers
// Components if it has any, subscribes Systems like anything else, and reaches
// its own frame-local resource from inside them through Read and Write. No
// binding type, no adapter, no registration call of the ECS's own. See
// ecs/docs/specs/ecs.md, which this package is judged against.
//
// The two decisions the rest of the design rests on are made here. Storage is
// sparse sets rather than archetype tables, so one Component type is exactly one
// object and therefore exactly one lock unit. And an Entity's generation lives
// in the sparse slot, so the compare that finds a row is the compare that
// rejects a stale handle: liveness is not an extra structure, it is the probe.
package ecs

import "strconv"

// Entity is an opaque handle to one thing. It is comparable, copyable and
// usable as a map key, and the zero value means no Entity.
//
// It is a uint64 with the index in the low 32 bits and the generation in the
// high 32, and that split is deliberately not public contract: there is no
// exported Index or Generation, only the unexported accessors below. Both this
// and a struct of two uint32s are 8 bytes and both are comparable, but the
// struct leaks its layout into every call site and can never be re-cut.
//
// Generations start at 1, so Entity(0) unambiguously means "no Entity" while
// index 0 stays an ordinary usable slot. Thirty-two bits of generation is about
// 4e9 reuses of one slot before a stale handle could alias.
type Entity uint64

// NoEntity is the absent handle. Compare with ==.
const NoEntity Entity = 0

// absentGeneration is the generation half a Store writes into a sparse slot it
// holds nothing for. No live entity ever carries it, which is what lets the
// membership test be one load and one compare with no tombstone branch.
const absentGeneration uint32 = 0xFFFFFFFF

func newEntity(index, generation uint32) Entity {
	return Entity(uint64(generation)<<32 | uint64(index))
}

func (e Entity) idx() uint32 { return uint32(e) }

func (e Entity) gen() uint32 { return uint32(e >> 32) }

// String renders an entity as its index and generation, "Entity(7v2)" being
// index 7 at generation 2. The halves are shown because a log that cannot
// distinguish a recycled index from the handle that preceded it is useless;
// reading them back in code is what the type refuses.
func (e Entity) String() string {
	if e == NoEntity {
		return "NoEntity"
	}
	return "Entity(" + strconv.FormatUint(uint64(e.idx()), 10) + "v" +
		strconv.FormatUint(uint64(e.gen()), 10) + ")"
}
