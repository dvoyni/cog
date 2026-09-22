package types

import (
	"reflect"
	"sync/atomic"
	"unsafe"
)

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
	// drainSpawns is one call per deferring spawn handle, which a drain's spawn
	// pass makes in enrolment order: each applies its own typed buffer, in the
	// order that handle queued. The handle enrols it in its own prepare, so the
	// list is fixed before any System runs, exactly as stores and captures are,
	// and a world that defers nothing has none.
	//
	// It is a func rather than an interface with two methods because the two
	// passes are two separate walks: a drain costs one indirect call per handle
	// per pass, never per queued change, and a handle that only despawns puts
	// nothing here. See bundles/ecs/docs/specs/deferred.md § Two func
	// registries, one per pass.
	drainSpawns []func()
	// drainDespawns is drainSpawns' twin for the despawn pass, walked after the
	// free list settles, in the same enrolment order.
	drainDespawns []func()
	// classes is what component registration baked, keyed by the Component's Go
	// type. It is written during registration and read during registration —
	// once, while a Query is planned. It is what lets a Query declare a lock on
	// a Store whose type it holds only as a reflect.Type: a generic cannot be
	// instantiated from one, so the generic call is made where C is a
	// compile-time type and kept here.
	//
	// While the engine runs, the one thing that reads it is the read Commands,
	// which scan it for a Component named by string (classNamed) and hold
	// write{*Entities} while they do, so nothing else is running. It is the
	// name-to-Component mapping; there is no other.
	classes map[reflect.Type]*componentClass
	// reserved is the reservation cursor: how many Reserved Entities deferring
	// handles have taken since the last drain settled it. It is the one atomic
	// in the whole deferral design, and it is one atomic add per deferred New —
	// parallel deferring Systems contend on this one word, no System is
	// serialised, and no lock set is widened.
	//
	// It is a cursor over the free list, read from the newest index down, and
	// past len(gens) at the floor once the free list is exhausted. It can be
	// taken without the write lock because nothing writes the free list or the
	// generations while any System runs: every System touching a Store holds
	// read{*Entities}, and a writer is excluded against all of them. settle cuts
	// the free list back to where it left off and resets it. See
	// bundles/ecs/docs/specs/deferred.md § Reservation holds only read{*Entities}.
	reserved atomic.Uint32
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

