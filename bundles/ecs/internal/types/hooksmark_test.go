package types

import (
	"fmt"
	"slices"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// The behaviour tests of Set.MarkChanged: hooks.md § Changed is a difference in
// bytes, for the write a byte compare cannot see.

// TestAMarkRecordsOneChangedPerEntityPerWriterRun is Set.MarkChanged on each
// write route: a marked row records one Changed at the writer's run end whether
// or not its bytes differ, and a mark beside a write, or a second mark, still
// records one.
func TestAMarkRecordsOneChangedPerEntityPerWriterRun(t *testing.T) {
	var got []heard
	w := newHookWorld(t, func(h *Hooks[collider, HookAddedChanged]) {
		got = got[:0]
		listen(h, &got, radius)
	})
	ids := make([]Entity, 7)
	for i := range ids {
		ids[i] = w.entities.alloc()
		w.components.colliders.Set(ids[i], collider{Radius: 1})
	}
	marked, twice, markedRef, refMarked, markedUpdate, restored, untouched := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6]
	expectOnce := func(what string, want []heard) {
		t.Helper()
		counts := changesIn(w.components.colliders.hooks)
		for _, h := range want {
			if counts[h.e] != 1 {
				t.Fatalf("%s: the log holds %d Changed records for %v, want 1: %v", what, counts[h.e], h.e, counts)
			}
		}
		if len(counts) != len(want) {
			t.Fatalf("%s: the log holds Changed records for %v, want only %v", what, counts, want)
		}
		w.read(t)
		expectHeard(t, what, byEntity(got), want)
	}

	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		set.MarkChanged(marked)
		set.MarkChanged(twice)
		set.MarkChanged(twice)
		set.MarkChanged(markedRef)
		ref, _ := set.Ref(markedRef)
		ref.Radius = 2
		ref, _ = set.Ref(refMarked)
		ref.Radius = 3
		set.MarkChanged(refMarked)
		set.MarkChanged(markedUpdate)
		set.UpdateFor(markedUpdate, collider{Radius: 4})
		set.MarkChanged(markedUpdate)
		set.UpdateFor(restored, collider{Radius: 9})
		set.MarkChanged(restored)
		set.UpdateFor(restored, collider{Radius: 1})
		set.Ref(untouched)
	})
	expectOnce("Ref, UpdateFor and MarkChanged", []heard{
		{marked, "changed", 1},
		{twice, "changed", 1},
		{markedRef, "changed", 2},
		{refMarked, "changed", 3},
		{markedUpdate, "changed", 4},
		{restored, "changed", 1},
	})

	w.walked(t, func(q *Query[colliderQuery], set *Set[collider], remove *Remove[collider]) {
		// A mark made before the Query binds its whole-Store copy survives it.
		set.MarkChanged(marked)
		for e, it := range q.All() {
			switch e {
			case twice:
				it.Collider.Radius = 5
				set.MarkChanged(e)
			case markedRef:
				set.MarkChanged(e)
			}
		}
		set.MarkChanged(twice)
	})
	expectOnce("a *T field beside MarkChanged", []heard{
		{marked, "changed", 1},
		{twice, "changed", 5},
		{markedRef, "changed", 2},
	})
}

// nestedRow is one element of nestedLists: a List inside a List's element.
type nestedRow struct{ Inner List[int32] }

// nestedLists is a Component whose List's elements hold Lists, so a Set on an
// inner List goes through At's copy of its element.
type nestedLists struct{ Rows List[nestedRow] }

// nestedListsPlugin owns nestedLists.
type nestedListsPlugin struct{ store *Store[nestedLists] }

func (p *nestedListsPlugin) Name() kernel.PluginName { return "nestedlists" }

