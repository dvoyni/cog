package types

// PROTOTYPE, throwaway. Branch proto/ecs-hooks, for "Where a Hook log lives, and
// what recording and resetting it cost"
// (https://github.com/dvoyni/cog/issues/380) on the map
// https://github.com/dvoyni/cog/issues/377. It exists to be measured; nothing
// here is the shape the spec names.
//
// The shape measured is a Hook per Component, ecs.Hooks[T, K], K one of a closed
// set of kind sets:
//
//   - One log per Store, shared by every reader of it, with a cursor per
//     reader. Every record is appended under a lock the act already holds:
//     write{T} for UpdateFor, Remove.From and a writer's run end, write{*Entities}
//     for a Spawn or a Despawn. No act reads another Store, so no lock set grows
//     and the log needs no sequence: the lock orders it.
//   - A removal or despawn copies T's row into the log's retained arena. An
//     addition copies nothing.
//   - A writer handed rows with write access on a Store watched for Changed
//     snapshots them and compares at its run end, appending a change record.
//   - A reader's run start folds its window into its own copy and fills values
//     from the next removal or the live Store. Its run end advances its cursor,
//     compacts what every reader has passed, and clears its copy.

import (
	"iter"
	"reflect"
	"sync"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

type hookKind uint8

const (
	kindSpawned hookKind = 1 << iota
	kindDespawned
	kindAdded
	kindRemoved
	kindChanged
)

// kindSet is closed: only the types below satisfy it.
type kindSet interface{ kinds() hookKind }

type (
	HookSpawned          struct{}
	HookDespawned        struct{}
	HookSpawnedDespawned struct{}
	HookAdded            struct{}
	HookRemoved          struct{}
	HookAddedRemoved     struct{}
	HookAddedChanged     struct{}
	HookAll              struct{}
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

type runBeginner interface {
	needsRunBegin() bool
	beginRun()
}

type runEnder interface {
	needsRunEnd() bool
	endRun()
}

// hookHub hangs off Entities once any Hooks[T, K] is planned.
type hookHub struct {
	// despawning is one capture per observed Store, walked before a despawn
	// empties the Stores.
	despawning []func(e Entity)
	nextWriter uint32
	preparing  uint32
}

func (en *Entities) hookHub() *hookHub {
	if en.hooks == nil {
		en.hooks = &hookHub{}
	}
	return en.hooks
}

type hookRecord struct {
	e Entity
	// value is the absolute index of the retained copy a removal took, or -1.
	value  int64
	writer uint32
	kinds  hookKind
}

// hookRecords is the half of a log that knows no T, which a writer's snapshot
// appends change records to. hookLog[T] embeds it first, so the Store header's
// erased pointer is a pointer to it.
type hookRecords struct {
	watch   hookKind
	mu      sync.Mutex
	records []hookRecord
	base    uint64
	cursors []*uint64
}

type hookLog[T any] struct {
	hookRecords
	trivial      bool
	retained     []T
	retainedBase int64
}

func logFor[T any](en *Entities) *hookLog[T] {
	hub := en.hookHub()
	class := en.classOf(reflect.TypeFor[T]())
	if class == nil {
		panic("ecs: Hooks names unregistered Component " + kernel.TypeName(reflect.TypeFor[T]()))
	}
	store := (*Store[T])(unsafe.Pointer(class.header))
	if store.hooks == nil {
		log := &hookLog[T]{trivial: class.trivial}
		store.hooks = log
		hub.despawning = append(hub.despawning, func(e Entity) {
			if row, ok := store.probe(e); ok {
				log.removed(e, &store.dense[row], kindRemoved|kindDespawned)
			}
		})
	}
	return store.hooks
}

// added records an addition. A Store watched only for Spawned or Despawned skips
// the additions an UpdateFor makes.
func (l *hookLog[T]) added(e Entity, kinds hookKind) {
	if l.watch&(kinds&(kindSpawned|kindAdded|kindChanged)) == 0 {
		return
	}
	l.records = append(l.records, hookRecord{e: e, value: -1, kinds: kinds})
}

// removed records a removal with T's last value. A Store watched only for
// Despawned skips a plain removal.
func (l *hookLog[T]) removed(e Entity, row *T, kinds hookKind) {
	if kinds&kindDespawned == 0 && l.watch&^kindDespawned == 0 {
		return
	}
	l.retained = append(l.retained, *row)
	l.records = append(l.records, hookRecord{e: e, value: l.retainedBase + int64(len(l.retained)-1), kinds: kinds})
}

func (r *hookRecords) changed(e Entity, writer uint32) {
	r.records = append(r.records, hookRecord{e: e, value: -1, writer: writer, kinds: kindChanged})
}

// compact drops what every reader has passed, zeroing the retained copies it
// drops so a string or List they hold is released. Held under mu.
func (l *hookLog[T]) compact() {
	least := l.base + uint64(len(l.records))
	for _, c := range l.cursors {
		least = min(least, *c)
	}
	n := int(least - l.base)
	if n == 0 {
		return
	}
	firstValue := l.retainedBase + int64(len(l.retained))
	for i := n; i < len(l.records); i++ {
		if l.records[i].value >= 0 {
			firstValue = l.records[i].value
			break
		}
	}
	dropped := int(firstValue - l.retainedBase)
	kept := copy(l.retained, l.retained[dropped:])
	if !l.trivial {
		clear(l.retained[kept:])
	}
	l.retained = l.retained[:kept]
	l.retainedBase = firstValue
	kept = copy(l.records, l.records[n:])
	l.records = l.records[:kept]
	l.base = least
}

// Hook is one record, as its reader sees it.
type Hook[T any] struct {
	kinds  hookKind
	filled bool
	Value  T
}

func (h *Hook[T]) IsSpawned() bool   { return h.kinds&kindSpawned != 0 }
func (h *Hook[T]) IsDespawned() bool { return h.kinds&kindDespawned != 0 }
func (h *Hook[T]) IsAdded() bool     { return h.kinds&kindAdded != 0 }
func (h *Hook[T]) IsRemoved() bool   { return h.kinds&kindRemoved != 0 }
func (h *Hook[T]) IsChanged() bool   { return h.kinds&kindChanged != 0 }

type hookEntry[T any] struct {
	e    Entity
	hook Hook[T]
}

const (
	markNone uint8 = iota
	markOpen
	markOutside
)

type hookMark struct {
	gen   uint32
	run   uint32
	out   int32
	state uint8
}

// Hooks is the System parameter.
type Hooks[T any, K kindSet] struct {
	log     *hookLog[T]
	store   *Store[T]
	cursor  uint64
	out     []hookEntry[T]
	pending []int32
	marks   []hookMark
	run     uint32
	writer  uint32
	deliver hookKind
}

func (h *Hooks[T, K]) prepare(en *Entities, access kernel.ResourceAccess) {
	h.bind(en)
	h.writer = en.hooks.preparing
	_ = en.classOf(reflect.TypeFor[T]()).declareRead(access)
}

// bind plans the reader with no kernel, for benchmarks.
func (h *Hooks[T, K]) bind(en *Entities) {
	var k K
	h.log = logFor[T](en)
	h.store = (*Store[T])(unsafe.Pointer(en.classOf(reflect.TypeFor[T]()).header))
	h.deliver = k.kinds()
	h.log.watch |= h.deliver
	h.cursor = h.log.base + uint64(len(h.log.records))
	h.log.cursors = append(h.log.cursors, &h.cursor)
}

func (h *Hooks[T, K]) needsRunBegin() bool { return true }
func (h *Hooks[T, K]) needsRunEnd() bool   { return true }

// All yields the records fixed at the run's start, in the order they happened.
func (h *Hooks[T, K]) All() iter.Seq2[Entity, *Hook[T]] {
	return func(yield func(Entity, *Hook[T]) bool) {
		for i := range h.out {
			o := &h.out[i]
			if !yield(o.e, &o.hook) {
				return
			}
		}
	}
}

func (h *Hooks[T, K]) mark(e Entity) *hookMark {
	index := e.idx()
	for int(index) >= len(h.marks) {
		h.marks = append(h.marks, hookMark{})
	}
	m := &h.marks[index]
	if m.run != h.run || m.gen != e.gen() {
		*m = hookMark{gen: e.gen(), run: h.run, out: -1}
	}
	return m
}

func (h *Hooks[T, K]) beginRun() {
	l := h.log
	h.run++
	h.out = h.out[:0]
	h.pending = h.pending[:0]
	l.mu.Lock()
	records := l.records[h.cursor-l.base:]
	for i := range records {
		r := &records[i]
		switch {
		case r.kinds == kindChanged:
			if h.deliver&kindChanged == 0 || r.writer == h.writer {
				continue
			}
			m := h.mark(r.e)
			if m.state != markNone {
				continue // folded into the record that opened the window
			}
			m.state, m.out = markOpen, int32(len(h.out))
			h.out = append(h.out, hookEntry[T]{e: r.e, hook: Hook[T]{kinds: kindChanged}})
			h.pending = append(h.pending, m.out)
		case r.kinds&kindAdded != 0:
			m := h.mark(r.e)
			m.state, m.out = markOpen, -1
			if r.kinds&h.deliver != 0 {
				m.out = int32(len(h.out))
				h.out = append(h.out, hookEntry[T]{e: r.e, hook: Hook[T]{kinds: r.kinds}})
				h.pending = append(h.pending, m.out)
			}
		default:
			retained := &l.retained[r.value-l.retainedBase]
			m := h.mark(r.e)
			if m.state == markOpen && m.out >= 0 {
				o := &h.out[m.out]
				o.hook.Value, o.hook.filled = *retained, true
			}
			m.state, m.out = markOutside, -1
			if r.kinds&h.deliver != 0 {
				h.out = append(h.out, hookEntry[T]{e: r.e, hook: Hook[T]{kinds: r.kinds, filled: true, Value: *retained}})
			}
		}
	}
	h.cursor = l.base + uint64(len(l.records))
	l.mu.Unlock()
	store := h.store
	for _, i := range h.pending {
		o := &h.out[i]
		if o.hook.filled {
			continue
		}
		if row, ok := store.probe(o.e); ok {
			o.hook.Value, o.hook.filled = store.dense[row], true
		}
	}
}

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

// rowSnapshot is a writer's copy of the rows it was handed with write access on
// a Store watched for Changed, compared when its run ends.
type rowSnapshot struct {
	log    *hookRecords
	store  *storeHeader
	size   uintptr
	writer uint32
	ents   []Entity
	rows   []byte
	stamps []uint32
	run    uint32
	taken  bool
	bulk   bool
}

func snapshotFor[T any](en *Entities) *rowSnapshot {
	return snapshotOfClass(en, en.classOf(reflect.TypeFor[T]()))
}

func snapshotOfClass(en *Entities, class *componentClass) *rowSnapshot {
	if class == nil || class.header.hooks == nil || class.header.hooks.watch&kindChanged == 0 || class.size == 0 {
		return nil
	}
	return &rowSnapshot{log: class.header.hooks, store: class.header, size: class.size, writer: en.hooks.preparing, run: 1}
}

func (s *rowSnapshot) take(e Entity, row uint32) {
	if s.bulk {
		return
	}
	s.taken = true
	index := e.idx()
	for int(index) >= len(s.stamps) {
		s.stamps = append(s.stamps, 0)
	}
	if s.stamps[index] == s.run {
		return
	}
	s.stamps[index] = s.run
	s.ents = append(s.ents, e)
	s.rows = append(s.rows, unsafe.Slice((*byte)(unsafe.Add(s.store.dense.data, uintptr(row)*s.size)), s.size)...)
}

// takeAll snapshots the whole Store, which is exact for a System holding its
// write lock: a row it was not handed cannot change.
func (s *rowSnapshot) takeAll() {
	if s.bulk {
		return
	}
	st := s.store
	n := len(st.owners)
	s.ents = append(s.ents[:0], st.owners...)
	s.rows = append(s.rows[:0], unsafe.Slice((*byte)(st.dense.data), uintptr(n)*s.size)...)
	s.taken, s.bulk = true, true
}

func (s *rowSnapshot) finish() {
	if !s.taken {
		return
	}
	log, st, size := s.log, s.store, s.size
	if s.bulk && len(st.owners) == len(s.ents) && entitiesBytes(st.owners) == entitiesBytes(s.ents) {
		live := unsafe.Slice((*byte)(st.dense.data), uintptr(len(s.ents))*size)
		const block = 64
		for start := 0; start < len(s.ents); start += block {
			end := min(start+block, len(s.ents))
			if string(live[uintptr(start)*size:uintptr(end)*size]) == string(s.rows[uintptr(start)*size:uintptr(end)*size]) {
				continue
			}
			for r := start; r < end; r++ {
				lo, hi := uintptr(r)*size, uintptr(r+1)*size
				if string(live[lo:hi]) != string(s.rows[lo:hi]) {
					log.changed(s.ents[r], s.writer)
				}
			}
		}
	} else {
		for i, e := range s.ents {
			index := e.idx()
			if int(index) >= len(st.sparse) {
				continue
			}
			slot := st.sparse[index]
			if uint32(slot>>32) != e.gen() {
				continue
			}
			live := unsafe.Slice((*byte)(unsafe.Add(st.dense.data, uintptr(uint32(slot))*size)), size)
			lo := uintptr(i) * size
			if string(live) != string(s.rows[lo:lo+size]) {
				log.changed(e, s.writer)
			}
		}
	}
	s.run++
	s.ents, s.rows = s.ents[:0], s.rows[:0]
	s.taken, s.bulk = false, false
}

func entitiesBytes(es []Entity) string {
	return unsafe.String((*byte)(unsafe.Pointer(unsafe.SliceData(es))), len(es)*8)
}

// recordChangeAtCall is the explicit-call fallback of #268: UpdateFor records
// its Changed at the call, deduplicated by a per-Entity stamp.
func (s *rowSnapshot) recordChangeAtCall(e Entity) {
	index := e.idx()
	for int(index) >= len(s.stamps) {
		s.stamps = append(s.stamps, 0)
	}
	if s.stamps[index] == s.run {
		return
	}
	s.stamps[index] = s.run
	s.log.changed(e, s.writer)
}
