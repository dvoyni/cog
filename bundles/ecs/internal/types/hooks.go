package types

import (
	"fmt"
	"iter"
	"reflect"
	"sync"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// A Hook is one record of one act on one Component of an Entity, and Hooks is
// how a System reads them: bundles/ecs/docs/specs/hooks.md, which this file is
// judged against.
//
// The shape, in one paragraph. A Store some reader watches keeps one log,
// shared by every reader of that Component, each with its own place in it.
// Every record is appended under a lock the act already holds — write{*Store[T]}
// for UpdateFor and Remove.From — so no act reads a Store other than its own and
// no handler's lock set grows because a reader exists. A removal copies T's last
// value into the log; an addition copies nothing. A reader's run start takes the
// records since its place, keeps those its kind set delivers, folds each
// addition's value to the Entity's next removal, and fills what is still open
// from the live Store under its own read{*Store[T]}. Its run end compacts what
// every reader has passed and clears its copy.
//
// A Spawn and a Despawn append under write{*Entities}. A Spawn records on each
// watched Store it carries, through the Store's setter baked beside the plain
// one. A Despawn calls one capture per watched Store before the Stores are
// emptied, each probing its own Store and retaining T's last value.
//
// A change is recorded at the end of its writer's run, by comparing the bytes
// of each row the System was handed with write access against a copy taken
// before it could write them: hookchanged.go.

// hookKind is the set of facts one record carries, all relative to its
// Component. An act is recorded with every kind true of it.
type hookKind uint8

const (
	kindSpawned hookKind = 1 << iota
	kindDespawned
	kindAdded
	kindRemoved
	kindChanged
)

// The kinds whose being watched makes an act recorded: hooks.md § Which acts
// are recorded. A removal is also recorded wherever an addition or a change is
// watched, because it fixes their values.
const (
	recordsAddition = kindAdded | kindChanged
	recordsSpawn    = kindSpawned | recordsAddition
	recordsRemoval  = kindSpawned | kindAdded | kindChanged | kindRemoved
	// A Despawn is recorded whichever kind is watched, so it has no mask: every
	// Store with a log enrols a capture.
)

// KindSet is the closed set of types a Hooks parameter names to choose which
// records it is given. Its one method is unexported, so nothing outside this
// package can add a kind set.
type KindSet interface{ kinds() hookKind }

// The eight kind sets. A record is delivered when any of its kinds is in the
// set; "every addition" includes Spawns, and "every removal" Despawns.
type (
	// HookSpawned delivers Spawns carrying T.
	HookSpawned struct{}
	// HookDespawned delivers Despawns of Entities holding T.
	HookDespawned struct{}
	// HookSpawnedDespawned delivers both.
	HookSpawnedDespawned struct{}
	// HookAdded delivers every addition.
	HookAdded struct{}
	// HookRemoved delivers every removal, with T's last value.
	HookRemoved struct{}
	// HookAddedRemoved delivers every addition and every removal.
	HookAddedRemoved struct{}
	// HookAddedChanged delivers every addition and every change.
	HookAddedChanged struct{}
	// HookAll delivers every addition, change and removal.
	HookAll struct{}
)

func (HookSpawned) kinds() hookKind          { return kindSpawned }
func (HookDespawned) kinds() hookKind        { return kindDespawned }
func (HookSpawnedDespawned) kinds() hookKind { return kindSpawned | kindDespawned }
func (HookAdded) kinds() hookKind            { return kindAdded }
func (HookRemoved) kinds() hookKind          { return kindRemoved }
func (HookAddedRemoved) kinds() hookKind     { return kindAdded | kindRemoved }
func (HookAddedChanged) kinds() hookKind     { return kindAdded | kindChanged }
func (HookAll) kinds() hookKind {
	return kindSpawned | kindDespawned | kindAdded | kindRemoved | kindChanged
}

// Hook is one record as its reader is given it: every kind true of the act, and
// T's value. The kinds sit before Value, which is what lets the IsX methods
// inline on a generic *Hook[T] at the cost of a hand-written bit test.
//
// The pointer All yields is valid until the System's run ends, when the
// reader's copy is cleared. Value may be copied out and kept.
type Hook[T any] struct {
	kinds hookKind
	// Value is T as the record carries it: a removal's last value, taken at the
	// act; or, for an addition, T at the Entity's next removal, or at the
	// reader's run start if it has not been removed since.
	Value T
}

// IsSpawned reports whether the record is of a Spawn carrying T.
func (h *Hook[T]) IsSpawned() bool { return h.kinds&kindSpawned != 0 }

// IsDespawned reports whether the record is of a Despawn of an Entity holding T.
func (h *Hook[T]) IsDespawned() bool { return h.kinds&kindDespawned != 0 }

// IsAdded reports whether the act gave the Entity T.
func (h *Hook[T]) IsAdded() bool { return h.kinds&kindAdded != 0 }

// IsRemoved reports whether the act took T away.
func (h *Hook[T]) IsRemoved() bool { return h.kinds&kindRemoved != 0 }

// IsChanged reports whether the record gives the reader a value of T it has not
// seen. Every addition carries it.
func (h *Hook[T]) IsChanged() bool { return h.kinds&kindChanged != 0 }

// hookRecord is one act in a Store's log, 24 bytes: the full Entity, generation
// included, so a despawned Entity and the one reusing its index are never
// folded together; where a removal's copy of T sits among the log's retained
// values, or -1; the System whose run end recorded a change; and the act's
// kinds.
type hookRecord struct {
	e Entity
	// value is the absolute position of a removal's retained copy, counted
	// from the first value the log ever retained, so compaction moves no index.
	value int64
	// writer is the System that recorded a change, and 0 on every other record:
	// a reader skips the changes its own System recorded. It sits in what was
	// the record's padding.
	writer uint32
	kinds  hookKind
}

// hookRecords is the half of a Store's log that knows no T: the records, and
// the readers' places in them. hookLog[T] begins with it, so the erased Store
// header reaches it through the same pointer, which is how a Changed compare
// that knows its Store only as bytes appends to it.
type hookRecords struct {
	mu      sync.Mutex
	records []hookRecord
	// base is the absolute position of records[0].
	base uint64
	// places is each reader's absolute position: the first record its next run
	// start takes.
	places []*uint64
}

// changed records a change a writer's run end found in e's bytes. The caller
// holds the Store's write lock, and has checked the Store watches changes.
func (r *hookRecords) changed(e Entity, writer uint32) {
	r.records = append(r.records, hookRecord{e: e, value: -1, writer: writer, kinds: kindChanged})
}

// hookLog is one watched Store's record of acts. Writers append to records and
// retained under the Store's write lock and never touch mu. Readers hold the
// Store's read lock for their whole run, so no append lands while one reads;
// mu is what two readers of the same Store, running together, share while they
// fold and compact. It is never held by a writer or across a System's body.
type hookLog[T any] struct {
	// hookRecords is first, and must stay first: see hookRecords.
	hookRecords
	// retained is the removals' copies of T, and retainedBase the absolute
	// position of retained[0].
	retained     []T
	retainedBase int64
	// trivial is T's pointer-free answer. A retained copy of a non-trivial T is
	// zeroed when compaction drops it, so a string or a List it holds is
	// released.
	trivial bool
}

// added records an addition. The caller has checked the Store watches it.
func (l *hookLog[T]) added(e Entity, kinds hookKind) {
	l.records = append(l.records, hookRecord{e: e, value: -1, kinds: kinds})
}

// removed records a removal and retains T's last value, taken at the act. The
// caller has checked the Store watches it.
func (l *hookLog[T]) removed(e Entity, row *T, kinds hookKind) {
	l.retained = append(l.retained, *row)
	l.records = append(l.records, hookRecord{
		e: e, value: l.retainedBase + int64(len(l.retained)-1), kinds: kinds,
	})
}

// compact drops every record all readers have passed, and the retained values
// only those records named. Capacity is kept until ShrinkCmd cuts it, so steady
// state never allocates. Held under mu.
func (l *hookLog[T]) compact() {
	least := l.base + uint64(len(l.records))
	for _, place := range l.places {
		least = min(least, *place)
	}
	passed := int(least - l.base)
	if passed == 0 {
		return
	}
	firstKept := l.retainedBase + int64(len(l.retained))
	for i := passed; i < len(l.records); i++ {
		if l.records[i].value >= 0 {
			firstKept = l.records[i].value
			break
		}
	}
	kept := copy(l.retained, l.retained[firstKept-l.retainedBase:])
	if !l.trivial {
		clear(l.retained[kept:])
	}
	l.retained = l.retained[:kept]
	l.retainedBase = firstKept
	kept = copy(l.records, l.records[passed:])
	l.records = l.records[:kept]
	l.base = least
}

// logFor is the Store's log, created by the first reader that registers, which
// also enrols the Store's Despawn capture: a Despawn is recorded whichever kind
// is watched. A Store nobody reads has neither.
func (s *Store[T]) logFor(en *Entities) *hookLog[T] {
	if s.hooks == nil {
		s.hooks = &hookLog[T]{trivial: s.trivial}
		en.captures = append(en.captures, s.captureDespawn)
		en.enrolHooks(s.hooks.shrink)
	}
	return s.hooks
}

// shrink cuts the records and the retained values to their length, and reports
// the bytes let go. What no reader has passed is the length, so every record a
// reader has yet to take survives, with its value, at the same absolute
// position; compaction has already dropped the rest. An array already at its
// length is left as it is, so a second shrink releases nothing.
//
// Only ShrinkCmd calls it, holding write{*Entities}, which excludes every writer
// that appends and every reader that folds or compacts, so mu is not taken.
func (l *hookLog[T]) shrink() uintptr {
	before := l.bytes()
	l.records = clip(l.records)
	l.retained = clip(l.retained)
	return before - l.bytes()
}

// bytes is what the log's records and retained values hold, by capacity.
func (l *hookLog[T]) bytes() uintptr {
	return uintptr(cap(l.records))*unsafe.Sizeof(hookRecord{}) +
		uintptr(cap(l.retained))*unsafe.Sizeof(*new(T))
}

// hookGate is a writer handle's check of its Store's watched kinds. The Store's
// watched kinds are fixed when registration closes, and a writer may register
// before the reader that watches its Store, so the check is made when each run
// of the writer's System starts: one load and one bit test per handle per run.
// The act then tests on alone.
type hookGate struct {
	watch *hookKind
	mask  hookKind
	on    bool
}

func (g *hookGate) check() { g.on = *g.watch&g.mask != 0 }

// gated is a System parameter holding a hookGate.
type gated interface{ gate() *hookGate }

// spawnGate is a Spawn's check of the watched kinds of every Store its Component
// set carries: a hookGate per field, and on when any of them is, so a Spawn
// that records nowhere pays one test per New. fields shares its array with the
// Spawn's own.
type spawnGate struct {
	fields []spawnField
	on     bool
}

func (g *spawnGate) check() {
	on := false
	for i := range g.fields {
		field := &g.fields[i].hooks
		field.check()
		on = on || field.on
	}
	g.on = on
}

// spawn writes each Component of a Spawn some watched Store carries, recording
// the addition on each Store whose gate is on and on no other.
func (g *spawnGate) spawn(e Entity, staging unsafe.Pointer) {
	for i := range g.fields {
		field := &g.fields[i]
		if field.hooks.on {
			field.recorded(e, unsafe.Add(staging, field.offset))
		} else {
			field.set(e, unsafe.Add(staging, field.offset))
		}
	}
}

// spawnGated is a System parameter holding a spawnGate.
type spawnGated interface{ spawnGate() *spawnGate }

// runEdges is a System parameter with work at the start and end of its System's
// run.
type runEdges interface {
	beginRun()
	endRun()
}

// hookEntry is one delivered record in a reader's copy.
type hookEntry[T any] struct {
	e    Entity
	hook Hook[T]
}

// hookMark is a reader's fold state for one Entity index during one run start.
// It is current only while run and gen match, so nothing is cleared between
// runs. window says whether this copy has seen a record of the Entity yet, and
// whether the last one opened a window or closed it; entry is where the record
// that opened it sits in the copy, or -1 if it was not delivered, and fill where
// it sits in the list of values to fill from the live Store.
type hookMark struct {
	gen    uint32
	run    uint32
	entry  int32
	fill   int32
	window uint8
}

const (
	// windowUnseen: no record of the Entity yet in this copy, so a change is in
	// the window already open when the copy began.
	windowUnseen uint8 = iota
	// windowOpen: an addition, or a standalone change, is waiting for its value.
	windowOpen
	// windowClosed: a removal fixed the value of whatever was open.
	windowClosed
)

// Hooks is the System parameter: what happened to T since the System's last
// run, filtered by the kind set K. It declares read{*Entities} and
// read{*Store[T]}, a Query's lock set over T, and changes no other handler's.
//
//	func index(h *ecs.Hooks[Shape, ecs.HookAddedRemoved]) {
//	    for e, hook := range h.All() {
//	        if hook.IsAdded()   { tree.Insert(e, hook.Value) }
//	        if hook.IsRemoved() { tree.Remove(e) }
//	    }
//	}
type Hooks[T any, K KindSet] struct {
	log   *hookLog[T]
	store kernel.Read[*Store[T]]
	// place is this reader's position in the log, enrolled with it.
	place   uint64
	deliver hookKind
	// out is this run's copy, fixed at run start and cleared at run end; fills
	// indexes the entries whose value comes from the live Store, -1 where a
	// later removal fixed it instead. Both keep their capacity.
	out   []hookEntry[T]
	fills []int32
	marks []hookMark
	run   uint32
	// writer is this reader's System, whose own changes it skips.
	writer uint32
}

// prepare declares the locks, and adds K's kinds to the Store's watched kinds.
// It runs once, at registration, and every reader registers before any System
// runs, so a reader has its place in the log before the first act.
func (h *Hooks[T, K]) prepare(en *Entities, access kernel.ResourceAccess) {
	componentType := reflect.TypeFor[T]()
	class := en.classOf(componentType)
	if class == nil {
		panic(fmt.Sprintf("ecs: Hooks[%s, %s] names unregistered Component %s",
			kernel.TypeName(componentType), kernel.TypeName(reflect.TypeFor[K]()), kernel.TypeName(componentType)))
	}
	access.GetRead[*Entities]()
	h.store = access.GetRead[*Store[T]]()
	store := class.store.(*Store[T])
	var k K
	h.deliver = k.kinds()
	if validate && h.deliver&kindChanged != 0 {
		if where, bytes := implicitPadding(componentType); bytes > 0 {
			panic(fmt.Sprintf(
				"ecs: Hooks[%s, %s] watches %s for Changed, and %s has %d bytes of implicit padding %s: a change is a difference in bytes, and Go leaves padding holding whatever was there, so equal fields can record a Changed no field made; spell the padding out as an explicit _ [%d]byte field there, which keeps the same size and alignment",
				kernel.TypeName(componentType), kernel.TypeName(reflect.TypeFor[K]()), kernel.TypeName(componentType),
				kernel.TypeName(componentType), bytes, where, bytes))
		}
	}
	store.watch |= h.deliver
	h.log = store.logFor(en)
	h.place = h.log.base + uint64(len(h.log.records))
	h.log.places = append(h.log.places, &h.place)
	en.enrolHooks(h.release)
}

// ownedBy is told this reader's System when the System registers.
func (h *Hooks[T, K]) ownedBy(writer uint32) { h.writer = writer }

// All yields the records fixed at this run's start, in the order the acts
// happened. Nothing is netted: a removal then a re-addition is two records.
func (h *Hooks[T, K]) All() iter.Seq2[Entity, *Hook[T]] {
	return func(yield func(Entity, *Hook[T]) bool) {
		for i := range h.out {
			entry := &h.out[i]
			if !yield(entry.e, &entry.hook) {
				return
			}
		}
	}
}

// mark is e's fold state for this run start.
func (h *Hooks[T, K]) mark(e Entity) *hookMark {
	index := e.idx()
	for int(index) >= len(h.marks) {
		h.marks = append(h.marks, hookMark{})
	}
	m := &h.marks[index]
	if m.run != h.run || m.gen != e.gen() {
		*m = hookMark{gen: e.gen(), run: h.run, entry: -1, fill: -1}
	}
	return m
}

// beginRun takes the records since this reader's place, keeps those whose kinds
// meet K, folds each addition's or change's value to the Entity's next removal,
// and moves the place to the end of the log. It then fills every addition and
// change still open from the live Store, outside mu and under the System's
// read{*Store[T]}.
//
// A change folds into the window it falls in: into the addition that opened it,
// or, in the window already open when this copy began, into that window's first
// change, which is where the one standalone Changed is delivered. A change this
// reader's own System recorded is skipped, as if it were not in the log.
func (h *Hooks[T, K]) beginRun() {
	h.run++
	if h.run == 0 {
		clear(h.marks)
		h.run = 1
	}
	h.out, h.fills = h.out[:0], h.fills[:0]
	l := h.log
	l.mu.Lock()
	records := l.records[h.place-l.base:]
	for i := range records {
		r := &records[i]
		if r.kinds == kindChanged {
			if h.deliver&kindChanged == 0 || r.writer == h.writer {
				continue
			}
			m := h.mark(r.e)
			if m.window != windowUnseen {
				continue
			}
			m.window = windowOpen
			m.entry, m.fill = int32(len(h.out)), int32(len(h.fills))
			h.fills = append(h.fills, m.entry)
			h.out = append(h.out, hookEntry[T]{e: r.e, hook: Hook[T]{kinds: r.kinds}})
			continue
		}
		m := h.mark(r.e)
		if r.kinds&kindRemoved != 0 {
			retained := &l.retained[r.value-l.retainedBase]
			if m.window == windowOpen && m.entry >= 0 {
				h.out[m.entry].hook.Value = *retained
				h.fills[m.fill] = -1
			}
			m.window, m.entry, m.fill = windowClosed, -1, -1
			if r.kinds&h.deliver != 0 {
				h.out = append(h.out, hookEntry[T]{e: r.e, hook: Hook[T]{kinds: r.kinds, Value: *retained}})
			}
			continue
		}
		m.window, m.entry, m.fill = windowOpen, -1, -1
		if r.kinds&h.deliver != 0 {
			m.entry, m.fill = int32(len(h.out)), int32(len(h.fills))
			h.fills = append(h.fills, m.entry)
			h.out = append(h.out, hookEntry[T]{e: r.e, hook: Hook[T]{kinds: r.kinds}})
		}
	}
	h.place = l.base + uint64(len(l.records))
	l.mu.Unlock()

	store := h.store.Get()
	for _, entry := range h.fills {
		if entry < 0 {
			continue
		}
		out := &h.out[entry]
		if row, ok := store.probe(out.e); ok {
			out.hook.Value = store.dense[row]
		}
	}
}

// endRun compacts what every reader of the Store has passed, then clears this
// reader's copy, zeroing it first for a non-trivial T so the values it holds
// are released.
func (h *Hooks[T, K]) endRun() {
	l := h.log
	l.mu.Lock()
	l.compact()
	l.mu.Unlock()
	if !l.trivial {
		clear(h.out)
	}
	h.out = h.out[:0]
}

// release cuts this reader's copy and its fills to their length, which between
// runs is 0, and drops its fold marks, and reports the bytes let go. A mark is
// current only during the run start that wrote it, so between runs none of them
// is, and the next run start grows what it needs from nothing. A reader's place
// is in the log, not in the copy, so what it has yet to take is untouched.
//
// Only ShrinkCmd calls it, holding write{*Entities}, which excludes the System
// that owns this reader.
func (h *Hooks[T, K]) release() uintptr {
	before := h.bytes()
	h.out = clip(h.out)
	h.fills = clip(h.fills)
	h.marks = nil
	return before - h.bytes()
}

// bytes is what the reader's copy, fills and marks hold, by capacity.
func (h *Hooks[T, K]) bytes() uintptr {
	return uintptr(cap(h.out))*unsafe.Sizeof(hookEntry[T]{}) +
		uintptr(cap(h.fills))*unsafe.Sizeof(int32(0)) +
		uintptr(cap(h.marks))*unsafe.Sizeof(hookMark{})
}
