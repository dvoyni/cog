//go:build ecs_validate

package types

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"
)

// This file is the whole of validation mode, and it exists because a List
// cannot be made safe the way a string is.
//
// A string is immutable, so a copy of one is not a write handle and the lock
// unit stays sound by construction. A List is mutable, so the soundness of
// read{C} rests on nobody calling Set through a value a read produced — which
// is a rule, not a property. This file checks the rule at the one place it can
// be broken, and it is compiled in only under -tags ecs_validate.
//
// What it catches. A Set through a value a read field, a Get accessor or a
// Store write handed out; and a Set through a value a finished Query run handed
// out, which is the case no before-and-after comparison can attribute, because
// the window it happens in belongs to some unrelated System. Since Hooks, a Set
// through a Hook's value, whenever it happens; and, on a Store a Changed reader
// watches, a Set on an array a second Component holds while its first owner
// still does.
//
// What it does not catch, stated rather than left to be discovered. A write
// through unsafe, or through a []T a caller extracted before the value ever
// reached a Store. A write by a callee the value was passed to, which is
// reported against whoever called Set and not against whoever handed it over. A
// List whose backing array was evicted from the table below. A Set through a
// Set.Of copy taken before a Ref or a write field reached the same array, which
// the later stamp makes look like a write through that handle. And, first among
// them, anything a run never executes: this is detection, and its coverage is
// the test suite rather than the type system.
//
// Since Hooks, three more. A kept Hook value whose array a writer later reaches
// is misattributed the same way: through a *T Query field the stamp becomes
// modeWrite tied to the writer's run, so a Set through the kept copy panics as
// "after the run that yielded it finished", naming the writer's run rather than
// the Hook. It still panics, because a reader holding read{T} never runs inside
// a writer's run. Through Set.Ref the stamp names no run, so a Set through the
// kept copy is allowed until a reader next stamps that array. A second owner on
// a Store no Changed reader watches, because only the Changed compare
// registers holders. And a second owner the compare has not reached yet: an
// array that arrives through an UpdateFor addition or a Spawn is registered to
// its new row only at the end of a later run that writes that row.
//
// What it costs, and why that is acceptable. One map write per List field per
// stamped row per run, under one mutex, so a validating build serialises where
// a release build runs concurrently and is slower by more than the map alone.
// That is the deliberate shape: the cost is confined to a build nobody ships,
// and the alternative — a check cheap enough to leave on — was a wider List,
// which would have cost the release build width it has no use for.

const validate = true

// runToken is one Query's current run. A Query owns exactly one and reuses it
// for the life of the engine, so a validating run allocates no more than a
// release one; gen is what distinguishes this run of it from the last.
//
// The run is one All() call rather than one System call, which is stricter than
// the lock window and deliberately so: the spec already says the pointer a
// Query yields is valid only for the current step, so a value outliving the
// loop it came out of is invalid whether or not the lock is still held.
type runToken struct {
	active atomic.Bool
	gen    atomic.Uint64
	name   string
}

func newRunToken(name string) *runToken { return &runToken{name: name} }

func (r *runToken) begin() {
	if r == nil {
		return
	}
	r.gen.Add(1)
	r.active.Store(true)
}

func (r *runToken) end() {
	if r == nil {
		return
	}
	r.active.Store(false)
}

// listStamp is what the table holds for one backing array: the mode it was last
// reached under, the run that reached it, and the Component it belongs to,
// which is the only part of the message a reader can act on.
type listStamp struct {
	run   *runToken
	gen   uint64
	mode  listMode
	owner string
	// reader and system are a modeHook stamp's: the Hooks parameter that handed
	// the value out, and its System.
	reader, system string
}

// stampCap bounds the table. Keys are real pointers, so an entry keeps its
// backing array alive; without a bound a spawn-heavy run would retain every
// array it ever stamped. Eviction is oldest-first and its only consequence is a
// missed diagnosis: a write through a List evicted long ago is treated as a
// write through a List the world has never seen, which is allowed.
const stampCap = 1 << 16

var stamps = struct {
	sync.Mutex
	by    map[unsafe.Pointer]listStamp
	order []unsafe.Pointer
}{by: make(map[unsafe.Pointer]listStamp, stampCap)}

func stamp(data unsafe.Pointer, entry listStamp) {
	if data == nil {
		return
	}
	stamps.Lock()
	defer stamps.Unlock()
	if _, seen := stamps.by[data]; !seen {
		if len(stamps.order) >= stampCap {
			oldest := stamps.order[0]
			stamps.order = stamps.order[1:]
			delete(stamps.by, oldest)
		}
		stamps.order = append(stamps.order, data)
	}
	stamps.by[data] = entry
}

