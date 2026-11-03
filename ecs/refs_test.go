package ecs

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

// named is a component holding a ref.
type named struct{ Name Ref[string] }

// mesh is a value of keyed refs found by its path.
type mesh struct {
	Path     string
	Vertices []float32
}

func (m mesh) Key() string { return m.Path }

// refsWorld is a world with an observed store of named, refs of strings
// with their deltas, and keyed refs of meshes.
type refsWorld struct {
	world      World[game]
	store      Store[game, named]
	deltas     DeltaQuery1[game, named]
	refs       Refs[game, string]
	lookup     RefsLookup[game, string]
	refDeltas  DeltaRefs[game, string]
	meshes     KeyedRefs[game, string, mesh]
	meshLookup KeyedRefsLookup[game, string, mesh]
	meshDeltas DeltaRefs[game, mesh]
}

func spawnRefs(t testing.TB) *refsWorld {
	o := &refsWorld{}
	spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, named](c)
		RegisterRefs[game, string](c, RefsOptions{LastIndex: 1<<20 - 1})
		RegisterKeyedRefs[game, string, mesh](c, RefsOptions{LastIndex: 1<<20 - 1})
		o.world = declare[World[game]](c)
		o.store = declare[Store[game, named]](c)
		o.deltas = declare[DeltaQuery1[game, named]](c)
		o.refs = declare[Refs[game, string]](c)
		o.lookup = declare[RefsLookup[game, string]](c)
		o.refDeltas = declare[DeltaRefs[game, string]](c)
		o.meshes = declare[KeyedRefs[game, string, mesh]](c)
		o.meshLookup = declare[KeyedRefsLookup[game, string, mesh]](c)
		o.meshDeltas = declare[DeltaRefs[game, mesh]](c)
	})
	return o
}

// refDelta is a ref's delta with its value copied out.
type refDelta[T any] struct {
	Delta Delta
	Value T
}

// collectRefDeltas answers what d yields, by handle, failing t on a handle
// yielded twice.
func collectRefDeltas[T any](t *testing.T, d DeltaRefs[game, T]) map[Ref[T]]refDelta[T] {
	got := map[Ref[T]]refDelta[T]{}
	for h, delta := range d.All() {
		if _, ok := got[h]; ok {
			t.Fatalf("ref %+v yielded twice", h)
		}
		got[h] = refDelta[T]{Delta: delta.Delta, Value: *delta.Value}
	}
	return got
}

// TestRefsMatchAReferenceModel creates, sets, releases and reads refs at
// random, before observation starts, and checks every live handle against a
// map of what it holds and every released one against nothing.
func TestRefsMatchAReferenceModel(t *testing.T) {
	o := spawnRefs(t)
	random := rand.New(rand.NewPCG(3, 7))
	model := map[Ref[string]]string{}
	var live, released []Ref[string]
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
			h := live[random.IntN(len(live))]
			if !o.refs.Set(h, value) {
				t.Fatalf("Set of live handle %+v failed", h)
			}
			model[h] = value
		default:
			i := random.IntN(len(live))
			h := live[i]
			if !o.refs.Release(h) {
				t.Fatalf("Release of live handle %+v failed", h)
			}
			delete(model, h)
			live[i] = live[len(live)-1]
			live = live[:len(live)-1]
			released = append(released, h)
		}
		for h, want := range model {
			if got := o.lookup.Get(h); got == nil || *got != want || !o.lookup.Alive(h) {
				t.Fatalf("step %d: handle %+v reads %v, want %q", step, h, got, want)
			}
		}
	}
	released = append(released, Ref[string]{}, Ref[string]{index: 1 << 30, generation: 1})
	for _, h := range released {
		if o.refs.Get(h) != nil || o.refs.Alive(h) || o.refs.Set(h, "x") || o.refs.Release(h) {
			t.Fatalf("released handle %+v still reads, sets or releases", h)
		}
	}
}

// TestRefSetIsNotAComponentChange sets a ref two components hold: neither
// changes, and DeltaRefs reports the ref.
func TestRefSetIsNotAComponentChange(t *testing.T) {
	o := spawnRefs(t)
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	h := o.refs.New("a")
	first := o.world.Spawn()
	second := o.world.Spawn()
	o.store.Add(first, named{Name: h})
	o.store.Add(second, named{Name: h})
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	o.refs.Set(h, "b")
	if got := collectDeltas(o.deltas); len(got) != 0 {
		t.Fatalf("a ref set changed the components holding it: %+v", got)
	}
	got := collectRefDeltas(t, o.refDeltas)
	if len(got) != 1 || got[h] != (refDelta[string]{Delta: DeltaChanged, Value: "b"}) {
		t.Fatalf("DeltaRefs answered %+v, want h changed to b", got)
	}
	if *o.lookup.Get(o.store.Get(first).Name) != "b" || *o.lookup.Get(o.store.Get(second).Name) != "b" {
		t.Fatal("a holder does not read the value set")
	}
}

