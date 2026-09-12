//go:build ecs_validate

package ecs

import (
	"fmt"
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
// the window it happens in belongs to some unrelated System.
//
// What it does not catch, stated rather than left to be discovered. A write
// through unsafe, or through a []T a caller extracted before the value ever
// reached a Store. A write by a callee the value was passed to, which is
// reported against whoever called Set and not against whoever handed it over. A
// List whose backing array was evicted from the table below. And, first among
// them, anything a run never executes: this is detection, and its coverage is
// the test suite rather than the type system.
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
func stampStored(row unsafe.Pointer, lists []uintptr, owner string) {
	for _, offset := range lists {
		stamp(listData(row, offset), listStamp{mode: modeStored, owner: owner})
	}
}

// stampRun records what a run handed out. A write field stamps modeWrite and a
// read field modeRead, and both are stamped rather than only the read: a write
// that left the previous read's stamp in place would report the next legal
// write as an illegal one.
func stampRun(row unsafe.Pointer, lists []uintptr, owner string, run *runToken, mode listMode) {
	for _, offset := range lists {
		entry := listStamp{run: run, mode: mode, owner: owner}
		if run != nil {
			entry.gen = run.gen.Load()
		}
		stamp(listData(row, offset), entry)
	}
}

// listData reads the backing-array pointer of the List at offset within a row.
// A List's slice header sits at the List's own offset, because the marker in
// front of it is zero-size.
func listData(row unsafe.Pointer, offset uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Add(row, offset))
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
	if !known {
		return
	}
	switch entry.mode {
	case modeStored:
		panic(fmt.Sprintf(
			"ecs: List.Set on a %s already in the world: a value that has entered a Store may only be written through a write-locked handle — a *%s Query field or a Set[%s] accessor — because every reader shares its backing array",
			entry.owner, entry.owner, entry.owner))
	case modeRead:
		panic(fmt.Sprintf(
			"ecs: List.Set through a read of %s: a read yields a copy and a copy of a List shares its backing array, so this writes the Store while every concurrent reader holds read{%s}. Name the Component as *%s to write it",
			entry.owner, entry.owner, entry.owner))
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
