package ecs

import (
	"fmt"
	"math"
	"math/bits"
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
	world *world[W]
}

// world is the kernel resource of world W: its entities and its stores.
type world[W WorldID] struct {
	entities     entityTable[W]
	stores       []*store
	storesByType map[reflect.Type]*store
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
	w := &world[W]{storesByType: map[reflect.Type]*store{}}
	w.entities.init(options.FirstIndex, last, options.Entities)
	configurator.InitResource(w)
	registry.worlds = append(registry.worlds, t)
}

// Declare declares the write on world W, the barrier.
func (World[W]) Declare(access *kernel.Access) World[W] {
	return World[W]{world: access.Write[*world[W]]().Get()}
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
	_, ok := w.world.storeOf(reflect.TypeFor[T]()).row(e)
	return ok
}

// Get returns e's T, or nil if it has none, for reading in place, as
// Store.Get does: writing through it goes unseen by observers.
func (w World[W]) Get[T any](e Entity) *T {
	s := w.world.storeOf(reflect.TypeFor[T]())
	row, ok := s.row(e)
	if !ok {
		return nil
	}
	return (*T)(s.address(row))
}

// Write returns e's T, or nil if it has none, for writing in place, and
// marks it changed for observers, as Store.Write does.
func (w World[W]) Write[T any](e Entity) *T {
	s := w.world.storeOf(reflect.TypeFor[T]())
	row, ok := s.row(e)
	if !ok {
		return nil
	}
	s.markChanged(row)
	return (*T)(s.address(row))
}

// Add gives e value as its T, replacing the one it has, and reports whether
// e is alive.
func (w World[W]) Add[T any](e Entity, value T) bool {
	s := w.world.storeOf(reflect.TypeFor[T]())
	row, ok := w.world.addComponent(e, s)
	if ok {
		*(*T)(s.address(row)) = value
	}
	return ok
}

// Remove removes e's T, and reports whether it had one.
func (w World[W]) Remove[T any](e Entity) bool {
	return w.world.removeComponent(e, w.world.storeOf(reflect.TypeFor[T]()))
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
	row, ok := s.row(e)
	if !ok {
		return nil
	}
	return unsafe.Slice((*byte)(s.address(row)), s.component.Size)
}

// WriteRaw returns e's component id as bytes, or nil if e has none, for
// writing in place, and marks it changed for observers.
func (w World[W]) WriteRaw(e Entity, id ComponentID) []byte {
	s := w.world.stores[id]
	row, ok := s.row(e)
	if !ok {
		return nil
	}
	s.markChanged(row)
	return unsafe.Slice((*byte)(s.address(row)), s.component.Size)
}

// AddRaw gives e value as its component id, replacing the one it has, and
// reports whether e is alive. It panics if value is not the component's
// Size long.
func (w World[W]) AddRaw(e Entity, id ComponentID, value []byte) bool {
	s := w.world.stores[id]
	if uintptr(len(value)) != s.component.Size {
		panic(fmt.Sprintf("ecs: adding %d bytes as component %v of %d", len(value), s.component.Type, s.component.Size))
	}
	row, ok := w.world.addComponent(e, s)
	if ok {
		copy(unsafe.Slice((*byte)(s.address(row)), s.component.Size), value)
	}
	return ok
}

// RemoveRaw removes e's component id, and reports whether it had one.
func (w World[W]) RemoveRaw(e Entity, id ComponentID) bool {
	return w.world.removeComponent(e, w.world.stores[id])
}

// despawn removes e and every component it has, and reports whether it was
// alive.
func (w *world[W]) despawn(e Entity) bool {
	mask, ok := w.entities.despawn(e)
	if !ok {
		return false
	}
	words := maskWords(&mask)
	for word := range words {
		for set := words[word]; set != 0; set &= set - 1 {
			s := w.stores[word<<6+bits.TrailingZeros64(set)]
			row, _ := s.row(e)
			s.remove(e, row)
		}
	}
	return true
}

// addComponent gives alive e a row in s, or marks the one it has changed,
// and returns it. It answers false if e is not alive.
func (w *world[W]) addComponent(e Entity, s *store) (int, bool) {
	if !w.entities.alive(e) {
		return 0, false
	}
	row, ok := s.row(e)
	if ok {
		s.markChanged(row)
		return row, true
	}
	row = s.add(e)
	w.entities.setComponent(e, s.component.ID)
	return row, true
}

// removeComponent removes e's row in s, and reports whether it had one.
func (w *world[W]) removeComponent(e Entity, s *store) bool {
	row, ok := s.row(e)
	if !ok {
		return false
	}
	s.remove(e, row)
	w.entities.unsetComponent(e, s.component.ID)
	return true
}

// checkValues panics unless values is as long as mask's components need.
func (w *world[W]) checkValues(mask *W, values []byte) {
	need := uintptr(0)
	words := maskWords(mask)
	for word := range words {
		for set := words[word]; set != 0; set &= set - 1 {
			id := word<<6 + bits.TrailingZeros64(set)
			if id >= len(w.stores) {
				panic(fmt.Sprintf("ecs: a mask names component %d, beyond the %d registered", id, len(w.stores)))
			}
			need += w.stores[id].component.Size
		}
	}
	if uintptr(len(values)) != need {
		panic(fmt.Sprintf("ecs: %d bytes of values for a mask needing %d", len(values), need))
	}
}

// addAll gives alive e the components in mask from values, laid out as for
// SpawnRaw and checked.
func (w *world[W]) addAll(e Entity, mask *W, values []byte) {
	words := maskWords(mask)
	for word := range words {
		for set := words[word]; set != 0; set &= set - 1 {
			s := w.stores[word<<6+bits.TrailingZeros64(set)]
			row, _ := w.addComponent(e, s)
			size := s.component.Size
			copy(unsafe.Slice((*byte)(s.address(row)), size), values[:size])
			values = values[size:]
		}
	}
}

// resetObservation ends the observation window: every observed store's
// shadows take their rows' values and its tail is dropped, and the entity
// table ends its window. The first reset turns observation on instead: each
// store with an observer starts observing.
func (w *world[W]) resetObservation() {
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
func registeredWorld[W WorldID](configurator *kernel.Configurator) *world[W] {
	w, ok := configurator.Resource[*world[W]]()
	if !ok {
		panic(fmt.Sprintf("ecs: world %v is not registered", reflect.TypeFor[W]()))
	}
	return w
}

// storeOf returns the store of component type t. A component not registered
// in the world is a programming error, and panics.
func (w *world[W]) storeOf(t reflect.Type) *store {
	s, ok := w.storesByType[t]
	if !ok {
		panic(fmt.Sprintf("ecs: component %v is not registered in world %v", t, reflect.TypeFor[W]()))
	}
	return s
}
