package types

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
	// gens holds the current generation of each index, so len(gens) is the
	// index space in use. An index that has been allocated is removed only when
	// ShrinkCmd drops it from the top of the index space, free.
	gens []uint32
	// free holds indices whose entity has been despawned, newest last.
	// Reclamation is eager and is therefore nothing at all: an index returns
	// here the moment its entity is retired, and nothing reclaims on its own.
	// Capacity goes back only when the app executes ShrinkCmd.
	free []uint32
	// stores is every Store enrolled with this authority, as the one call a
	// despawn makes of it: its remove. It is the reason holding Entities for
	// write is the one lock that covers structural change: a despawn empties all
	// of them naming no Component at all, and every handler that touches any
	// Store holds Entities for read.
	//
	// It is the Store's remove bound to the Store, which NewStore enrols. A
	// despawn pays one indirect call per Store, never per entity.
	stores []func(e Entity) bool
	// captures is one call per Store a Hooks reader watches, which a despawn
	// makes before it empties the Stores: each probes its own Store and records
	// the removal with T's last value. A watched Store enrols its capture beside
	// its remove when its first reader registers, so the list is fixed before any
	// System runs, and a world nobody reads has none and takes the despawn path
	// it always took. See hooks.go.
	captures []func(e Entity)
	// classes is what component registration baked, keyed by the Component's Go
	// type. It is written during registration and read during registration —
	// once, while a Query is planned — and never touched while the engine runs.
	// It is what lets a Query declare a lock on a Store whose type it holds only
	// as a reflect.Type: a generic cannot be instantiated from one, so the
	// generic call is made where C is a compile-time type and kept here.
	classes map[reflect.Type]*componentClass
	// The fields below are ShrinkCmd's. They sit after everything a spawn or a
	// despawn reads, and what only the Command reads is behind one pointer,
	// because this object's size class is measurable on a despawn: three
	// slices here took it from 80 to 144 bytes and BenchmarkDespawnOnly from
	// 17.6 to 19.5 ns with no instruction on its path changed, where a uint32
	// and a pointer, 96 bytes, measured 17.6.
	//
	// floor is the generation an index appended to the index space starts at.
	// A shrink that drops free indices from the top of the index space forgets
	// their generations, so it raises the floor to the highest of them first:
	// each is the generation that index would have carried next, above every
	// generation it was ever issued, so a handle to a dropped index never
	// matches the Entity the index is later allocated to. It starts at 1, which
	// is where generations start.
	floor uint32
	// writers is how many Systems have registered, and so the name the next one
	// gets: a Changed record names the System whose run end recorded it. It is
	// written during registration only, and sits in what was padding after
	// floor, so this object's size class is unchanged.
	writers uint32
	// shrinkable is what ShrinkCmd reaches, enrolled at registration and
	// created by the first enrolment.
	shrinkable *shrinkable
}

// shrinkable is everything ShrinkCmd reaches besides the authority's own
// arrays, and nothing else reads it.
type shrinkable struct {
	// stores is every enrolled Store's shrink. It is kept apart from
	// Entities.stores because a despawn must never pay for it.
	stores []func() uintptr
	// scratch is every Query's release, enrolled when the Query is planned, and
	// every System's row copy's, enrolled when the System shares it.
	scratch []func() uintptr
	// hooks is every Hook log's shrink, enrolled when the log is created, and
	// every reader's release, enrolled when the reader is prepared.
	hooks []func() uintptr
}

// newEntities creates the authority, reserving room for ids indices. The number
// is the peak concurrent entity count the app expects, not a cap: exceeding it
// costs a growth, not an error.
//
// The ecs plugin is its only caller outside tests, which is what keeps it one
// per Engine. Component registration and the handler builder reach the value it
// publishes through kernel.Registrar.Dependency.
func newEntities(ids uint32) *Entities {
	return &Entities{
		gens:  make([]uint32, 0, ids),
		free:  make([]uint32, 0, ids),
		floor: 1,
	}
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

// enrol adds a Store to the set a despawn empties, as its remove. It happens
// when the Store is created, because a Store the authority cannot reach would
// keep rows for entities that no longer exist and no later call would find
// them.
func (en *Entities) enrol(remove func(e Entity) bool, shrink func() uintptr) {
	en.stores = append(en.stores, remove)
	en.shrinkables().stores = append(en.shrinkables().stores, shrink)
}

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
	en.gens = append(en.gens, en.floor)
	return newEntity(index, en.floor)
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
	if validate {
		// The Entity's Components stop holding their Lists at the Despawn,
		// whatever a Hook log retains: ecs.md § Validation mode.
		releaseEntityLists(e)
	}
	// Every capture runs before any Store is emptied, so each records T's value
	// as it stood at the Despawn. Despawn's one call to each Store is unchanged.
	for _, capture := range en.captures {
		capture(e)
	}
	for _, remove := range en.stores {
		remove(e)
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

// nextWriter names a System as it registers. Names start at 1, so the 0 every
// record but a change carries names no System.
func (en *Entities) nextWriter() uint32 {
	en.writers++
	return en.writers
}

// enrolScratch adds a per-System buffer's release to the set ShrinkCmd calls.
// It happens once, when the buffer's owner is planned at registration.
func (en *Entities) enrolScratch(release func() uintptr) {
	en.shrinkables().scratch = append(en.shrinkables().scratch, release)
}

// enrolHooks adds a Hook log's shrink, or a reader's release, to the set
// ShrinkCmd calls unless KeepHooks. It happens once, at registration.
func (en *Entities) enrolHooks(release func() uintptr) {
	en.shrinkables().hooks = append(en.shrinkables().hooks, release)
}

// shrinkables is what ShrinkCmd reaches, created on first use. Only
// registration enrols, so only registration creates it.
func (en *Entities) shrinkables() *shrinkable {
	if en.shrinkable == nil {
		en.shrinkable = &shrinkable{}
	}
	return en.shrinkable
}
