package ecs

import "reflect"

// Entities is the id authority: it allocates indices, tracks their generations,
// answers whether a handle is alive, and holds a reference to every Store so a
// despawn can empty all of them. There is exactly one per Engine, and that is
// what makes an Engine the boundary of one simulation.
//
// It is a kernel resource. Every handler that touches any Store declares
// read{*Entities}; spawning and despawning take it for write, which is why
// neither is a method a reader can reach: the authority to change which
// entities exist arrives through the write-locked promotions of this value —
// Spawn and WriteableEntities — and nowhere else.
type Entities struct {
	// gens holds the current generation of each index. An index that has been
	// allocated is never removed, so len(gens) is the index space in use.
	gens []uint32
	// free holds indices whose entity has been despawned, newest last.
	// Reclamation is eager and is therefore nothing at all: an index returns
	// here the moment its entity is retired, so there is no compaction, no
	// shrink and no sweep anywhere in the package.
	free []uint32
	// stores is every Store enrolled with this authority. It is the reason
	// holding Entities for write is the one lock that covers structural change:
	// a despawn empties all of them naming no Component at all, and every
	// handler that touches any Store holds Entities for read.
	stores []storeCore
	// classes is what component registration baked, keyed by the Component's Go
	// type. It is written during registration and read during registration —
	// once, while a Query is planned — and never touched while the engine runs.
	// It is what lets a Query declare a lock on a Store whose type it holds only
	// as a reflect.Type: a generic cannot be instantiated from one, so the
	// generic call is made where C is a compile-time type and kept here.
	classes map[reflect.Type]*componentClass
}

// declare records what component registration baked for one Component type. The
// Store's own duplicate-registration diagnostic is the kernel's, which names
// both plugins, so nothing is refused here.
func (en *Entities) declare(componentType reflect.Type, class *componentClass) {
	if en.classes == nil {
		en.classes = map[reflect.Type]*componentClass{}
	}
	en.classes[componentType] = class
}

// classOf reports what registration baked for a Component type, or nil if no
// plugin ever registered it. A Query asks this while it is planned, which is
// what turns "no such Component" into a composition failure naming the
// Component rather than a missing resource naming a store type nobody wrote.
func (en *Entities) classOf(componentType reflect.Type) *componentClass {
	return en.classes[componentType]
}

// NewEntities creates the authority, reserving room for ids indices. The number
// is the peak concurrent entity count the app expects, not a cap: exceeding it
// costs a growth, not an error.
//
// It is called at the composition root and handed to the plugins that register
// Components, because both component registration and the handler builder need
// the value at registration, where no resource may be read:
//
//	entities := ecs.NewEntities(maxIDs)
//	kernel.New(cfg).WithPlugins(ecs.Plugin(entities), game.Plugin(entities))
func NewEntities(ids uint32) *Entities {
	return &Entities{
		gens: make([]uint32, 0, ids),
		free: make([]uint32, 0, ids),
	}
}

// Alive reports whether e is the handle of an entity that exists now. A stale
// handle — one whose index has been recycled, or whose entity is simply gone —
// reports false, because the generation it carries is not the generation the
// index is at.
//
// This is rarely the question a System wants. "Is my target still alive" almost
// always means "does my target still have Health", which an accessor answers
// with the Store lock the System already holds; this one needs read{*Entities}.
//
// It is exact for every handle this authority ever issued. The one thing it
// cannot distinguish is a fabricated handle naming a generation that has not
// been handed out yet — the generation a freed index will carry when it is next
// allocated — because the index's current generation is the only thing
// consulted, and a separate record of which indices are free would be a second
// liveness structure for a handle nobody can hold.
func (en *Entities) Alive(e Entity) bool {
	index := e.idx()
	return int(index) < len(en.gens) && en.gens[index] == e.gen()
}

// enrol adds a Store to the set a despawn empties. It happens when the Store is
// created, because a Store the authority cannot reach would keep rows for
// entities that no longer exist and no later call would find them.
func (en *Entities) enrol(s storeCore) { en.stores = append(en.stores, s) }

// alloc hands out an id, recycling a freed index where there is one so that the
// flat sparse index of every Store stays bounded by peak concurrent entities
// rather than by entities ever created.
func (en *Entities) alloc() Entity {
	if n := len(en.free); n > 0 {
		index := en.free[n-1]
		en.free = en.free[:n-1]
		return newEntity(index, en.gens[index])
	}
	index := uint32(len(en.gens))
	en.gens = append(en.gens, 1)
	return newEntity(index, 1)
}

// despawn empties every Store of e and retires the handle, returning its index
// to the free list at once. It reports whether e was alive to begin with.
//
// It is eager, and eager reclamation is therefore nothing at all: no Store ever
// holds a dead entity, len(owners) stays the exact population driver selection
// reads, and adding a Component needs no orphan-slot branch — without eager
// removal, recycling one index leaks one dense row per recycle.
func (en *Entities) despawn(e Entity) bool {
	if !en.Alive(e) {
		return false
	}
	for _, store := range en.stores {
		store.remove(e)
	}
	index := e.idx()
	en.gens[index] = nextGeneration(en.gens[index])
	en.free = append(en.free, index)
	return true
}

// nextGeneration steps a generation, skipping the two values a live entity may
// never carry: 0, which would make the handle equal to NoEntity, and the
// all-ones generation a Store writes into an empty sparse slot.
func nextGeneration(g uint32) uint32 {
	g++
	if g == 0 || g == absentGeneration {
		return 1
	}
	return g
}