// TestDeltaRefsMatchAReferenceModel makes, sets and releases refs at random
// in an observed world, resetting now and then, and checks after every step
// that DeltaRefs yields what a model of the window expects, and that no
// index released in a window is handed out again in it.
func TestDeltaRefsMatchAReferenceModel(t *testing.T) {
	for seed := range uint64(4) {
		o := spawnRefs(t)
		random := rand.New(rand.NewPCG(seed, 11))
		now := map[Ref[string]]string{}
		atReset := map[Ref[string]]bool{}
		changed := map[Ref[string]]bool{}
		released := map[Ref[string]]string{}
		releasedIndices := map[uint32]bool{}
		// freeIndices holds the indices freed by a reset and not handed out
		// since, which a New takes before any fresh one.
		freeIndices := map[uint32]bool{}
		usedIndices := map[uint32]bool{}
		var live []Ref[string]
		for i := range 300 { // before observation starts, so every ref counts as added
			h := o.refs.New(fmt.Sprint(i))
			now[h] = fmt.Sprint(i)
			live = append(live, h)
			usedIndices[h.index] = true
		}
		ObservationResetSystem(nil, kernel.NoArg{}, o.world)
		// listedSteps and overflowedSteps count the steps whose deltas came
		// from the listing and from a walk of every slot.
		listedSteps := 0
		overflowedSteps := 0
		for step := range 4000 {
			value := fmt.Sprint(random.IntN(1000))
			// Phases of frequent resets keep the listing; phases of rare
			// ones overflow it.
			resetFrom := 85
			if step/500%2 == 1 {
				resetFrom = 99
			}
			switch op := random.IntN(100); {
			case op < 15 || len(live) == 0:
				h := o.refs.New(value)
				if releasedIndices[h.index] {
					t.Fatalf("seed %d step %d: index %d reused in the window it was released in", seed, step, h.index)
				}
				if len(freeIndices) > 0 && !freeIndices[h.index] || len(freeIndices) == 0 && usedIndices[h.index] {
					t.Fatalf("seed %d step %d: New answered index %d, not a freed one first and a fresh one then", seed, step, h.index)
				}
				delete(freeIndices, h.index)
				usedIndices[h.index] = true
				now[h] = value
				live = append(live, h)
			case op < 70 || (op < resetFrom && op >= 85):
				h := live[random.IntN(len(live))]
				o.refs.Set(h, value)
				now[h] = value
				changed[h] = true
			case op < resetFrom:
				i := random.IntN(len(live))
				h := live[i]
				o.refs.Release(h)
				if atReset[h] {
					released[h] = now[h]
				}
				delete(now, h)
				releasedIndices[h.index] = true
				live[i] = live[len(live)-1]
				live = live[:len(live)-1]
			default:
				ObservationResetSystem(nil, kernel.NoArg{}, o.world)
				clear(atReset)
				for h := range now {
					atReset[h] = true
				}
				clear(changed)
				clear(released)
				for index := range releasedIndices {
					if o.refs.refs.slots.at(index).value != "" {
						t.Fatalf("seed %d step %d: freed slot %d keeps its value", seed, step, index)
					}
					freeIndices[index] = true
				}
				clear(releasedIndices)
			}
			want := map[Ref[string]]refDelta[string]{}
			for h, value := range now {
				if !atReset[h] {
					want[h] = refDelta[string]{Delta: DeltaAdded, Value: value}
				} else if changed[h] {
					want[h] = refDelta[string]{Delta: DeltaChanged, Value: value}
				}
			}
			for h, value := range released {
				want[h] = refDelta[string]{Delta: DeltaReleased, Value: value}
			}
			if o.refs.refs.changedRefsOverflowed {
				overflowedSteps++
			} else {
				listedSteps++
			}
			got := collectRefDeltas(t, o.refDeltas)
			if len(got) != len(want) {
				t.Fatalf("seed %d step %d: %d deltas, want %d:\n got %v\nwant %v", seed, step, len(got), len(want), got, want)
			}
			for h, delta := range want {
				if got[h] != delta {
					t.Fatalf("seed %d step %d: ref %+v answered %+v, want %+v", seed, step, h, got[h], delta)
				}
			}
			for h, value := range now {
				if got := o.lookup.Get(h); got == nil || *got != value {
					t.Fatalf("seed %d step %d: ref %+v reads %v, want %q", seed, step, h, got, value)
				}
			}
		}
		if listedSteps < 500 || overflowedSteps < 500 {
			t.Fatalf("seed %d: %d steps listed and %d walked every slot", seed, listedSteps, overflowedSteps)
		}
	}
}

