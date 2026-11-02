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
	// firstIndex and lastIndex bound the allocation range, inclusive.
	firstIndex, lastIndex uint32
	entitySlots           indexTable[entitySlot[W]]
	stores                []*store
	storesByType          map[reflect.Type]*store

	// freeEntities holds the entities to hand out next, oldest first: each
	// is a freed index in the allocation range at its next generation.
	freeEntities container.Array[Entity]
	// reservationCount counts the entities handed out since reservations
	// were last settled: the free entities first, then fresh indices from
	// nextFreshIndex.
	reservationCount atomic.Int64
	// nextFreshIndex is the first index in the allocation range never handed
	// out. It is one past lastIndex once the range is used up.
	nextFreshIndex int64
	// observationEnabled is set by the first observation reset. Until then
	// the world keeps nothing for observers: a despawned index is freed at
	// once, and a removed row is dropped.
	observationEnabled bool
	// despawnedIndices holds the indices despawned in this observation
	// window, freed at the next observation reset so none is reused within
	// it. It stays empty while observation is off.
	despawnedIndices container.Array[uint32]
}

// entitySlot is the entity table's entry for one index.
type entitySlot[W WorldID] struct {
	// liveGeneration is the generation of the entity alive at this index, or
	// 0 if none is.
	liveGeneration uint32
	// lastGeneration is the last generation placed at this index.
	lastGeneration uint32
	// componentMask holds the components the entity has, one bit per
	// ComponentID.
	componentMask W
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
		firstIndex:     options.FirstIndex,
		lastIndex:      last,
		nextFreshIndex: int64(options.FirstIndex),
		storesByType:   map[reflect.Type]*store{},
	}
	for i := range options.Entities {
		w.entitySlots.atOrCreate(options.FirstIndex + uint32(i))
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
	if !w.world.observationEnabled {
		w.world.settleReservations(false)
	}
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
	if index := e.index(); index >= w.world.firstIndex && index <= w.world.lastIndex {
		return fmt.Errorf("ecs: creating entity %#x inside the allocation range", uint64(e))
	}
	if slot := w.world.entitySlots.at(e.index()); slot != nil && slot.liveGeneration != 0 {
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
	slot := w.world.entitySlots.at(e.index())
	words := maskWords(&slot.componentMask)
	for word := range words {
		for set := words[word]; set != 0; set &= set - 1 {
			s := w.world.stores[word<<6+bits.TrailingZeros64(set)]
			row, _ := s.row(e)
			s.remove(e, row)
		}
	}
	slot.componentMask = *new(W)
	slot.liveGeneration = 0
	if w.world.observationEnabled {
		w.world.despawnedIndices.Append(e.index())
	} else {
		w.world.settleReservations(false)
		w.world.free(e.index())
	}
	return true
}

// alive reports whether e is alive. It is one load and one compare.
func (w *world[W]) alive(e Entity) bool {
	slot := w.entitySlots.at(e.index())
	return slot != nil && slot.liveGeneration == e.generation() && slot.liveGeneration != 0
}

// reserve takes the next entity to spawn: the oldest free index, else the
// next fresh one. It answers NoEntity once the allocation range is used up.
// Concurrent reserves take distinct entities; nothing else may change the
// world meanwhile.
func (w *world[W]) reserve() Entity {
	reservation := w.reservationCount.Add(1) - 1
	freeCount := int64(w.freeEntities.Len())
	if reservation < freeCount {
		return *w.freeEntities.At(int(reservation))
	}
	index := w.nextFreshIndex + reservation - freeCount
	if index > int64(w.lastIndex) {
		return NoEntity
	}
	return makeEntity(uint32(index), 1)
}

// place makes e alive at its index and generation.
func (w *world[W]) place(e Entity) {
	slot := w.entitySlots.atOrCreate(e.index())
	slot.lastGeneration = e.generation()
	slot.liveGeneration = e.generation()
}

// settleReservations folds the reservations taken since they were last
// settled into the free list and nextFreshIndex, so the free list starts at
// the next entity to hand out. It runs under the barrier, where no
// reservation is being taken.
//
// With reclaim, a reservation never spawned goes back to the back of the
// free list with its generation burned, so the stale entity never comes
// alive. Only the observation reset reclaims: it is the one point where no
// reservation of the window still waits for its commit. Without reclaim,
// such an index is lost.
func (w *world[W]) settleReservations(reclaim bool) {
	reservations := int(w.reservationCount.Load())
	if reservations == 0 {
		return
	}
	takenFree := min(reservations, w.freeEntities.Len())
	takenFresh := min(int64(reservations-takenFree), int64(w.lastIndex)+1-w.nextFreshIndex)
	if reclaim {
		for i := range takenFree {
			reserved := *w.freeEntities.At(i)
			if slot := w.entitySlots.at(reserved.index()); slot == nil || slot.lastGeneration != reserved.generation() {
				w.freeEntities.Append(makeEntity(reserved.index(), nextGeneration(reserved.generation())))
			}
		}
		for i := range takenFresh {
			index := uint32(w.nextFreshIndex + i)
			if slot := w.entitySlots.at(index); slot == nil || slot.lastGeneration == 0 {
				w.freeEntities.Append(makeEntity(index, 2))
			}
		}
	}
	w.nextFreshIndex += takenFresh
	remainingFree := w.freeEntities.Len() - takenFree
	for i := range remainingFree {
		*w.freeEntities.At(i) = *w.freeEntities.At(takenFree + i)
	}
	w.freeEntities.Resize(remainingFree)
	w.reservationCount.Store(0)
}

// free puts a despawned index at the back of the free list, at its next
// generation, if it lies in the allocation range. Reservations are settled.
func (w *world[W]) free(index uint32) {
	if index >= w.firstIndex && index <= w.lastIndex {
		w.freeEntities.Append(makeEntity(index, nextGeneration(w.entitySlots.at(index).lastGeneration)))
	}
}

// resetObservation ends the observation window: every observed store's
// shadows take their rows' values and its tail is dropped, reservations are
// settled and reclaimed, and the window's despawned indices are freed in
// despawn order. The first reset turns observation on instead: each store
// with an observer starts observing, every row it holds counting as added
// in the first window.
func (w *world[W]) resetObservation() {
	if !w.observationEnabled {
		w.settleReservations(false)
		w.observationEnabled = true
		for _, s := range w.stores {
			if s.observerRegistered {
				s.startObservation()
			}
		}
		return
	}
	for _, s := range w.stores {
		s.resetObservation()
	}
	w.settleReservations(true)
	for i := range w.despawnedIndices.Len() {
		w.free(*w.despawnedIndices.At(i))
	}
	w.despawnedIndices.Resize(0)
}

// registeredWorld returns world W, panicking if it is not registered.
func registeredWorld[W WorldID](configurator *kernel.Configurator) *world[W] {
	w, ok := configurator.Resource[*world[W]]()
	if !ok {
		panic(fmt.Sprintf("ecs: world %v is not registered", reflect.TypeFor[W]()))
	}
	return w
}
