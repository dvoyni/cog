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

// CommandRefs records changes to world W's refs of T into command buffer
// Tag, applied by the buffer's CommandSink with its other records, so a
// producer can make, set and release refs beside the components holding
// them: a ref released with a buffered Despawn dies with its entity.
//
// It holds a read on the refs, the buffer and the world, so producers run in
// parallel with each other and with refs lookups, and refs writers wait.
// Each system taking it gets a lane of its own, as with CommandBuffer.
type CommandRefs[W WorldID, Tag any, T any] struct {
	lane *refsLaneOf[W, T]
}

// CommandKeyedRefs records updates and releases of world W's keyed refs of
// T into command buffer Tag, as CommandRefs does for plain refs. A Put is
// not recorded: two producers putting one key would take two refs for it.
type CommandKeyedRefs[W WorldID, Tag any, K comparable, T Keyed[K]] struct {
	lane *keyedRefsLaneOf[W, K, T]
}

// commandRecorder is the kernel resource behind command buffer Tag of world
// W: its lanes, in registration order.
type commandRecorder[W WorldID, Tag any] struct {
	world *worldResource[W]
	lanes []*commandLane
	// refsLanes holds the lanes of the systems taking CommandRefs, of any
	// refs type, in registration order.
	refsLanes []refsLane
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

// refsLane is a lane of ref records, whatever the refs' type, as the sink
// commits it.
type refsLane interface {
	// commit applies the lane's records and empties it, keeping its
	// capacity, and answers how many were dropped.
	commit() int
	// shrink releases the lane's chunks past its records and values.
	shrink()
}

// refsLaneOf is one producing system's part of a command buffer for refs of
// T: its records in order, and the values of its new refs until the commit.
type refsLaneOf[W WorldID, T any] struct {
	mutex   sync.Mutex
	refs    *refsResource[W, T]
	records container.Array[refsRecord[T]]
	// values holds the value of each new-ref record, in record order. It is
	// typed, so the GC sees the pointers a T holds.
	values container.Array[T]
}

type refsRecord[T any] struct {
	handle    Ref[T]
	operation commandOperation
}

// keyedRefsLaneOf is one producing system's part of a command buffer for
// keyed refs of T: its records in order, and the values of its updates
// until the commit.
type keyedRefsLaneOf[W WorldID, K comparable, T Keyed[K]] struct {
	mutex   sync.Mutex
	refs    *refsResource[W, T]
	keys    map[K]Ref[T]
	records container.Array[keyedRefsRecord[K, T]]
	// values holds the value of each update record, in record order.
	values container.Array[T]
}

// keyedRefsRecord is a record of keyed refs: the handle of a release, or
// the key of a release by key.
type keyedRefsRecord[K comparable, T any] struct {
	handle    Ref[T]
	key       K
	operation commandOperation
}

type commandOperation uint8

const (
	commandSpawn commandOperation = iota
	commandDespawn
	commandAdd
	commandSet
	commandRemove
	commandNewRef
	commandSetRef
	commandReleaseRef
	commandUpdateRef
	commandReleaseKey
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
	recorder := &commandRecorder[W, Tag]{world: w}
	w.shrinkers = append(w.shrinkers, recorder)
	configurator.InitResource(recorder)
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

// Declare declares a read on W's refs of T, on the buffer and on world W,
// and gives this system a lane of its own. It is a composition error, and
// panics, for the refs to be keyed: CommandKeyedRefs records them.
func (CommandRefs[W, Tag, T]) Declare(access *kernel.Access) CommandRefs[W, Tag, T] {
	access.Read[*worldResource[W]]()
	recorder := access.Read[*commandRecorder[W, Tag]]().Get()
	refs := access.Read[*refsResource[W, T]]().Get()
	if refs.keys != nil {
		panic(fmt.Sprintf("ecs: CommandRefs of keyed refs of %v in world %v: CommandKeyedRefs records them", reflect.TypeFor[T](), reflect.TypeFor[W]()))
	}
	lane := &refsLaneOf[W, T]{refs: refs}
	recorder.refsLanes = append(recorder.refsLanes, lane)
	return CommandRefs[W, Tag, T]{lane: lane}
}

// New records storing value in a new ref, and returns its handle at once,
// to record in a component. The handle reads nothing until the record is
// committed. It answers the zero Ref once the allocation range is used up.
func (c CommandRefs[W, Tag, T]) New(value T) Ref[T] {
	h := c.lane.refs.reserve()
	c.lane.record(commandNewRef, h, value)
	return h
}

// Set records storing value in ref h, which keeps its handle, as with
// Refs.Set.
func (c CommandRefs[W, Tag, T]) Set(h Ref[T], value T) {
	c.lane.record(commandSetRef, h, value)
}

// Release records freeing ref h, whatever becomes of the entity holding it.
func (c CommandRefs[W, Tag, T]) Release(h Ref[T]) {
	var none T
	c.lane.record(commandReleaseRef, h, none)
}

// Declare declares a read on W's keyed refs of T, on the buffer and on
// world W, and gives this system a lane of its own. It is a composition
// error, and panics, for the refs not to be keyed by K.
func (CommandKeyedRefs[W, Tag, K, T]) Declare(access *kernel.Access) CommandKeyedRefs[W, Tag, K, T] {
	access.Read[*worldResource[W]]()
	recorder := access.Read[*commandRecorder[W, Tag]]().Get()
	refs := access.Read[*refsResource[W, T]]().Get()
	lane := &keyedRefsLaneOf[W, K, T]{refs: refs, keys: keysOf[W, K](refs)}
	recorder.refsLanes = append(recorder.refsLanes, lane)
	return CommandKeyedRefs[W, Tag, K, T]{lane: lane}
}

// Update records storing value in the ref of its key, as with
// KeyedRefs.Update.
func (c CommandKeyedRefs[W, Tag, K, T]) Update(value T) {
	c.lane.record(keyedRefsRecord[K, T]{operation: commandUpdateRef}, value)
}

// Release records freeing ref h and its key.
func (c CommandKeyedRefs[W, Tag, K, T]) Release(h Ref[T]) {
	var none T
	c.lane.record(keyedRefsRecord[K, T]{handle: h, operation: commandReleaseRef}, none)
}

// ReleaseKey records freeing the ref of key.
func (c CommandKeyedRefs[W, Tag, K, T]) ReleaseKey(key K) {
	var none T
	c.lane.record(keyedRefsRecord[K, T]{key: key, operation: commandReleaseKey}, none)
}

// Declare declares the write on the buffer and on world W, the barrier.
func (CommandSink[W, Tag]) Declare(access *kernel.Access) CommandSink[W, Tag] {
	access.Write[*worldResource[W]]()
	return CommandSink[W, Tag]{recorder: access.Write[*commandRecorder[W, Tag]]().Get()}
}

// Commit applies every record and empties the lanes, keeping their
// capacity. Despawns of every lane come first, then the other records in
// lane order, which is registration order, and in the order recorded; the
// lanes of CommandRefs come last. Commits go through the same paths as
// direct writes, so observers see them.
//
// A record on an entity not alive, a Spawn of an entity that is not a
// reservation still waiting, or a release of a ref not alive, is dropped;
// Commit answers how many were.
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
	for _, lane := range s.recorder.refsLanes {
		dropped += lane.commit()
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

// record appends a record of h, with value for a new ref or a set.
func (l *refsLaneOf[W, T]) record(operation commandOperation, h Ref[T], value T) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if operation != commandReleaseRef {
		l.values.Append(value)
	}
	l.records.Append(refsRecord[T]{handle: h, operation: operation})
}

// commit stores each new ref, sets each set one and releases each released
// one, in the order recorded, and answers how many were dropped: a new ref
// past the allocation range, or a set or release of a ref not alive. A
// value is cleared from the lane as it is stored, so the lane keeps no
// pointer of a T alive.
func (l *refsLaneOf[W, T]) commit() int {
	l.refs.settleReservations()
	dropped := 0
	stored := 0
	var zero T
	for i := range l.records.Len() {
		record := l.records.At(i)
		if record.operation == commandReleaseRef {
			if !l.refs.release(record.handle) {
				dropped++
			}
			continue
		}
		value := l.values.At(stored)
		stored++
		if record.operation == commandNewRef {
			if record.handle != (Ref[T]{}) {
				l.refs.store(record.handle, *value)
			} else {
				dropped++
			}
		} else if slot := l.refs.slot(record.handle); slot != nil {
			slot.value = *value
			l.refs.markChanged(record.handle, slot)
		} else {
			dropped++
		}
		*value = zero
	}
	l.records.Resize(0)
	l.values.Resize(0)
	return dropped
}

// record appends record, with value for an update.
func (l *keyedRefsLaneOf[W, K, T]) record(record keyedRefsRecord[K, T], value T) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if record.operation == commandUpdateRef {
		l.values.Append(value)
	}
	l.records.Append(record)
}

// commit updates and releases, in the order recorded, and answers how many
// were dropped: an update or a release by key of a key with no ref, or a
// release of a ref not alive.
func (l *keyedRefsLaneOf[W, K, T]) commit() int {
	dropped := 0
	updated := 0
	var zero T
	var noKey K
	for i := range l.records.Len() {
		record := l.records.At(i)
		applied := false
		switch record.operation {
		case commandUpdateRef:
			value := l.values.At(updated)
			updated++
			applied = updateKeyed(l.refs, l.keys, *value)
			*value = zero
		case commandReleaseRef:
			applied = releaseKeyed(l.refs, l.keys, record.handle)
		case commandReleaseKey:
			h, ok := l.keys[record.key]
			applied = ok && releaseKeyed(l.refs, l.keys, h)
			record.key = noKey
		}
		if !applied {
			dropped++
		}
	}
	l.records.Resize(0)
	l.values.Resize(0)
	return dropped
}

// shrink releases the chunks past the lane's records and values.
func (l *keyedRefsLaneOf[W, K, T]) shrink() {
	l.records.Shrink()
	l.values.Shrink()
}

// shrink releases the chunks past each lane's records and values, which
// stay. It runs under the barrier.
func (r *commandRecorder[W, Tag]) shrink() {
	for _, lane := range r.lanes {
		lane.records.Shrink()
		lane.values.Shrink()
	}
	for _, lane := range r.refsLanes {
		lane.shrink()
	}
}

// shrink releases the chunks past the lane's records and values.
func (l *refsLaneOf[W, T]) shrink() {
	l.records.Shrink()
	l.values.Shrink()
}
