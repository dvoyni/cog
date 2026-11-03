package ecs

import (
	"fmt"
	"reflect"
	"sync"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/kernel/container"
)

// CommandBuffer records changes to world W into command buffer Tag, to be
// applied together by a CommandSink of the same W and Tag, so a system that
// cannot hold the barrier can still spawn, despawn, add and remove.
//
// It holds a read on the buffer and on the world, so producers run in
// parallel. Each system taking it gets a lane of its own at registration and
// records there; two jobs of one read-only system share their lane, and
// their records interleave.
type CommandBuffer[W WorldID, Tag any] struct {
	recorder *commandRecorder[W, Tag]
	lane     *commandLane
}

// CommandSink applies command buffer Tag of world W. It holds the write on
// the buffer and the barrier, so any record, structural or not, can be
// applied.
type CommandSink[W WorldID, Tag any] struct {
	recorder *commandRecorder[W, Tag]
}

// commandRecorder is the kernel resource behind command buffer Tag of world
// W: its lanes, in registration order.
type commandRecorder[W WorldID, Tag any] struct {
	world *worldResource[W]
	lanes []*commandLane
}

// commandLane is one producing system's part of a command buffer. Records
// are appended in order; each value starts at a word of values, so it is
// copied out a word at a time.
type commandLane struct {
	mutex   sync.Mutex
	records container.Array[commandRecord]
	values  container.Array[uint64]
}

type commandRecord struct {
	entity    Entity
	operation commandOperation
	component ComponentID
	// valueStart is the record's first word in the lane's values.
	valueStart int
}

type commandOperation uint8

const (
	commandSpawn commandOperation = iota
	commandDespawn
	commandAdd
	commandSet
	commandRemove
)

// RegisterCommandBuffer registers command buffer Tag of world W, a kernel
// resource of its own. Tag is any type naming the buffer: a world may have
// several, so producers of unrelated work do not share lanes, and each is
// committed where it is needed. It is called in a plugin's Init, after W is
// registered.
//
// It is a composition error, and panics, for W not to be registered or for
// the buffer to be registered twice.
func RegisterCommandBuffer[W WorldID, Tag any](configurator *kernel.Configurator) {
	w := registeredWorld[W](configurator)
	if _, ok := configurator.Resource[*commandRecorder[W, Tag]](); ok {
		panic(fmt.Sprintf("ecs: command buffer %v of world %v is registered twice", reflect.TypeFor[Tag](), reflect.TypeFor[W]()))
	}
	configurator.InitResource(&commandRecorder[W, Tag]{world: w})
}

// Declare declares a read on the buffer and on world W, and gives this
// system a lane of its own.
func (CommandBuffer[W, Tag]) Declare(access *kernel.Access) CommandBuffer[W, Tag] {
	access.Read[*worldResource[W]]()
	recorder := access.Read[*commandRecorder[W, Tag]]().Get()
	lane := &commandLane{}
	recorder.lanes = append(recorder.lanes, lane)
	return CommandBuffer[W, Tag]{recorder: recorder, lane: lane}
}

// Reserve takes an entity to spawn with a Spawn record. It is not alive
// until that record is committed; in an observing world it must be before
// the next observation reset, which takes back a reservation not spawned.
// It answers NoEntity once the allocation range is used up.
func (c CommandBuffer[W, Tag]) Reserve() Entity {
	return c.recorder.world.entities.reserve()
}

// Spawn records making reserved entity e alive, with no components.
func (c CommandBuffer[W, Tag]) Spawn(e Entity) {
	c.lane.record(commandSpawn, e, 0, nil)
}

// Despawn records removing e and every component it has.
func (c CommandBuffer[W, Tag]) Despawn(e Entity) {
	c.lane.record(commandDespawn, e, 0, nil)
}