// classNamed resolves a Component named by string: the class whose owner,
// kernel.TypeName of its type, is name when exactly one class has that owner,
// or the class whose package-qualified form, PkgPath.Name, is name, which is
// what resolves two types rendering one owner. It returns nil for an unknown or
// an ambiguous name, and the caller's refusal path walks classes again to say
// which.
//
// It is a scan, and it is called only by the read Commands, under
// write{*Entities}: nothing on a frame's path ever names a Component by string.
func (en *Entities) classNamed(name string) *componentClass {
	var found *componentClass
	for componentType, class := range en.classes {
		if class.owner != name && qualifiedName(componentType) != name {
			continue
		}
		if found != nil && found != class {
			return nil
		}
		found = class
	}
	return found
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
// It is one compare against one word and consults nothing else — no free list,
// no second liveness structure — and it is exact for every handle this
// authority ever issued and for the one handle that used to fool it. A freed
// index stores its next generation with freeGeneration set, so the handle
// fabricated from that index and that generation does not match until alloc
// clears the bit as the index goes live.
//
// The one handle it still says true of is one fabricated with freeGeneration
// itself, which is not a generation any Entity carries and which no caller
// inside Go can name: Entity's halves are unexported and every handle comes
// from newEntity. The one path where a caller outside Go can make one is the
// read by name, whose readable checks for it (readbyname.go).
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

// enrolDrainSpawn adds a deferring spawn handle's apply to the set a drain's
// spawn pass walks. It happens in the handle's own prepare, which runs once at
// registration, so the registry is append-only and fixed before any System
// runs.
func (en *Entities) enrolDrainSpawn(apply func()) {
	en.drainSpawns = append(en.drainSpawns, apply)
}

// enrolDrainDespawn adds a deferring despawn handle's apply to the set a
// drain's despawn pass walks. See enrolDrainSpawn.
func (en *Entities) enrolDrainDespawn(apply func()) {
	en.drainDespawns = append(en.drainDespawns, apply)
}

// drain applies everything queued in this Engine, whatever System and whatever
// event queued it, and nothing else: one drain is the spawn pass, the free list
// settling, and the despawn pass, in that order.
//
// The order is the whole of the rule. Every handle's spawns are applied before
// any handle's despawns, so a queued Despawn can never reach an Entity a later
// handle has not spawned yet; and the settle sits between the passes so that
// the despawn pass is literally today's despawn, with no special case for an
// outstanding reservation.
//
// There is no empty-drain skip. A drain over empty buffers is a length check
// per enrolled buffer, and a skip would be a branch guarding a branch.
//
// It is reached only through WriteableEntities.Drain, so a drain always holds
// write{*Entities} and can never overlap a System queuing into a buffer: every
// System declares read{*Entities}, and the scheduler is the exclusion. See
// bundles/ecs/docs/specs/deferred.md § The drain.
func (en *Entities) drain() {
	for _, apply := range en.drainSpawns {
		apply()
	}
	en.settle()
	for _, apply := range en.drainDespawns {
		apply()
	}
}

// settle returns the free list to an ordinary free list between the two passes:
// the indices the reservation cursor handed out are cut off the free list, and
// the cursor is reset to nothing.
//
// It sits between the passes so that the despawn pass is literally today's
// despawn, with no special case for an outstanding reservation: an index freed
// there goes onto a free list nothing has already handed out, and is available
// to the very next reservation.
//
// The cut is by count rather than by value, because the cursor hands indices
// out from the newest end of the free list downward, which is the end alloc
// takes from. A cursor that ran past the free list took the rest of its indices
// from past len(gens), and there is nothing to cut for those: the spawn pass
// grew the index space to reach them.
//
// Moving the released list onto the free list is the third step of the settle
// and arrives with the released list itself.
func (en *Entities) settle() {
	taken := min(int(en.reserved.Swap(0)), len(en.free))
	en.free = en.free[:len(en.free)-taken]
}

// reserve hands out one Reserved Entity: an index and a generation fixed here,
// with no Components until the drain and Alive false until then. It is what a
// deferred New returns, and it is the only thing in the ECS that changes state
// without a write lock.
//
// The cursor is one atomic add. Below len(free) it names an index from the free
// list, newest first, which is the index alloc would have taken; past it the
// indices come from past len(gens) at the floor, the generation a fresh index
// starts live at. Nothing is written here — not the generations, not the free
// list — so nothing needs the write lock: the spawn pass does the writing, and
// settle does the cutting.
//
// The generation it hands back is the generation the index will be alive at,
// with the free bit off, so Alive stays exactly false until the spawn pass
// stores it. A free index carries that same generation with the bit on, and an
// index past the end is past len(gens); both miss in the one compare Alive is.
func (en *Entities) reserve() Entity {
	cursor := int(en.reserved.Add(1)) - 1
	if cursor < len(en.free) {
		index := en.free[len(en.free)-1-cursor]
		return newEntity(index, en.gens[index]&^freeGeneration)
	}
	return newEntity(uint32(len(en.gens)+cursor-len(en.free)), en.floor)
}

// spawnReserved brings a Reserved Entity to life, which is the first half of
// what the spawn pass does for one queued Spawn: it grows the index space where
// the reservation went past the end, and clears the free bit by storing the
// generation the handle has carried since it was reserved.
//
// The indices it grows past are reservations of their own — the cursor hands
// out past-the-end indices consecutively, and every reservation is queued by
// the same call that made it — so each is filled by its own queued Spawn, in
// whatever order the handles enrolled. Until then the placeholder carries the
// free bit, so an index this pass has not reached yet answers Alive false like
// any other index that is not live.
func (en *Entities) spawnReserved(e Entity) {
	index := int(e.idx())
	for len(en.gens) <= index {
		en.gens = append(en.gens, en.floor|freeGeneration)
	}
	en.gens[index] = e.gen()
}

// alloc hands out an id, recycling a freed index where there is one so that the
// flat sparse index of every Store stays bounded by peak concurrent entities
// rather than by entities ever created.
func (en *Entities) alloc() Entity {
	if n := len(en.free); n > 0 {
		index := en.free[n-1]
		en.free = en.free[:n-1]
		// Going live clears the free bit despawn set. The generation itself is
		// the one despawn stepped to: this hands out the handle the index was
		// already recorded as owing, and it is alive from here.
		generation := en.gens[index] &^ freeGeneration
		en.gens[index] = generation
		return newEntity(index, generation)
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
	// The generation steps and takes the free bit with it, so while the index is
	// free neither the handle just retired nor the handle the index will carry
	// next is alive. The bit comes off at alloc.
	en.gens[index] = nextGeneration(en.gens[index]) | freeGeneration
	en.free = append(en.free, index)
	return true
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

func (en *Entities) shrink(request ShrinkRequest) ShrinkResponse {
	var released ShrinkResponse
	if !request.KeepHooks {
		for _, shrink := range en.shrinkables().hooks {
			released.Hooks += shrink()
		}
	}
	if !request.KeepStores {
		for _, shrink := range en.shrinkables().stores {
			released.Stores += shrink()
		}
	}
	if !request.KeepEntities {
		released.Entities = en.shrinkIndices()
	}
	if !request.KeepScratch {
		for _, release := range en.shrinkables().scratch {
			released.Scratch += release()
		}
	}
	return released
}

// shrinkIndices drops the free indices at the top of the index space and cuts
// the generations and the free list to capacity equal to length, reporting the
// bytes let go.
func (en *Entities) shrinkIndices() uintptr {
	before := en.bytes()
	// A free index carries the free bit, so the unused run at the top is one
	// walk down the generations — no pass over the free list, and none over the
	// bitmap of it this used to build.
	top := len(en.gens)
	for top > 0 && en.gens[top-1]&freeGeneration != 0 {
		top--
	}
	// Dropping an index forgets its generation, so the floor takes the highest
	// generation dropped before the index space is cut. The free bit comes off
	// first: the floor is the generation a fresh index starts live at.
	for _, generation := range en.gens[top:] {
		en.floor = max(en.floor, generation&^freeGeneration)
	}
	kept := en.free[:0]
	for _, index := range en.free {
		if int(index) < top {
			kept = append(kept, index)
		}
	}
	en.free = clip(kept)
	en.gens = clip(en.gens[:top])
	return before - en.bytes()
}

// bytes is what the generations and the free list hold, by capacity.
func (en *Entities) bytes() uintptr {
	return uintptr(cap(en.gens)+cap(en.free)) * unsafe.Sizeof(uint32(0))
}

// nextGeneration steps a live generation, wrapping back to 1 at the top of the
// live half rather than stepping into the free half. That wrap is also what
// keeps the two values a live entity may never carry out of reach: 0, which
// would make the handle equal to NoEntity, is now past a wrap rather than one
// step away, and absentGeneration, the all-ones generation a Store writes into
// an empty sparse slot, is itself in the free half.
//
// Its argument is always a live generation: despawn is the only caller and the
// value it steps is the one the entity is alive at.
func nextGeneration(g uint32) uint32 {
	g++
	if g&freeGeneration != 0 {
		return 1
	}
	return g
}