func (p *nestedListsPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (p *nestedListsPlugin) Register(registrar *kernel.Registrar, _ any) error {
	p.store = RegisterComponent[nestedLists](registrar, 16)
	return nil
}

type (
	nestedWriteCmd kernel.Command[hookRequest, hookResponse]
	nestedReadCmd  kernel.Command[hookRequest, hookResponse]
)

// TestAMarkRecordsANestedListSet is the nested-List Gap of hooks.md § Changed is
// a difference in bytes, closed by its call: a Set on a List nested in another
// List's element leaves the stored row's bytes as they were and records
// nothing, and the same Set followed by MarkChanged records one Changed, whose
// value carries the nested write.
func TestAMarkRecordsANestedListSet(t *testing.T) {
	var got []string
	var write func(set *Set[nestedLists])
	owner := &nestedListsPlugin{}
	entities, _, engine := newWorldWith(t, 16, func(registrar *kernel.Registrar) {
		registrar.HandleCommand[nestedWriteCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(set *Set[nestedLists]) { write(set) }))
		registrar.HandleCommand[nestedReadCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(h *Hooks[nestedLists, HookAddedChanged]) {
				got = got[:0]
				for e, hook := range h.All() {
					var inner []int32
					for _, item := range hook.Value.Rows.At(0).Inner.All() {
						inner = append(inner, item)
					}
					got = append(got, fmt.Sprintf("%v changed=%v %v", e, hook.IsChanged(), inner))
				}
			}))
	}, []kernel.PluginName{Name, "components", "nestedlists"}, owner)
	written, untouched := entities.alloc(), entities.alloc()
	for _, e := range []Entity{written, untouched} {
		owner.store.Set(e, nestedLists{Rows: NewList(nestedRow{Inner: NewList[int32](1, 2)})})
	}
	run := func(what string, mark bool, at int, value int32) {
		t.Helper()
		write = func(set *Set[nestedLists]) {
			ref, _ := set.Ref(written)
			row := ref.Rows.At(0)
			row.Inner.Set(at, value)
			if mark {
				set.MarkChanged(written)
			}
		}
		engine.Executioner().ExecuteCommand[nestedWriteCmd](hookRequest{})
	}
	read := func() {
		t.Helper()
		engine.Executioner().ExecuteCommand[nestedReadCmd](hookRequest{})
	}

	run("unmarked", false, 0, 7)
	if counts := changesIn(owner.store.hooks); len(counts) != 0 {
		t.Fatalf("a nested List.Set with no mark recorded Changed %v, want none: the byte compare cannot see it", counts)
	}
	read()
	if len(got) != 0 {
		t.Fatalf("the reader was given %v for an unmarked nested List.Set, want nothing", got)
	}

	run("marked", true, 1, 8)
	if counts := changesIn(owner.store.hooks); counts[written] != 1 || len(counts) != 1 {
		t.Fatalf("a nested List.Set and MarkChanged recorded Changed %v, want one for %v", counts, written)
	}
	read()
	if want := []string{fmt.Sprintf("%v changed=true [7 8]", written)}; !slices.Equal(got, want) {
		t.Fatalf("the reader was given %v, want %v", got, want)
	}
}

// TestAMarkRecordsNothingWhereNoChangedIsDue is Set.MarkChanged where it does
// nothing: on an Entity without T, even one that gains T later in the run, on a
// dead Entity, on one despawned or losing T later in the run, which records only
// the removal, and on a Store no reader watches for Changed. A marked run that
// also grows the Store past the indices it marked still compares the rows it
// copied.
func TestAMarkRecordsNothingWhereNoChangedIsDue(t *testing.T) {
	var got []heard
	w := newHookWorld(t, func(h *Hooks[collider, HookAll]) {
		got = got[:0]
		listen(h, &got, radius)
	})
	kept, withoutT, deadBefore, despawned, removed := w.entities.alloc(), w.entities.alloc(), w.entities.alloc(), w.entities.alloc(), w.entities.alloc()
	for _, e := range []Entity{kept, deadBefore, despawned, removed} {
		w.components.colliders.Set(e, collider{Radius: 1})
	}
	w.entities.despawn(deadBefore)
	w.read(t)

	w.write(t, func(set *Set[collider], remove *Remove[collider]) {
		set.MarkChanged(withoutT)
		set.UpdateFor(withoutT, collider{Radius: 3})
		set.MarkChanged(deadBefore)
		set.MarkChanged(removed)
		remove.From(removed)
	})
	if kinds := kindsIn(w.components.colliders.hooks); !slices.Equal(kinds, []string{"added+changed", "removed"}) {
		t.Fatalf("marks on an Entity without T, a dead one, and one then removed left %v, want only the addition and the removal", kinds)
	}
	w.read(t)
	expectHeard(t, "a mark then a removal", got, []heard{{withoutT, "added+changed", 3}, {removed, "removed", 1}})

	var grown Entity
	w.structural(t, func(r restacking) {
		r.set.MarkChanged(kept)
		r.set.MarkChanged(despawned)
		r.we.Despawn(despawned)
		for range 2 * len(w.components.colliders.sparse) {
			grown = r.armed.New(armedSet{Collider: collider{Radius: 1}})
		}
		ref, _ := r.set.Ref(grown)
		ref.Radius = 2
	})
	counts := changesIn(w.components.colliders.hooks)
	if counts[kept] != 1 || counts[despawned] != 0 || counts[grown] != 1 || len(counts) != 2 {
		t.Fatalf("the log holds Changed records %v, want one each for %v (marked) and %v (written), and none for %v (despawned)",
			counts, kept, grown, despawned)
	}

	unwatched := newHookWorld(t, func(h *Hooks[collider, HookAddedRemoved]) {})
	held := unwatched.entities.alloc()
	unwatched.components.colliders.Set(held, collider{Radius: 1})
	unwatched.read(t)
	unwatched.write(t, func(set *Set[collider], remove *Remove[collider]) {
		set.MarkChanged(held)
	})
	if kinds := kindsIn(unwatched.components.colliders.hooks); len(kinds) != 0 {
		t.Fatalf("a mark on a Store no reader watches for Changed recorded %v, want nothing", kinds)
	}
}

