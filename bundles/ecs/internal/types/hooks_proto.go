package types

// PROTOTYPE, throwaway. Branch proto/ecs-hooks, for "Where a Hook log lives, and
// what recording and resetting it cost"
// (https://github.com/dvoyni/cog/issues/380) on the map
// https://github.com/dvoyni/cog/issues/377. It exists to be measured; nothing
// here is the shape the spec names, and it takes shortcuts a real build may not
// (a log reaches Stores through their erased header rather than a kernel handle).
//
// The shape measured:
//
//   - One log per Q type, shared by every reader of it, with a cursor per
//     reader. Membership records (spawned, despawned, entered, exited) are
//     appended at the act, under locks the act holds: a Store's own add/remove
//     for Set and Remove (widened to read Q's other Stores), write{*Entities}
//     for a Spawn or a Despawn. An exit or despawn copies Q's values into the
//     log's retained arena; nothing else copies at the act.
//   - Changes live per Store, not per Q, because two value writers of two of
//     Q's Stores run concurrently and are never widened. A writer snapshots the
//     rows it is handed with write access and compares at its run end, appending
//     (Entity, sequence, writer) for each row whose bytes differ.
//   - A reader's run start merges its log and change streams by sequence into
//     its own copy, folds to one values-carrying record per window, and fills
//     entry and change values from the next exit or from the live Stores. Its
//     run end advances its cursors, compacts what every reader has passed, and
//     clears its copy so retained strings and Lists are released.

