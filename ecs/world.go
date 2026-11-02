package ecs

import (
	"fmt"
	"math"
	"math/bits"
	"reflect"
	"sync/atomic"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/kernel/container"
)

// WorldID is the constraint over world IDs. A world ID is a named array of
// words, such as type Game [2]uint64, that names a world: every handle of
// the world takes it, as World[Game] or Query1[Game, Position]. Its values
// are also the world's component masks, so its size bounds how many
// component types the world holds: 64 per word.
type WorldID interface {
	~[1]uint64 | ~[2]uint64 | ~[4]uint64 | ~[8]uint64
}

// WorldOptions configures a world as RegisterWorld creates it.
type WorldOptions struct {
	// FirstIndex and LastIndex bound the allocation range, inclusive: the
	// indices the world hands out to new entities. Indices outside it are
	// only ever placed with CreateAt. A LastIndex of 0 means the top of the
	// 32-bit range.
	FirstIndex, LastIndex uint32
	// Entities is how many entities to prepare room for, from FirstIndex.
	Entities int
}

// World is the barrier of world W: a system taking it holds the write on the
// world, so nothing else of W runs alongside it. Spawning, despawning and
// placing entities happen through it.
type World[W WorldID] struct {
	world *world[W]
}

// world is the kernel resource of world W: its entities and its stores.
type world[W WorldID] struct {
	first, last  uint32
	entities     pages[entitySlot[W]]
	stores       []*store
	storesByType map[reflect.Type]*store

	// free holds the indices despawned and freed, oldest first; those before
	// freeHead are taken.
	free     container.Array[uint32]
	freeHead int
	// reserved counts the entities handed out since the free list was last
	// settled: the next free index first, then fresh indices from fresh.
	reserved atomic.Int64
	fresh    uint32
	// despawned holds the indices despawned in this observation window,
	// freed at the next observation reset so none is reused within it.
	despawned container.Array[uint32]
}

// entitySlot is the entity table's entry for one index.
type entitySlot[W WorldID] struct {
	// live is the generation of the entity alive at this index, or 0.
	live uint32
	// generation is the last generation handed out at this index.
	generation uint32
	// mask holds the components the entity has, one bit per ComponentID.
	mask W
}

// RegisterWorld registers world W, named by its world ID, as a kernel
// resource. It is called in a plugin's Init, given after the ECS plugin.
//
// It is a composition error, and panics, for the ECS plugin not to be
// initialized yet, for W to be registered twice, or for the allocation range
// to be empty.
func RegisterWorld[W WorldID](configurator *kernel.Configurator, options WorldOptions) {
	t := reflect.TypeFor[W]()
	registry, ok := configurator.Resource[*registry]()
	if !ok {
		panic(fmt.Sprintf("ecs: world %v is registered before the ecs plugin", t))
	}
	if _, ok := configurator.Resource[*world[W]](); ok {
		panic(fmt.Sprintf("ecs: world %v is registered twice", t))
	}
	last := options.LastIndex
	if last == 0 {
		last = math.MaxUint32
	}
	if last < options.FirstIndex {
		panic(fmt.Sprintf("ecs: world %v has an empty allocation range", t))
	}
	w := &world[W]{
		first:        options.FirstIndex,
		last:         last,
		fresh:        options.FirstIndex,
		storesByType: map[reflect.Type]*store{},
	}
	for i := range options.Entities {
		w.entities.make(options.FirstIndex + uint32(i))
	}
	configurator.InitResource(w)
	registry.worlds = append(registry.worlds, t)
}

// Declare declares the write on world W, the barrier.
func (World[W]) Declare(access *kernel.Access) World[W] {
	return World[W]{world: access.Write[*world[W]]().Get()}
}

// Alive reports whether e is alive.
func (w World[W]) Alive(e Entity) bool {
	return w.world.alive(e)
}

// Spawn makes a new entity, with no components, and returns it. It answers
// NoEntity once the allocation range is used up.
func (w World[W]) Spawn() Entity {
	e := w.world.reserve()
	if e != NoEntity {
		w.world.place(e)
	}
	return e
}

// CreateAt makes e alive at its index and generation, for mirrors and for
// loading a save. e's index must lie outside the allocation range and hold
// no live entity.
func (w World[W]) CreateAt(e Entity) error {
	if e.generation() == 0 {
		return fmt.Errorf("ecs: creating entity %#x, whose generation is 0", uint64(e))
	}
	if index := e.index(); index >= w.world.first && index <= w.world.last {
		return fmt.Errorf("ecs: creating entity %#x inside the allocation range", uint64(e))
	}
	if slot := w.world.entities.at(e.index()); slot != nil && slot.live != 0 {
		return fmt.Errorf("ecs: creating entity %#x where one is alive", uint64(e))
	}
	w.world.place(e)
	return nil
}

// Despawn removes e and every component it has, and reports whether it was
// alive.
func (w World[W]) Despawn(e Entity) bool {
	if !w.world.alive(e) {
		return false
	}
	slot := w.world.entities.at(e.index())
	words := maskWords(&slot.mask)
	for word := range words {
		for set := words[word]; set != 0; set &= set - 1 {
			s := w.world.stores[word<<6+bits.TrailingZeros64(set)]
			row, _ := s.row(e)
			s.remove(e, row)
		}
	}
	slot.mask = *new(W)
	slot.live = 0
	w.world.despawned.Append(e.index())
	return true
}

// alive reports whether e is alive. It is one load and one compare.
func (w *world[W]) alive(e Entity) bool {
	slot := w.entities.at(e.index())
	return slot != nil && slot.live == e.generation() && slot.live != 0
}

// reserve takes the next entity to spawn: the oldest free index, else the
// next fresh one. It answers NoEntity once the allocation range is used up.
// Concurrent reserves take distinct entities; nothing else may change the
// world meanwhile.
func (w *world[W]) reserve() Entity {
	n := w.reserved.Add(1) - 1
	pending := int64(w.free.Len() - w.freeHead)
	if n < pending {
		index := *w.free.At(w.freeHead + int(n))
		generation := uint32(0)
		if slot := w.entities.at(index); slot != nil {
			generation = slot.generation
		}
		return makeEntity(index, nextGeneration(generation))
	}
	fresh := int64(w.fresh) + n - pending
	if fresh > int64(w.last) {
		return NoEntity
	}
	return makeEntity(uint32(fresh), 1)
}

// place makes e alive at its index and generation.
func (w *world[W]) place(e Entity) {
	slot := w.entities.make(e.index())
	slot.generation = e.generation()
	slot.live = e.generation()
}

// registeredWorld returns world W, panicking if it is not registered.
func registeredWorld[W WorldID](configurator *kernel.Configurator) *world[W] {
	w, ok := configurator.Resource[*world[W]]()
	if !ok {
		panic(fmt.Sprintf("ecs: world %v is not registered", reflect.TypeFor[W]()))
	}
	return w
}
