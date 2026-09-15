package types

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"

	"github.com/dvoyni/cog/kernel"
)

// The Validation checks for Hooks that need no table: hooks.md § Validation
// mode checks the rule and § What IsX reports. Everything here is reached only
// from behind `if validate`, so a release build compiles none of it into a path
// that runs. The List checks, which do need a table, are in validate_on.go.

// hookPaceLimit is how many counted runs may pass between two runs of a Hooks
// reader: hooks.md publishes it as 16, so a writer that runs twice in a tick to
// catch up a fixed step does not trip it.
const hookPaceLimit = 16

// systemPace is a System's count of its own runs on the Hook logs it can append
// to: each run that grew a log bumps that log's counter once, however many
// records it appended.
//
// A System appends only to the logs of Stores it holds a write lock on, so it
// reads each one only while nothing else appends to it or compacts it.
type systemPace struct {
	// stores are the Stores the System writes, named at registration; every
	// says it holds write{*Entities} and so may append to any log, through a
	// Spawn or a Despawn. Neither is resolved to logs until the first run,
	// because a Store's log exists only once a reader registers, which may be
	// after this System.
	stores   []*storeHeader
	every    bool
	resolved bool
	logs     []pacedLog
}

// pacedLog is one log and where it ended when this run started.
type pacedLog struct {
	log *hookRecords
	end uint64
}

// enrol names a Store the System writes.
func (p *systemPace) enrol(store *storeHeader) {
	for _, known := range p.stores {
		if known == store {
			return
		}
	}
	p.stores = append(p.stores, store)
}

// begin marks where each log ends as the run starts. A log's end is counted
// from the first record it ever held, so compaction does not move it and only
// an append does.
func (p *systemPace) begin(en *Entities) {
	if !p.resolved {
		p.resolve(en)
	}
	for i := range p.logs {
		paced := &p.logs[i]
		paced.end = paced.log.base + uint64(len(paced.log.records))
	}
}

// end counts the run once on every log it appended to. It runs after the
// System's Changed compares, which append too.
func (p *systemPace) end() {
	for i := range p.logs {
		paced := &p.logs[i]
		if paced.log.base+uint64(len(paced.log.records)) != paced.end {
			paced.log.runs++
		}
	}
}

func (p *systemPace) resolve(en *Entities) {
	p.resolved = true
	if p.every {
		for _, class := range en.classes {
			if class.header.hooks != nil {
				p.logs = append(p.logs, pacedLog{log: class.header.hooks})
			}
		}
		return
	}
	for _, store := range p.stores {
		if store.hooks != nil {
			p.logs = append(p.logs, pacedLog{log: store.hooks})
		}
	}
}

// keepPace is a reader's check at its run start: more than hookPaceLimit
// counted runs since its last run panics, naming its System and the Store.
func (h *Hooks[T, K]) keepPace() {
	runs := h.log.runs
	if behind := runs - h.seen; behind > hookPaceLimit {
		panic(fmt.Sprintf(
			"ecs: System %s reads %s, and its run starts %d counted runs of Systems appending to Store[%s]'s log after its last run, past the %d allowed: a System reading Hooks runs as often as the Systems writing its Component, and one that falls behind holds that log for every reader of it. Move the reader to its writers' event, or pause the writers when the reader pauses",
			h.system, h.named, behind, kernel.TypeName(reflect.TypeFor[T]()), hookPaceLimit))
	}
	h.seen = runs
}

// systemName is how a diagnostic names a System: its func's name without the
// import path, or its signature for a func the runtime cannot name.
func systemName(fn reflect.Value) string {
	if f := runtime.FuncForPC(fn.Pointer()); f != nil {
		name := f.Name()
		return name[strings.LastIndex(name, "/")+1:]
	}
	return kernel.TypeName(fn.Type())
}

// ask is IsX's check: a kind the record's kind set can never deliver panics,
// because asking is the bug of an index that never removes. A Hook no reader
// delivered carries no kind set and is not checked.
func (h *Hook[T]) ask(kind hookKind, method string) {
	under := deliveredUnder(h.under)
	if under == 0 {
		return
	}
	const additions, removals = kindSpawned | kindAdded | kindChanged, kindDespawned | kindRemoved
	possible := under&additions != 0
	if kind&removals != 0 {
		possible = under&removals != 0
	}
	if possible {
		return
	}
	what := "a removal or a despawn"
	if kind&removals == 0 {
		what = "an addition, a spawn or a change"
	}
	panic(fmt.Sprintf(
		"ecs: %s on a Hook[%s] delivered under %s, which never delivers %s, so it can only ever report false: asking is the bug of an index that never removes. Read Hooks[%s, K] under a kind set that delivers what the code asks about",
		method, kernel.TypeName(reflect.TypeFor[T]()), kindSetName(under), what, kernel.TypeName(reflect.TypeFor[T]())))
}

// kindSetName names the kind set whose kinds these are.
func kindSetName(kinds hookKind) string {
	for _, set := range []KindSet{
		HookSpawned{}, HookDespawned{}, HookSpawnedDespawned{}, HookAdded{},
		HookRemoved{}, HookAddedRemoved{}, HookAddedChanged{}, HookAll{},
	} {
		if set.kinds() == kinds {
			return kernel.TypeName(reflect.TypeOf(set))
		}
	}
	return fmt.Sprintf("kinds %05b", kinds)
}