import (
	"fmt"
	"iter"
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// The blank marker fields a Hooks Q declares its kinds with.
type (
	Spawned   struct{}
	Despawned struct{}
	Entered   struct{}
	Exited    struct{}
	Changed   struct{}
)

type hookKind uint8

const (
	kindSpawned hookKind = 1 << iota
	kindDespawned
	kindEntered
	kindExited
	kindChanged
)

// membership is what makes a log need Store sinks: a Changed reader needs
// entries and exits whether it declares them or not.
const membership = kindEntered | kindExited | kindChanged

var markerKinds = map[reflect.Type]hookKind{
	reflect.TypeFor[Spawned]():   kindSpawned,
	reflect.TypeFor[Despawned](): kindDespawned,
	reflect.TypeFor[Entered]():   kindEntered,
	reflect.TypeFor[Exited]():    kindExited,
	reflect.TypeFor[Changed]():   kindChanged,
}

type runBeginner interface {
	needsRunBegin() bool
	beginRun()
}

type runEnder interface {
	needsRunEnd() bool
	endRun()
}

// hookHub hangs off Entities once any Hooks[Q] is planned.
type hookHub struct {
	// seq orders membership records against change records across logs and
	// Stores. A membership record takes Add; a change takes Load, which is
	// stable against Q's own log because every appender to that log is
	// excluded from a writer of Q's Stores.
	seq atomic.Uint64
	sinks      []hookSink
	logs       map[reflect.Type]hookSink
	nextWriter uint32
	preparing  uint32
	// noWiden is a benchmark switch: plan widened locks or not.
	noWiden bool
}

func (en *Entities) hookHub() *hookHub {
	if en.hooks == nil {
		en.hooks = &hookHub{logs: map[reflect.Type]hookSink{}}
	}
	return en.hooks
}

type hookSink interface {
	gained(e Entity, field int)
	losing(e Entity, field int)
	spawned(e Entity)
	despawning(e Entity)
	classes() []*componentClass
	kinds() hookKind
}

func (w *hookHub) spawned(e Entity) {
	for _, s := range w.sinks {
		s.spawned(e)
	}
}

func (w *hookHub) despawning(e Entity) {
	for _, s := range w.sinks {
		s.despawning(e)
	}
}

type storeSink struct {
	sink  hookSink
	field int
}

// storeHooks is what a Store carries once some Hooks[Q] names it.
type storeHooks struct {
	hub   *hookHub
	sinks []storeSink
	// watched is set when some reader declares Changed over this Store as a
	// value field; it is what makes a writer snapshot.
	watched bool
	size    uintptr
	mu      sync.Mutex
	changes []changeRecord
	base    uint64
	cursors []*uint64
}

type changeRecord struct {
	e      Entity
	seq    uint64
	writer uint32
}

func (h *storeHooks) gained(e Entity) {
	for i := range h.sinks {
		h.sinks[i].sink.gained(e, h.sinks[i].field)
	}
}

func (h *storeHooks) losing(e Entity) {
	for i := range h.sinks {
		h.sinks[i].sink.losing(e, h.sinks[i].field)
	}
}

// compact drops the changes every reader has passed. Held under mu.
func (h *storeHooks) compact() {
	least := h.base + uint64(len(h.changes))
	for _, c := range h.cursors {
		least = min(least, *c)
	}
	n := int(least - h.base)
	if n == 0 {
		return
	}
	kept := copy(h.changes, h.changes[n:])
	h.changes = h.changes[:kept]
	h.base = least
}

type hookField struct {
	class  *componentClass
	store  *storeHeader
	hooks  *storeHooks
	wanted uint32
	offset uintptr
	size   uintptr
	filter bool
	copy   func(dst, src unsafe.Pointer)
}

type hookRecord struct {
	e     Entity
	kinds hookKind
	seq   uint64
	// value is the absolute index of the retained copy an exit or despawn
	// took, or -1.
	value int64
}

// hookLog is one Q's membership log, shared by all its readers.
type hookLog[Q any] struct {
	hub          *hookHub
	fields       []hookField
	watch        hookKind
	trivial      bool
	mu           sync.Mutex
	records      []hookRecord
	base         uint64
	retained     []Q
	retainedBase int64
	cursors      []*uint64
}

func hookLogFor[Q any](en *Entities) *hookLog[Q] {
	w := en.hookHub()
	queryType := reflect.TypeFor[Q]()
	if existing, ok := w.logs[queryType]; ok {
		return existing.(*hookLog[Q])
	}
	l := &hookLog[Q]{hub: w, trivial: PointerFree(queryType) == nil}
	for i := range queryType.NumField() {
		field := queryType.Field(i)
		if kind, ok := markerKinds[field.Type]; ok {
			l.watch |= kind
			continue
		}
		componentType, wanted, isFilter := field.Type, uint32(0), false
		if component, present, is := filterOf(field.Type); is {
			componentType, isFilter = component, true
			if !present {
				wanted = absentGeneration
			}
		} else if field.Type.Kind() == reflect.Pointer {
			panic(fmt.Sprintf("ecs: Hooks %s field %s is a %s; a Hook's values are a copy, so write through Set[T].Ref",
				kernel.TypeName(queryType), field.Name, kernel.TypeName(field.Type)))
		}
		class := en.classOf(componentType)
		if class == nil {
			panic(fmt.Sprintf("ecs: Hooks %s names unregistered Component %s", kernel.TypeName(queryType), kernel.TypeName(componentType)))
		}
		planned := hookField{class: class, store: class.header, wanted: wanted, offset: field.Offset, size: class.size, filter: isFilter}
		if isFilter {
			planned.size = 0
		} else if !class.trivial {
			planned.copy = class.copyValue
		}
		l.fields = append(l.fields, planned)
	}
	if l.watch == 0 {
		panic(fmt.Sprintf("ecs: Hooks %s declares no kind", kernel.TypeName(queryType)))
	}
	for i := range l.fields {
		f := &l.fields[i]
		if f.store.hooks == nil {
			f.store.hooks = &storeHooks{hub: w, size: f.class.size}
		}
		f.hooks = f.store.hooks
		if l.watch&membership != 0 {
			f.hooks.sinks = append(f.hooks.sinks, storeSink{sink: l, field: i})
		}
		if l.watch&kindChanged != 0 && !f.filter && f.size > 0 {
			f.hooks.watched = true
		}
	}
	w.sinks = append(w.sinks, l)
	w.logs[queryType] = l
	return l
}

func (l *hookLog[Q]) kinds() hookKind { return l.watch }

func (l *hookLog[Q]) classes() []*componentClass {
	out := make([]*componentClass, len(l.fields))
	for i := range l.fields {
		out[i] = l.fields[i].class
	}
	return out
}

// matches is Q's match for e, skipping one field: the Store the act is on.
func (l *hookLog[Q]) matches(e Entity, except int) bool {
	index := e.idx()
	for i := range l.fields {
		if i == except {
			continue
		}
		f := &l.fields[i]
		sparse := f.store.sparse
		if int(index) >= len(sparse) {
			if f.wanted != absentGeneration {
				return false
			}
			continue
		}
		if uint32(sparse[index]>>32) != e.gen()|f.wanted {
			return false
		}
	}
	return true
}

// fill copies Q's value fields for a matching e into dst.
func (l *hookLog[Q]) fill(e Entity, dst unsafe.Pointer) {
	index := e.idx()
	for i := range l.fields {
		f := &l.fields[i]
		if f.size == 0 {
			continue
		}
		src := unsafe.Add(f.store.dense.data, uintptr(uint32(f.store.sparse[index]))*f.size)
		if f.copy != nil {
			f.copy(unsafe.Add(dst, f.offset), src)
			continue
		}
		(queryCursor{rows: src, size: f.size, offset: f.offset}).fill(0, dst)
	}
}

func (l *hookLog[Q]) capture(e Entity) int64 {
	var zero Q
	l.retained = append(l.retained, zero)
	l.fill(e, unsafe.Pointer(&l.retained[len(l.retained)-1]))
	return l.retainedBase + int64(len(l.retained)-1)
}

func (l *hookLog[Q]) record(e Entity, kinds hookKind, value int64) {
	l.records = append(l.records, hookRecord{e: e, kinds: kinds, seq: l.hub.seq.Add(1), value: value})
}

func (l *hookLog[Q]) gained(e Entity, field int) {
	if !l.matches(e, field) {
		return
	}
	if l.fields[field].wanted == absentGeneration {
		l.record(e, kindExited, l.capture(e))
		return
	}
	l.record(e, kindEntered|kindChanged, -1)
}

func (l *hookLog[Q]) losing(e Entity, field int) {
	if !l.matches(e, field) {
		return
	}
	if l.fields[field].wanted == absentGeneration {
		l.record(e, kindEntered|kindChanged, -1)
		return
	}
	l.record(e, kindExited, l.capture(e))
}

func (l *hookLog[Q]) spawned(e Entity) {
	if l.watch&(kindSpawned|membership) == 0 || !l.matches(e, -1) {
		return
	}
	l.record(e, kindSpawned|kindEntered|kindChanged, -1)
}

func (l *hookLog[Q]) despawning(e Entity) {
	if l.watch&(kindDespawned|membership) == 0 || !l.matches(e, -1) {
		return
	}
	l.record(e, kindDespawned|kindExited, l.capture(e))
}

// compact drops what every reader has passed, and zeroes the retained copies
// it drops so a string or List they hold is released. Held under mu.
func (l *hookLog[Q]) compact() {
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
type Hook[Q any] struct {
	kinds  hookKind
	filled bool
	Values Q
}

func (h *Hook[Q]) IsSpawned() bool   { return h.kinds&kindSpawned != 0 }
func (h *Hook[Q]) IsDespawned() bool { return h.kinds&kindDespawned != 0 }
func (h *Hook[Q]) IsEntered() bool   { return h.kinds&kindEntered != 0 }
func (h *Hook[Q]) IsExited() bool    { return h.kinds&kindExited != 0 }
func (h *Hook[Q]) IsChanged() bool   { return h.kinds&kindChanged != 0 }

type hookEntry[Q any] struct {
	e    Entity
	hook Hook[Q]
}

const (
	markNone uint8 = iota
	markTentative
	markOpen
	markOutside
)

type hookMark struct {
	gen   uint32
	run   uint32
	out   int32
	state uint8
}

type hookStream struct {
	hooks  *storeHooks
	cursor uint64
	view   []changeRecord
	pos    int
}

// Hooks is the System parameter.
type Hooks[Q any] struct {
	log      *hookLog[Q]
	cursor   uint64
	streams  []hookStream
	out      []hookEntry[Q]
	pending  []int32
	marks    []hookMark
	run      uint32
	writer   uint32
	declared hookKind
}

func (h *Hooks[Q]) prepare(en *Entities, access kernel.ResourceAccess) {
	h.bind(en)
	h.writer = en.hooks.preparing
	for i := range h.log.fields {
		_ = h.log.fields[i].class.declareRead(access)
	}
}

// bind plans the reader against the hub, with no kernel, for benchmarks.
func (h *Hooks[Q]) bind(en *Entities) {
	h.log = hookLogFor[Q](en)
	h.declared = h.log.watch
	h.cursor = h.log.base + uint64(len(h.log.records))
	h.log.cursors = append(h.log.cursors, &h.cursor)
	if h.log.watch&kindChanged != 0 {
		for i := range h.log.fields {
			f := &h.log.fields[i]
			if f.filter || f.size == 0 {
				continue
			}
			h.streams = append(h.streams, hookStream{hooks: f.hooks})
		}
		for i := range h.streams {
			s := &h.streams[i]
			s.cursor = s.hooks.base + uint64(len(s.hooks.changes))
			s.hooks.cursors = append(s.hooks.cursors, &s.cursor)
		}
	}
}

func (h *Hooks[Q]) needsRunBegin() bool { return true }
func (h *Hooks[Q]) needsRunEnd() bool   { return true }

// All yields the records fixed at the run's start, in the order they happened.
func (h *Hooks[Q]) All() iter.Seq2[Entity, *Hook[Q]] {
	return func(yield func(Entity, *Hook[Q]) bool) {
		for i := range h.out {
			o := &h.out[i]
			if o.hook.kinds == 0 {
				continue
			}
			if !yield(o.e, &o.hook) {
				return
			}
		}
	}
}

func (h *Hooks[Q]) mark(e Entity) *hookMark {
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

func (h *Hooks[Q]) beginRun() {
	l := h.log
	h.run++
	h.out = h.out[:0]
	h.pending = h.pending[:0]
	l.mu.Lock()
	for i := range h.streams {
		s := &h.streams[i]
		s.hooks.mu.Lock()
		s.view = s.hooks.changes[s.cursor-s.hooks.base:]
		s.pos = 0
	}
	records := l.records[h.cursor-l.base:]
	for i := range records {
		r := &records[i]
		if h.streams != nil {
			h.drain(r.seq)
		}
		h.apply(r)
	}
	if h.streams != nil {
		h.drain(^uint64(0))
	}
	h.cursor = l.base + uint64(len(l.records))
	for i := range h.streams {
		s := &h.streams[i]
		s.cursor = s.hooks.base + uint64(len(s.hooks.changes))
		s.view = nil
		s.hooks.mu.Unlock()
	}
	l.mu.Unlock()
	for _, i := range h.pending {
		o := &h.out[i]
		if o.hook.kinds == 0 || o.hook.filled {
			continue
		}
		if h.mark(o.e).state == markTentative && !l.matches(o.e, -1) {
			o.hook.kinds = 0
			continue
		}
		l.fill(o.e, unsafe.Pointer(&o.hook.Values))
		o.hook.filled = true
	}
}

func (h *Hooks[Q]) drain(limit uint64) {
	for i := range h.streams {
		s := &h.streams[i]
		for s.pos < len(s.view) && s.view[s.pos].seq < limit {
			c := s.view[s.pos]
			s.pos++
			if c.writer == h.writer {
				continue
			}
			m := h.mark(c.e)
			if m.state != markNone {
				continue
			}
			m.state = markTentative
			m.out = int32(len(h.out))
			h.out = append(h.out, hookEntry[Q]{e: c.e, hook: Hook[Q]{kinds: kindChanged}})
			h.pending = append(h.pending, m.out)
		}
	}
}

func (h *Hooks[Q]) apply(r *hookRecord) {
	m := h.mark(r.e)
	kinds := r.kinds & h.declared
	if r.kinds&kindEntered != 0 {
		if m.state == markTentative {
			h.out[m.out].hook.kinds = 0
		}
		m.state, m.out = markOpen, -1
		if kinds != 0 {
			m.out = int32(len(h.out))
			h.out = append(h.out, hookEntry[Q]{e: r.e, hook: Hook[Q]{kinds: kinds}})
			h.pending = append(h.pending, m.out)
		}
		return
	}
	retained := &h.log.retained[r.value-h.log.retainedBase]
	if (m.state == markOpen || m.state == markTentative) && m.out >= 0 {
		if o := &h.out[m.out]; o.hook.kinds != 0 {
			o.hook.Values = *retained
			o.hook.filled = true
		}
	}
	m.state, m.out = markOutside, -1
	if kinds != 0 {
		h.out = append(h.out, hookEntry[Q]{e: r.e, hook: Hook[Q]{kinds: kinds, filled: true, Values: *retained}})
	}
}

func (h *Hooks[Q]) endRun() {
	l := h.log
	l.mu.Lock()
	l.compact()
	l.mu.Unlock()
	for i := range h.streams {
		s := h.streams[i].hooks
		s.mu.Lock()
		s.compact()
		s.mu.Unlock()
	}
	if !l.trivial {
		clear(h.out)
	}
	h.out = h.out[:0]
}

// rowSnapshot is a writer's copy of the rows it was handed with write access on
// a watched Store, compared when its run ends.
type rowSnapshot struct {
	hooks  *storeHooks
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
	if class == nil || class.header.hooks == nil || !class.header.hooks.watched || class.size == 0 {
		return nil
	}
	return &rowSnapshot{hooks: class.header.hooks, store: class.header, size: class.size, writer: en.hooks.preparing, run: 1}
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
	h, st, size := s.hooks, s.store, s.size
	seq := h.hub.seq.Load()
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
					h.changes = append(h.changes, changeRecord{e: s.ents[r], seq: seq, writer: s.writer})
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
				h.changes = append(h.changes, changeRecord{e: e, seq: seq, writer: s.writer})
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

// widenForHooks is section 5 of #379: a handle that adds to or removes from one
// of a multi-Store Q's Stores also reads Q's other Stores, while a reader of Q
// watches a kind that depends on the match.
func widenForHooks[T any](en *Entities, access kernel.ResourceAccess) {
	w := en.hooks
	if w == nil || w.noWiden {
		return
	}
	class := en.classOf(reflect.TypeFor[T]())
	for _, sink := range w.sinks {
		if sink.kinds()&membership == 0 {
			continue
		}
		classes := sink.classes()
		if len(classes) < 2 {
			continue
		}
		names := false
		for _, c := range classes {
			names = names || c == class
		}
		if !names {
			continue
		}
		for _, c := range classes {
			if c != class {
				_ = c.declareRead(access)
			}
		}
	}
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
	s.hooks.changes = append(s.hooks.changes, changeRecord{e: e, seq: s.hooks.hub.seq.Load(), writer: s.writer})
}