// stampStored records that a value has entered a Store, so every alias of its
// arrays — including the one the caller who built it still holds — becomes
// writable only through a write-locked handle.
func stampStored(row unsafe.Pointer, lists []listSite, owner string) {
	stampSites(row, lists, listStamp{mode: modeStored, owner: owner})
}

// stampRun records what a run handed out. A write field stamps modeWrite and a
// read field modeRead, and both are stamped rather than only the read: a write
// that left the previous read's stamp in place would report the next legal
// write as an illegal one.
func stampRun(row unsafe.Pointer, lists []listSite, owner string, run *runToken, mode listMode) {
	entry := listStamp{run: run, mode: mode, owner: owner}
	if run != nil {
		entry.gen = run.gen.Load()
	}
	stampSites(row, lists, entry)
}

// stampHook records that a Hooks reader handed out a record's value holding
// these Lists. The stamp names no run, because a Hook's value is never writable,
// in the run that yielded it or after.
func stampHook(value unsafe.Pointer, lists []listSite, owner, reader, system string) {
	stampSites(value, lists, listStamp{mode: modeHook, owner: owner, reader: reader, system: system})
}

// stampSites stamps every List a row names, and every List those Lists'
// elements name, with one entry: a nested backing array is reached through the
// same handle as the List holding it, so it takes the same mode and owner.
//
// The walk is what makes a List of Lists checkable, and it is also what makes
// one expensive to validate: a row holding a List of n elements, each with a
// List of its own, stamps n+1 arrays rather than one, and they fill the table
// below n+1 times as fast - so the oldest-first eviction forgets sooner, and a
// write through a List stamped long ago is likelier to go undiagnosed.
func stampSites(row unsafe.Pointer, lists []listSite, entry listStamp) {
	for i := range lists {
		site := &lists[i]
		header := (*sliceHeader)(unsafe.Add(row, site.offset))
		stamp(header.data, entry)
		if len(site.nested) == 0 {
			continue
		}
		for element := range header.len {
			stampSites(unsafe.Add(header.data, uintptr(element)*site.stride), site.nested, entry)
		}
	}
}

// sliceHeader is the layout of a List's slice field, read at a site's offset.
// A List's slice header sits at the List's own offset, because the marker in
// front of it is zero-size.
type sliceHeader struct {
	data     unsafe.Pointer
	len, cap int
}

// checkListWritable is the check List.Set makes. A backing array the table does
// not know is a List the world has never seen, and writing it is the caller's
// own business.
func checkListWritable(data unsafe.Pointer) {
	if data == nil {
		return
	}
	stamps.Lock()
	entry, known := stamps.by[data]
	stamps.Unlock()
	if known {
		checkStamp(entry)
	}
	checkHolders(data)
}

// checkStamp panics when the handle a backing array was last reached through
// may not write it.
func checkStamp(entry listStamp) {
	switch entry.mode {
	case modeStored:
		panic(fmt.Sprintf(
			"ecs: List.Set on a %s already in the world: a value that has entered a Store may only be written through a write-locked handle — a *%s Query field or a Set[%s] accessor — because every reader shares its backing array",
			entry.owner, entry.owner, entry.owner))
	case modeRead:
		panic(fmt.Sprintf(
			"ecs: List.Set through a read of %s: a read yields a copy and a copy of a List shares its backing array, so this writes the Store while every concurrent reader holds read{%s}. Name the Component as *%s to write it",
			entry.owner, entry.owner, entry.owner))
	case modeSetOf:
		panic(fmt.Sprintf(
			"ecs: List.Set through a Set[%s].Of copy: the copy shares the stored List's backing array but not the row, so the write never reaches the stored Component's bytes and no Changed Hook can see it. Write the stored List through Set[%s].Ref",
			entry.owner, entry.owner))
	case modeHook:
		panic(fmt.Sprintf(
			"ecs: List.Set through the Value of a Hook that System %s read through %s: a Hook's Value is a read of %s, and its List shares its backing array with the Store for an addition or a change, and with every reader's copy for a removal, so this writes memory other Systems read. Write the Entity's List through Set[%s].Ref(e) while the Entity holds %s; a removal's Entity no longer holds one, so there is nothing to write",
			entry.system, entry.reader, entry.owner, entry.owner, entry.owner))
	case modeWrite:
		if entry.run == nil {
			return
		}
		if !entry.run.active.Load() || entry.run.gen.Load() != entry.gen {
			panic(fmt.Sprintf(
				"ecs: List.Set on %s after the run that yielded it finished (%s): the value a Query yields is valid only for the current step, and its List is no more durable than the pointer it came in",
				entry.owner, entry.run.name))
		}
	}
}

// hookUnder is the kind set a delivered Hook was read under, which IsX checks a
// kind against: hooks.md § What IsX reports.
type hookUnder = hookKind

func underOf(kinds hookKind) hookUnder        { return kinds }
func deliveredUnder(under hookUnder) hookKind { return under }

