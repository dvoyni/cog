package ecs

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

// named is a component holding a ref.
type named struct{ Name Ref[string] }

// refsWorld is a world with an observed store of named and refs of strings.
type refsWorld struct {
	world   World[game]
	store   Store[game, named]
	changes Changes[game, named]
	refs    Refs[game, string]
	lookup  RefsLookup[game, string]
}

func spawnRefs(t testing.TB) *refsWorld {
	o := &refsWorld{}
	spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, named](c)
		RegisterRefs[game, string](c)
		o.world = declare[World[game]](c)
		o.store = declare[Store[game, named]](c)
		o.changes = declare[Changes[game, named]](c)
		o.refs = declare[Refs[game, string]](c)
		o.lookup = declare[RefsLookup[game, string]](c)
	})
	return o
}

// TestRefsMatchAReferenceModel creates, sets, releases and reads refs at
// random, and checks every live handle against a map of what it holds and
// every handle gone stale against nil.
func TestRefsMatchAReferenceModel(t *testing.T) {
	o := spawnRefs(t)
	random := rand.New(rand.NewPCG(3, 7))
	model := map[Ref[string]]string{}
	var live, stale []Ref[string]
	for step := range 5000 {
		value := fmt.Sprint(step)
		switch {
		case len(live) == 0 || random.IntN(3) == 0:
			h := o.refs.New(value)
			if _, ok := model[h]; ok || h == (Ref[string]{}) {
				t.Fatalf("New returned a live or zero handle %+v", h)
			}
			model[h] = value
			live = append(live, h)
		case random.IntN(2) == 0:
			i := random.IntN(len(live))
			old := live[i]
			if !o.refs.Set(&live[i], value) || live[i] == old {
				t.Fatalf("Set of live handle %+v failed or kept the handle", old)
			}
			delete(model, old)
			model[live[i]] = value
			stale = append(stale, old)
		default:
			i := random.IntN(len(live))
			h := live[i]
			if !o.refs.Release(h) {
				t.Fatalf("Release of live handle %+v failed", h)
			}
			delete(model, h)
			live[i] = live[len(live)-1]
			live = live[:len(live)-1]
			stale = append(stale, h)
		}
		for h, want := range model {
			if got := o.lookup.Get(h); got == nil || *got != want {
				t.Fatalf("step %d: handle %+v reads %v, want %q", step, h, got, want)
			}
		}
	}
	past := uint32(o.refs.refs.slots.Len()) + 1 // the slot past the end, zeroed if its chunk exists
	stale = append(stale, Ref[string]{}, Ref[string]{index: past}, Ref[string]{index: 1 << 20, generation: 1})
	for _, h := range stale {
		field := h
		if o.refs.Get(h) != nil || o.refs.Set(&field, "x") || o.refs.Release(h) {
			t.Fatalf("stale handle %+v still reads, sets or releases", h)
		}
	}
}

func TestRefSetIsAComponentChange(t *testing.T) {
	o := spawnRefs(t)
	ObservationReset(nil, kernel.NoArg{}, o.world)
	e := o.world.Spawn()
	o.store.Add(e, named{Name: o.refs.New("a")})
	ObservationReset(nil, kernel.NoArg{}, o.world)
	first := o.store.Get(e).Name
	*o.refs.Get(first) = "b"
	for range o.changes.All() {
		t.Fatal("a write through Get was seen")
	}
	o.refs.Set(&o.store.Write(e).Name, "c")
	got := map[Entity]Change[named]{}
	for e, change := range o.changes.All() {
		got[e] = change
	}
	change := got[e]
	if len(got) != 1 || change.Kind != ChangeChanged || change.Previous.Name != first || change.Value.Name == first {
		t.Fatalf("Set reported %+v, want a change from the old handle to a new one", got)
	}
	if o.refs.Get(change.Previous.Name) != nil || *o.refs.Get(change.Value.Name) != "c" {
		t.Fatal("the old handle still reads, or the new one does not read the value set")
	}
}

func TestRefsAllocateNothingOnceGrown(t *testing.T) {
	o := spawnRefs(t)
	handles := make([]Ref[string], 500)
	cycle := func() {
		for i := range handles {
			handles[i] = o.refs.New("a")
		}
		for i := range handles {
			o.refs.Set(&handles[i], "b")
			if *o.lookup.Get(handles[i]) != "b" {
				t.Fatal("a ref did not read what was set")
			}
			o.refs.Release(handles[i])
		}
	}
	cycle()
	if got := testing.AllocsPerRun(10, cycle); got != 0 {
		t.Fatalf("New, Set, Get and Release allocated %v times", got)
	}
}

func TestRegisterRefsCompositionErrors(t *testing.T) {
	type unregistered [1]uint64
	expectPanic(t, "not registered", func() {
		spawn(t, func(c *kernel.Configurator) { RegisterRefs[unregistered, string](c) })
	})
	expectPanic(t, "twice", func() {
		spawn(t, func(c *kernel.Configurator) {
			RegisterRefs[game, string](c)
			RegisterRefs[game, string](c)
		})
	})
	expectPanic(t, "not initialized", func() {
		spawn(t, func(c *kernel.Configurator) { declare[Refs[game, string]](c) })
	})
}
