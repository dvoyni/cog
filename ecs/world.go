package ecs

import (
	"fmt"
	"math"
	"reflect"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
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
	world *worldResource[W]
}

// WorldLookup reads world W: whether an entity is alive, and the components
// the world holds. It holds a read on the world, so it runs alongside every
// handle of W but the barrier. Stores have locks of their own, so it reaches
// no component: a Lookup or a query reads them.
type WorldLookup[W WorldID] struct {
	world *worldResource[W]
}

// worldResource is the kernel resource of world W: its entities and its
// stores.
type worldResource[W WorldID] struct {
	entities     entityTable[W]
	stores       []*storeResource
	storesByType map[reflect.Type]*storeResource
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
	if _, ok := configurator.Resource[*worldResource[W]](); ok {
		panic(fmt.Sprintf("ecs: world %v is registered twice", t))
	}
	last := options.LastIndex
	if last == 0 {
		last = math.MaxUint32
	}
	if last < options.FirstIndex {
		panic(fmt.Sprintf("ecs: world %v has an empty allocation range", t))
	}
	w := &worldResource[W]{storesByType: map[reflect.Type]*storeResource{}}
	w.entities.init(options.FirstIndex, last, options.Entities)
	configurator.InitResource(w)
	registry.worlds = append(registry.worlds, t)
}

// Declare declares the write on world W, the barrier.
func (World[W]) Declare(access *kernel.Access) World[W] {
	return World[W]{world: access.Write[*worldResource[W]]().Get()}
}

// Alive reports whether e is alive.
func (w World[W]) Alive(e Entity) bool {
	return w.world.entities.alive(e)
}

// Spawn makes a new entity, with no components, and returns it. It answers
// NoEntity once the allocation range is used up.
func (w World[W]) Spawn() Entity {
	return w.world.entities.spawn()
}

// CreateAt makes e alive at its index and generation, for mirrors and for
// loading a save. e's index must lie outside the allocation range and hold
// no live entity.
func (w World[W]) CreateAt(e Entity) error {
	return w.world.entities.place(e)
}

// Despawn removes e and every component it has, and reports whether it was
// alive.
func (w World[W]) Despawn(e Entity) bool {
	return w.world.despawn(e)
}

// ComponentID answers T's ComponentID in the world, for typed code reaching
// the raw methods. A T not registered is a programming error, and panics.
func (w World[W]) ComponentID[T any]() ComponentID {
	return w.world.storeOf(reflect.TypeFor[T]()).component.ID
}

// Has reports whether e has a T.
//
// The typed methods of World find T's store by its type on each call, which
// allocates nothing but costs a map lookup: they suit work under the barrier,
// such as building a level. A loop over many entities is faster with a
// query or a Store, which bind the store once, at registration.
func (w World[W]) Has[T any](e Entity) bool {
	return w.world.storeOf(reflect.TypeFor[T]()).get(e) != nil
}

// Get returns e's T, or nil if it has none, for reading in place, as
// Store.Get does: writing through it goes unseen by observers.
func (w World[W]) Get[T any](e Entity) *T {
	return (*T)(w.world.storeOf(reflect.TypeFor[T]()).get(e))
}

// Write returns e's T, or nil if it has none, for writing in place, and
// marks it changed for observers, as Store.Write does.
func (w World[W]) Write[T any](e Entity) *T {
	return (*T)(w.world.storeOf(reflect.TypeFor[T]()).write(e))
}

// Add gives e given as its T, replacing the one it has, and reports whether
// e is alive.
func (w World[W]) Add[T any](e Entity, given T) bool {
	value := w.world.put(e, w.world.storeOf(reflect.TypeFor[T]()))
	if value != nil {
		*(*T)(value) = given
	}
	return value != nil
}

// Remove removes e's T, and reports whether it had one.
func (w World[W]) Remove[T any](e Entity) bool {
	return w.world.remove(e, w.world.storeOf(reflect.TypeFor[T]()))
}

// SpawnRaw makes a new entity with the components in mask and returns it,
// observed as one spawn. values holds each component's bytes, in ascending
// ComponentID order, each its component's Size long. It answers NoEntity
// once the allocation range is used up. It panics if values is not as long
// as mask needs, or mask names a component not registered.
func (w World[W]) SpawnRaw(mask W, values []byte) Entity {
	w.world.checkValues(&mask, values)
	e := w.Spawn()
	if e != NoEntity {
		w.world.addAll(e, &mask, values)
	}
	return e
}

// CreateAtRaw is CreateAt that also gives e the components in mask, with
// values laid out as for SpawnRaw.
func (w World[W]) CreateAtRaw(e Entity, mask W, values []byte) error {
	w.world.checkValues(&mask, values)
	if err := w.CreateAt(e); err != nil {
		return err
	}
	w.world.addAll(e, &mask, values)
	return nil
}

// ComponentCount answers how many component types the world holds.
func (w World[W]) ComponentCount() int {
	return len(w.world.stores)
}

// Component describes component id, which must be below ComponentCount.
func (w World[W]) Component(id ComponentID) ComponentInfo {
	return w.world.stores[id].component
}

// ComponentMask answers the components e has, one bit per ComponentID, or
// false if e is not alive.
func (w World[W]) ComponentMask(e Entity) (W, bool) {
	return w.world.entities.mask(e)
}

// ReadRaw returns e's component id as bytes, or nil if e has none, for
// reading in place. The slice is valid as Store.Get's pointer is.
func (w World[W]) ReadRaw(e Entity, id ComponentID) []byte {
	s := w.world.stores[id]
	value := s.get(e)
	if value == nil {
		return nil
	}
	return unsafe.Slice((*byte)(value), s.component.Size)
}