// TestATagNeverRecordsAMarkedChange is hooks.md § Which acts are recorded for
// MarkChanged: a Component with no fields never records Changed, marked or not.
func TestATagNeverRecordsAMarkedChange(t *testing.T) {
	entities, components, engine := newWorld(t, 16, func(registrar *kernel.Registrar) {
		registrar.HandleCommand[tagWriteCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(q *Query[disabledQuery], set *Set[disabled]) {
				for e := range q.All() {
					set.MarkChanged(e)
				}
			}))
		registrar.HandleCommand[tagReadCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(h *Hooks[disabled, HookAddedChanged]) {}))
	})
	held := entities.alloc()
	components.disableds.Set(held, disabled{})
	for range 2 {
		engine.Executioner().ExecuteCommand[tagWriteCmd](hookRequest{})
	}
	if kinds := kindsIn(components.disableds.hooks); len(kinds) != 0 {
		t.Fatalf("marking a Tag recorded %v, want nothing", kinds)
	}
}

// TestASystemNeverSeesItsOwnMark is hooks.md § A System never sees its own
// Changed, for MarkChanged: a System that marks collider and reads its Hooks is
// not given its own mark's Changed, and is given another writer's.
func TestASystemNeverSeesItsOwnMark(t *testing.T) {
	var got []heard
	var own func(set *Set[collider])
	w := newHookWorld(t, func(h *Hooks[collider, HookAll], set *Set[collider]) {
		got = got[:0]
		listen(h, &got, radius)
		if own != nil {
			own(set)
		}
	})
	held, other := w.entities.alloc(), w.entities.alloc()
	w.components.colliders.Set(held, collider{Radius: 1})
	w.components.colliders.Set(other, collider{Radius: 2})

	own = func(set *Set[collider]) { set.MarkChanged(held) }
	w.read(t)
	if counts := changesIn(w.components.colliders.hooks); counts[held] != 1 || len(counts) != 1 {
		t.Fatalf("the System's own mark of %v left Changed records %v, want one", held, counts)
	}
	w.write(t, func(set *Set[collider], remove *Remove[collider]) { set.MarkChanged(other) })
	own = nil
	w.read(t)
	expectHeard(t, "the run after its own mark and another writer's", got, []heard{{other, "changed", 2}})
}

// TestMarkingAChangeAllocatesNothingAfterTheFirstWatchedRun is hooks.md § How
// recording is switched on, for MarkChanged: its marks are allocated by the
// writer's first marking run on a watched Store and kept, so every later run
// allocates nothing, and a mark on a Store nothing watches allocates nothing at
// all.
func TestMarkingAChangeAllocatesNothingAfterTheFirstWatchedRun(t *testing.T) {
	var colliders *Set[collider]
	var bodies *Set[body]
	var reader *Hooks[collider, HookAddedChanged]
	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar) {
		registrar.Subscribe[hookCaptureSystem](ToHandler[app.UpdateEvent](registrar, func(c *Set[collider], b *Set[body]) {
			colliders, bodies = c, b
		}))
		registrar.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](registrar, func(h *Hooks[collider, HookAddedChanged]) {
			reader = h
		}))
	})
	ids := make([]Entity, 64)
	for i := range ids {
		ids[i] = entities.alloc()
		components.colliders.Set(ids[i], collider{})
		components.bodies.Set(ids[i], body{})
	}
	frame(t, engine, 1)

	delivered := 0
	run := func() {
		for _, e := range ids {
			colliders.MarkChanged(e)
		}
		colliders.changes.compare()
		reader.beginRun()
		delivered += len(reader.out)
		reader.endRun()
	}
	if first := allocationsDuring(run); first == 0 {
		t.Fatal("the first marking run on a watched Store allocated nothing, want its marks allocated")
	}
	if objects := testing.AllocsPerRun(100, run); objects != 0 {
		t.Fatalf("MarkChanged on a watched Store allocated %v objects a run after the first, want 0", objects)
	}
	if runs := 1 + 101; delivered != runs*len(ids) {
		t.Fatalf("the reader was given %d Changed over %d runs of %d marked Entities, want one per Entity per run",
			delivered, runs, len(ids))
	}

	unwatched := func() {
		for _, e := range ids {
			bodies.MarkChanged(e)
		}
		bodies.changes.compare()
	}
	if objects := testing.AllocsPerRun(100, unwatched); objects != 0 {
		t.Fatalf("MarkChanged on an unwatched Store allocated %v objects a run, want 0", objects)
	}
	if components.bodies.hooks != nil {
		t.Fatal("MarkChanged on an unwatched Store gave it a log")
	}
}