// listHolder is one Component of one Entity holding a List's backing array, as
// the Changed compare at a writer's run end last found it.
type listHolder struct {
	store *storeHeader
	e     Entity
}

// heldList is one backing array an Entity's Component holds.
type heldList struct {
	store *storeHeader
	data  unsafe.Pointer
}

// holdings is the second-owner table: ecs.md § Validation mode. holders is
// each registered array's holders, first owner first, and held each Entity's
// arrays, so an act that removes a row can let go of them without reading the
// Store it removed from.
//
// Only a Changed compare registers, so only a Store a Changed reader watches is
// covered. An entry lasts while its Component holds the array: a compare that
// finds the row holding another array lets the old one go, and Remove.From and
// a Despawn let go of everything the row held. A Hook log is never a holder.
// Keys are real pointers, so an entry keeps its array alive while it lasts,
// which is while a live row holds it.
var holdings = struct {
	sync.Mutex
	holders map[unsafe.Pointer][]listHolder
	held    map[Entity][]heldList
	current []unsafe.Pointer
}{holders: map[unsafe.Pointer][]listHolder{}, held: map[Entity][]heldList{}}

// holdLists registers the List arrays of each compared row to its Component and
// Entity, and lets go of the arrays those rows held and no longer do. The
// caller holds the Store's write lock.
func holdLists(store *storeHeader, size uintptr, owners []Entity) {
	holdings.Lock()
	defer holdings.Unlock()
	for _, e := range owners {
		row, ok := store.probe(e)
		if !ok {
			continue
		}
		current := appendListArrays(holdings.current[:0], unsafe.Add(store.dense.data, uintptr(row)*size), store.lists)
		holdings.current = current
		held := holdings.held[e]
		kept := held[:0]
		for _, h := range held {
			if h.store == store && !slices.Contains(current, h.data) {
				letGo(h.data, listHolder{store, e})
				continue
			}
			kept = append(kept, h)
		}
		for _, data := range current {
			if !slices.Contains(kept, heldList{store, data}) {
				kept = append(kept, heldList{store, data})
				holdings.holders[data] = append(holdings.holders[data], listHolder{store, e})
			}
		}
		if len(kept) == 0 {
			delete(holdings.held, e)
		} else {
			holdings.held[e] = kept
		}
	}
}

// releaseLists is Remove.From letting go of the arrays e's row of store held.
func releaseLists(store *storeHeader, e Entity) {
	holdings.Lock()
	defer holdings.Unlock()
	held, ok := holdings.held[e]
	if !ok {
		return
	}
	kept := held[:0]
	for _, h := range held {
		if h.store == store {
			letGo(h.data, listHolder{store, e})
			continue
		}
		kept = append(kept, h)
	}
	if len(kept) == 0 {
		delete(holdings.held, e)
	} else {
		holdings.held[e] = kept
	}
}

// releaseEntityLists is a Despawn letting go of every array e's rows held.
func releaseEntityLists(e Entity) {
	holdings.Lock()
	defer holdings.Unlock()
	for _, h := range holdings.held[e] {
		letGo(h.data, listHolder{h.store, e})
	}
	delete(holdings.held, e)
}

// letGo drops one holder of data. Held under holdings.
func letGo(data unsafe.Pointer, holder listHolder) {
	holders := slices.DeleteFunc(holdings.holders[data], func(h listHolder) bool { return h == holder })
	if len(holders) == 0 {
		delete(holdings.holders, data)
		return
	}
	holdings.holders[data] = holders
}

// appendListArrays appends the backing array of every List a row names, and of
// every List within those Lists' elements.
func appendListArrays(into []unsafe.Pointer, row unsafe.Pointer, lists []listSite) []unsafe.Pointer {
	for i := range lists {
		site := &lists[i]
		header := (*sliceHeader)(unsafe.Add(row, site.offset))
		if header.data == nil {
			continue
		}
		into = append(into, header.data)
		for element := range header.len {
			into = appendListArrays(into, unsafe.Add(header.data, uintptr(element)*site.stride), site.nested)
		}
	}
	return into
}

// checkHolders panics on a Set of an array a second Component holds while its
// first owner still does.
func checkHolders(data unsafe.Pointer) {
	holdings.Lock()
	holders := holdings.holders[data]
	if len(holders) < 2 {
		holdings.Unlock()
		return
	}
	first, second := holders[0], holders[1]
	holdings.Unlock()
	panic(fmt.Sprintf(
		"ecs: List.Set on a List two Components hold, %s of %v and %s of %v: a write under one's write lock changes memory the other's readers read under theirs, and no lock names it. A List that is Set belongs to exactly one Component; give the second its own copy, built with ecs.ListOf from the elements",
		first.store.owner, first.e, second.store.owner, second.e))
}