func TestKeyedRefsFindOneRefPerKey(t *testing.T) {
	o := spawnRefs(t)
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	rock := o.meshes.Put(mesh{Path: "rock", Vertices: []float32{1}})
	tree := o.meshes.Put(mesh{Path: "tree"})
	if again := o.meshes.Put(mesh{Path: "rock", Vertices: []float32{2}}); again != rock || o.meshLookup.Get(rock).Vertices[0] != 1 {
		t.Fatal("a Put of a key with a ref made another, or replaced its value")
	}
	if found, ok := o.meshLookup.Find("tree"); !ok || found != tree {
		t.Fatal("Find did not answer the ref of its key")
	}
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	if !o.meshes.Update(mesh{Path: "rock", Vertices: []float32{3}}) || o.meshes.Update(mesh{Path: "bush"}) {
		t.Fatal("Update of a key with a ref failed, or of one without succeeded")
	}
	if !o.meshes.ReleaseKey("tree") || o.meshes.ReleaseKey("tree") || o.meshes.Alive(tree) {
		t.Fatal("ReleaseKey did not release once")
	}
	got := collectRefDeltas(t, o.meshDeltas)
	if len(got) != 2 || got[rock].Delta != DeltaChanged || got[rock].Value.Vertices[0] != 3 || got[tree].Delta != DeltaReleased || got[tree].Value.Path != "tree" {
		t.Fatalf("DeltaRefs answered %+v, want rock changed and tree released", got)
	}
	if !o.meshes.Release(rock) || o.meshes.Release(rock) {
		t.Fatal("Release did not release once")
	}
	if _, ok := o.meshes.Find("rock"); ok {
		t.Fatal("a released ref's key still finds it")
	}
	if again := o.meshes.Put(mesh{Path: "rock"}); again == rock || !o.meshes.Alive(again) {
		t.Fatal("a Put of a released key did not make a new ref")
	}
}

// TestRefsPlacedAtLieOutsideTheRange places refs where a mirror would, and
// checks what placing refuses, that placed refs are never handed out again
// and that New stops at the end of the range.
func TestRefsPlacedAtLieOutsideTheRange(t *testing.T) {
	var refs Refs[game, string]
	var meshes KeyedRefs[game, string, mesh]
	var w World[game]
	spawn(t, func(c *kernel.Configurator) {
		RegisterRefs[game, string](c, RefsOptions{FirstIndex: 10, LastIndex: 11})
		RegisterKeyedRefs[game, string, mesh](c, RefsOptions{FirstIndex: 10, LastIndex: 11})
		declare[DeltaRefs[game, string]](c)
		w = declare[World[game]](c)
		refs = declare[Refs[game, string]](c)
		meshes = declare[KeyedRefs[game, string, mesh]](c)
	})
	ObservationResetSystem(nil, kernel.NoArg{}, w)
	placed := Ref[string]{index: 3, generation: 7}
	for _, refused := range []Ref[string]{{index: 3}, {index: 10, generation: 1}, {index: 11, generation: 1}} {
		if refs.NewAt(refused, "x") == nil {
			t.Fatalf("NewAt(%+v) was not refused", refused)
		}
	}
	if err := refs.NewAt(placed, "a"); err != nil || *refs.Get(placed) != "a" {
		t.Fatalf("NewAt failed: %v", err)
	}
	if refs.NewAt(placed, "b") == nil {
		t.Fatal("NewAt over a live ref was not refused")
	}
	refs.Release(placed)
	again := Ref[string]{index: 3, generation: 8}
	if refs.NewAt(again, "c") == nil {
		t.Fatal("NewAt over a ref released this window was not refused")
	}
	ObservationResetSystem(nil, kernel.NoArg{}, w)
	if err := refs.NewAt(again, "c"); err != nil || refs.Alive(placed) {
		t.Fatalf("NewAt after the reset failed (%v), or the old handle came back", err)
	}
	first := refs.New("d")
	second := refs.New("e")
	if first.index < 10 || second.index < 10 || refs.New("f") != (Ref[string]{}) {
		t.Fatalf("New answered %+v and %+v, then went past the range", first, second)
	}
	if err := meshes.PutAt(Ref[mesh]{index: 3, generation: 1}, mesh{Path: "rock"}); err != nil {
		t.Fatalf("PutAt failed: %v", err)
	}
	if meshes.PutAt(Ref[mesh]{index: 4, generation: 1}, mesh{Path: "rock"}) == nil {
		t.Fatal("PutAt of a key with a ref was not refused")
	}
	if h, ok := meshes.Find("rock"); !ok || h != (Ref[mesh]{index: 3, generation: 1}) {
		t.Fatal("a placed ref is not found by its key")
	}
}