// WriteRaw returns e's component id as bytes, or nil if e has none, for
// writing in place, and marks it changed for observers.
func (w World[W]) WriteRaw(e Entity, id ComponentID) []byte {
	s := w.world.stores[id]
	value := s.write(e)
	if value == nil {
		return nil
	}
	return unsafe.Slice((*byte)(value), s.component.Size)
}

// AddRaw gives e given as its component id, replacing the one it has, and
// reports whether e is alive. It panics if given is not the component's
// Size long.
func (w World[W]) AddRaw(e Entity, id ComponentID, given []byte) bool {
	s := w.world.stores[id]
	if uintptr(len(given)) != s.component.Size {
		panic(fmt.Sprintf("ecs: adding %d bytes as component %v of %d", len(given), s.component.Type, s.component.Size))
	}
	value := w.world.put(e, s)
	if value != nil {
		copy(unsafe.Slice((*byte)(value), s.component.Size), given)
	}
	return value != nil
}

// RemoveRaw removes e's component id, and reports whether it had one.
func (w World[W]) RemoveRaw(e Entity, id ComponentID) bool {
	return w.world.remove(e, w.world.stores[id])
}

// Declare declares a read on world W.
func (WorldLookup[W]) Declare(access *kernel.Access) WorldLookup[W] {
	return WorldLookup[W]{world: access.Read[*worldResource[W]]().Get()}
}

// Alive reports whether e is alive. An entity held in a component may have
// died since it was stored: its generation no longer matches, so it reads as
// dead even once its index is reused.
func (l WorldLookup[W]) Alive(e Entity) bool {
	return l.world.entities.alive(e)
}

// ComponentID answers T's ComponentID in the world, as World.ComponentID
// does.
func (l WorldLookup[W]) ComponentID[T any]() ComponentID {
	return l.world.storeOf(reflect.TypeFor[T]()).component.ID
}

// ComponentCount answers how many component types the world holds.
func (l WorldLookup[W]) ComponentCount() int {
	return len(l.world.stores)
}

// Component describes component id, which must be below ComponentCount.
func (l WorldLookup[W]) Component(id ComponentID) ComponentInfo {
	return l.world.stores[id].component
}

// despawn removes e and every component it has, and reports whether it was
// alive.
func (w *worldResource[W]) despawn(e Entity) bool {
	mask, ok := w.entities.despawn(e)
	if !ok {
		return false
	}
	for id := range maskIDs(&mask) {
		s := w.stores[id]
		row, _ := s.row(e)
		s.remove(e, row)
	}
	return true
}

// put gives alive e a row in s, or marks the one it has changed, and
// returns the address of its value, or nil if e is not alive.
func (w *worldResource[W]) put(e Entity, s *storeResource) unsafe.Pointer {
	if !w.entities.alive(e) {
		return nil
	}
	if value := s.write(e); value != nil {
		return value
	}
	row := s.add(e)
	w.entities.setComponent(e, s.component.ID)
	return s.address(row)
}

// remove removes e's row in s, and reports whether it had one.
func (w *worldResource[W]) remove(e Entity, s *storeResource) bool {
	row, ok := s.row(e)
	if !ok {
		return false
	}
	s.remove(e, row)
	w.entities.unsetComponent(e, s.component.ID)
	return true
}

// checkValues panics unless values is as long as mask's components need.
func (w *worldResource[W]) checkValues(mask *W, values []byte) {
	need := uintptr(0)
	for id := range maskIDs(mask) {
		if int(id) >= len(w.stores) {
			panic(fmt.Sprintf("ecs: a mask names component %d, beyond the %d registered", id, len(w.stores)))
		}
		need += w.stores[id].component.Size
	}
	if uintptr(len(values)) != need {
		panic(fmt.Sprintf("ecs: %d bytes of values for a mask needing %d", len(values), need))
	}
}

// addAll gives alive e the components in mask from values, laid out as for
// SpawnRaw and checked.
func (w *worldResource[W]) addAll(e Entity, mask *W, values []byte) {
	for id := range maskIDs(mask) {
		s := w.stores[id]
		size := s.component.Size
		copy(unsafe.Slice((*byte)(w.put(e, s)), size), values[:size])
		values = values[size:]
	}
}

// resetObservation ends the observation window: every observed store's
// shadows take their rows' values and its tail is dropped, and the entity
// table ends its window. The first reset turns observation on instead: each
// store with an observer starts observing.
func (w *worldResource[W]) resetObservation() {
	if w.entities.endWindow() {
		for _, s := range w.stores {
			s.startObservation()
		}
		return
	}
	for _, s := range w.stores {
		s.resetObservation()
	}
}

// registeredWorld returns world W, panicking if it is not registered.
func registeredWorld[W WorldID](configurator *kernel.Configurator) *worldResource[W] {
	w, ok := configurator.Resource[*worldResource[W]]()
	if !ok {
		panic(fmt.Sprintf("ecs: world %v is not registered", reflect.TypeFor[W]()))
	}
	return w
}

// storeOf returns the store of component type t. A component not registered
// in the world is a programming error, and panics.
func (w *worldResource[W]) storeOf(t reflect.Type) *storeResource {
	s, ok := w.storesByType[t]
	if !ok {
		panic(fmt.Sprintf("ecs: component %v is not registered in world %v", t, reflect.TypeFor[W]()))
	}
	return s
}