// Add records giving e value as its T, replacing the one it has.
func (c CommandBuffer[W, Tag]) Add[T any](e Entity, value T) {
	id := c.recorder.world.storeOf(reflect.TypeFor[T]()).component.ID
	c.lane.record(commandAdd, e, id, unsafe.Slice((*byte)(unsafe.Pointer(&value)), unsafe.Sizeof(value)))
}

// Set records replacing e's T with value. It does nothing if e has no T.
func (c CommandBuffer[W, Tag]) Set[T any](e Entity, value T) {
	id := c.recorder.world.storeOf(reflect.TypeFor[T]()).component.ID
	c.lane.record(commandSet, e, id, unsafe.Slice((*byte)(unsafe.Pointer(&value)), unsafe.Sizeof(value)))
}

// Remove records removing e's T. It does nothing if e has no T.
func (c CommandBuffer[W, Tag]) Remove[T any](e Entity) {
	id := c.recorder.world.storeOf(reflect.TypeFor[T]()).component.ID
	c.lane.record(commandRemove, e, id, nil)
}

// Declare declares the write on the buffer and on world W, the barrier.
func (CommandSink[W, Tag]) Declare(access *kernel.Access) CommandSink[W, Tag] {
	access.Write[*worldResource[W]]()
	return CommandSink[W, Tag]{recorder: access.Write[*commandRecorder[W, Tag]]().Get()}
}

// Commit applies every record and empties the lanes, keeping their
// capacity. Despawns of every lane come first, then the other records in
// lane order, which is registration order, and in the order recorded.
// Commits go through the same paths as direct writes, so observers see
// them.
//
// A record on an entity not alive, or a Spawn of an entity that is not a
// reservation still waiting, is dropped; Commit answers how many were.
func (s CommandSink[W, Tag]) Commit() int {
	w := s.recorder.world
	dropped := 0
	for _, lane := range s.recorder.lanes {
		for i := range lane.records.Len() {
			if record := lane.records.At(i); record.operation == commandDespawn && !w.despawn(record.entity) {
				dropped++
			}
		}
	}
	for _, lane := range s.recorder.lanes {
		for i := range lane.records.Len() {
			if !s.apply(lane, lane.records.At(i)) {
				dropped++
			}
		}
		lane.records.Resize(0)
		lane.values.Resize(0)
	}
	return dropped
}

// apply applies record, a despawn already applied, and reports whether its
// entity could take it.
func (s CommandSink[W, Tag]) apply(lane *commandLane, record *commandRecord) bool {
	w := s.recorder.world
	switch record.operation {
	case commandSpawn:
		return w.entities.spawnReserved(record.entity)
	case commandAdd:
		store := w.stores[record.component]
		value := w.put(record.entity, store)
		if value != nil {
			lane.copyValue(record.valueStart, value, store.component.Size)
		}
		return value != nil
	case commandSet:
		if !w.entities.alive(record.entity) {
			return false
		}
		store := w.stores[record.component]
		if value := store.write(record.entity); value != nil {
			lane.copyValue(record.valueStart, value, store.component.Size)
		}
		return true
	case commandRemove:
		if !w.entities.alive(record.entity) {
			return false
		}
		w.remove(record.entity, w.stores[record.component])
		return true
	}
	return true
}

// record appends a record, its value copied in from the next word.
func (l *commandLane) record(operation commandOperation, e Entity, id ComponentID, value []byte) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	start := l.values.Len()
	l.values.Resize(start + (len(value)+7)/8)
	for word := start; len(value) > 0; word++ {
		value = value[copy(unsafe.Slice((*byte)(unsafe.Pointer(l.values.At(word))), 8), value):]
	}
	l.records.Append(commandRecord{entity: e, operation: operation, component: id, valueStart: start})
}

// copyValue copies size bytes of the value from word start to destination.
func (l *commandLane) copyValue(start int, destination unsafe.Pointer, size uintptr) {
	to := unsafe.Slice((*byte)(destination), size)
	for word := start; len(to) > 0; word++ {
		to = to[copy(to, unsafe.Slice((*byte)(unsafe.Pointer(l.values.At(word))), 8)):]
	}
}