func TestRefsAllocateNothingOnceGrown(t *testing.T) {
	o := spawnRefs(t)
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	handles := make([]Ref[string], 500)
	cycle := func() {
		for i := range handles {
			handles[i] = o.refs.New("a")
		}
		ObservationResetSystem(nil, kernel.NoArg{}, o.world)
		for _, h := range handles[:20] {
			o.refs.Set(h, "b")
		}
		for _, delta := range o.refDeltas.All() {
			benchSum += float32(len(*delta.Value))
		}
		for _, h := range handles {
			if *o.lookup.Get(h) != "a" && *o.lookup.Get(h) != "b" {
				t.Fatal("a ref did not read what was set")
			}
			o.refs.Release(h)
		}
		ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	}
	cycle()
	if got := testing.AllocsPerRun(10, cycle); got != 0 {
		t.Fatalf("New, Set, Get, Release, DeltaRefs and the reset allocated %v times", got)
	}
}

func TestKeyedRefsAllocateNothingOnceGrown(t *testing.T) {
	o := spawnRefs(t)
	ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	meshes := make([]mesh, 200)
	for i := range meshes {
		meshes[i] = mesh{Path: fmt.Sprint("mesh", i)}
	}
	cycle := func() {
		for _, m := range meshes {
			o.meshes.Put(m)
		}
		for _, m := range meshes[:50] {
			o.meshes.Update(m)
		}
		for _, m := range meshes {
			if h, ok := o.meshLookup.Find(m.Path); !ok || o.meshLookup.Get(h).Path != m.Path {
				t.Fatal("a mesh is not found by its path")
			}
			o.meshes.ReleaseKey(m.Path)
		}
		ObservationResetSystem(nil, kernel.NoArg{}, o.world)
	}
	cycle()
	if got := testing.AllocsPerRun(10, cycle); got != 0 {
		t.Fatalf("Put, Update, Find and ReleaseKey allocated %v times", got)
	}
}

func TestRegisterRefsCompositionErrors(t *testing.T) {
	type unregistered [1]uint64
	expectPanic(t, "not registered", func() {
		spawn(t, func(c *kernel.Configurator) { RegisterRefs[unregistered, string](c, RefsOptions{}) })
	})
	expectPanic(t, "twice", func() {
		spawn(t, func(c *kernel.Configurator) {
			RegisterRefs[game, string](c, RefsOptions{})
			RegisterRefs[game, string](c, RefsOptions{})
		})
	})
	expectPanic(t, "twice", func() {
		spawn(t, func(c *kernel.Configurator) {
			RegisterRefs[game, mesh](c, RefsOptions{})
			RegisterKeyedRefs[game, string, mesh](c, RefsOptions{})
		})
	})
	expectPanic(t, "empty allocation range", func() {
		spawn(t, func(c *kernel.Configurator) { RegisterRefs[game, string](c, RefsOptions{FirstIndex: 5, LastIndex: 4}) })
	})
	expectPanic(t, "not initialized", func() {
		spawn(t, func(c *kernel.Configurator) { declare[Refs[game, string]](c) })
	})
	expectPanic(t, "KeyedRefs writes them", func() {
		spawn(t, func(c *kernel.Configurator) {
			RegisterKeyedRefs[game, string, mesh](c, RefsOptions{})
			declare[Refs[game, mesh]](c)
		})
	})
	expectPanic(t, "not keyed", func() {
		spawn(t, func(c *kernel.Configurator) {
			RegisterRefs[game, mesh](c, RefsOptions{})
			declare[KeyedRefs[game, string, mesh]](c)
		})
	})
}
