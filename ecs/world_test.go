package ecs

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

func TestEntityLayout(t *testing.T) {
	e := makeEntity(7, 3)
	if e.index() != 7 || e.generation() != 3 {
		t.Fatalf("entity holds index %d generation %d, want 7 and 3", e.index(), e.generation())
	}
	if nextGeneration(^uint32(0)) != 1 {
		t.Fatal("a wrapping generation does not skip 0")
	}
}

func TestMaskBits(t *testing.T) {
	var m game
	maskSet(&m, 3)
	maskSet(&m, 100)
	maskUnset(&m, 3)
	if maskHas(&m, 3) || !maskHas(&m, 100) || m[1] != 1<<36 {
		t.Fatalf("mask is %b after setting 3 and 100 and unsetting 3", m)
	}
	var spread game
	for _, id := range []ComponentID{127, 0, 64, 63} {
		maskSet(&spread, id)
	}
	if got := slices.Collect(maskIDs(&spread)); !slices.Equal(got, []ComponentID{0, 63, 64, 127}) {
		t.Fatalf("maskIDs yielded %v, want 0 63 64 127", got)
	}
	for id := range maskIDs(&spread) {
		if id != 0 {
			t.Fatalf("maskIDs yielded %d after the loop broke", id)
		}
		break
	}
	if maskBits[game]() != 128 {
		t.Fatalf("game holds %d component types, want 128", maskBits[game]())
	}
}

func TestSpawnHandsOutTheAllocationRange(t *testing.T) {
	type small [1]uint64
	var w World[small]
	s := kernel.NewSupervisor(nil)
	t.Cleanup(s.Quit)
	_, err := s.Spawn(nil, 1, NewPlugin(), testPlugin{init: func(c *kernel.Configurator) {
		RegisterWorld[small](c, WorldOptions{FirstIndex: 10, LastIndex: 12})
		w = declare[World[small]](c)
	}})
	if err != nil {
		t.Fatal(err)
	}
	for want := uint32(10); want <= 12; want++ {
		if e := w.Spawn(); e.index() != want || e.generation() != 1 || !w.Alive(e) {
			t.Fatalf("Spawn answered index %d generation %d, want %d and 1, alive", e.index(), e.generation(), want)
		}
	}
	if e := w.Spawn(); e != NoEntity {
		t.Fatalf("Spawn past the range answered %#x, want NoEntity", uint64(e))
	}
}

func TestCreateAtPlacesOutsideTheRange(t *testing.T) {
	var w World[game]
	spawn(t, func(c *kernel.Configurator) { w = declare[World[game]](c) })
	mirrored := makeEntity(1<<30, 5)
	if err := w.CreateAt(mirrored); err != nil || !w.Alive(mirrored) {
		t.Fatalf("CreateAt failed: %v", err)
	}
	if err := w.CreateAt(makeEntity(1<<30, 6)); err == nil {
		t.Fatal("CreateAt placed an entity where one is alive")
	}
	if err := w.CreateAt(makeEntity(3, 1)); err == nil {
		t.Fatal("CreateAt placed an entity inside the allocation range")
	}
	if err := w.CreateAt(makeEntity(1<<29, 0)); err == nil {
		t.Fatal("CreateAt placed an entity of generation 0")
	}
	if w.Alive(makeEntity(1<<30, 4)) || w.Alive(NoEntity) {
		t.Fatal("a stale generation or NoEntity is alive")
	}
}

func TestDespawnRemovesEveryComponent(t *testing.T) {
	var w World[game]
	var positions Store[game, position]
	var tags Store[game, frozen]
	spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, position](c)
		RegisterComponent[game, frozen](c)
		w = declare[World[game]](c)
		positions = declare[Store[game, position]](c)
		tags = declare[Store[game, frozen]](c)
	})
	a := w.Spawn()
	b := w.Spawn()
	positions.Add(a, position{X: 1})
	positions.Add(b, position{X: 2})
	tags.Add(a, frozen{})
	if !w.Despawn(a) || w.Despawn(a) || w.Alive(a) {
		t.Fatal("Despawn did not end a, or ended it twice")
	}
	if positions.Has(a) || tags.Has(a) || positions.Get(b).X != 2 {
		t.Fatal("Despawn left a's components or disturbed b's")
	}
	if positions.Add(a, position{}) {
		t.Fatal("Add on a despawned entity succeeded")
	}
	if c := w.Spawn(); c != makeEntity(a.index(), 2) {
		t.Fatalf("Spawn answered %#x, want a's index reused at once, as nothing observes", uint64(c))
	}
}

func TestRegisterWorldCompositionErrors(t *testing.T) {
	s := kernel.NewSupervisor(nil)
	t.Cleanup(s.Quit)
	spawnWith := func(plugins ...kernel.Plugin) { s.Spawn(nil, 1, plugins...) }
	expectPanic(t, "before the ecs plugin", func() {
		spawnWith(testPlugin{init: func(c *kernel.Configurator) { RegisterWorld[game](c, WorldOptions{}) }})
	})
	expectPanic(t, "registered twice", func() {
		spawnWith(NewPlugin(), testPlugin{init: func(c *kernel.Configurator) {
			RegisterWorld[game](c, WorldOptions{})
			RegisterWorld[game](c, WorldOptions{})
		}})
	})
	expectPanic(t, "empty allocation range", func() {
		spawnWith(NewPlugin(), testPlugin{init: func(c *kernel.Configurator) {
			RegisterWorld[game](c, WorldOptions{FirstIndex: 5, LastIndex: 4})
		}})
	})
}

// TestWorldAllocatesNothingOnceGrown covers what the other allocation tests
// leave out: spawning and despawning directly, the typed and raw World
// methods, and commits, with observation on and off. Observing, a round also
// abandons reservations, which the reset reclaims; without observation such
// an index is lost, so the round abandons none.
func TestWorldAllocatesNothingOnceGrown(t *testing.T) {
	for _, observing := range []bool{false, true} {
		o := spawnCommands(t)
		if observing {
			ObservationReset(nil, kernel.NoArg{}, o.world)
		}
		w := o.world
		healthID := w.ComponentID[health]()
		var mask game
		maskSet(&mask, healthID)
		values := []byte{1, 0, 0, 0}
		spawned := make([]Entity, 0, 300)
		round := func() {
			spawned = spawned[:0]
			for i := range 100 {
				e := w.Spawn()
				w.Add(e, health{Points: int32(i)})
				w.Write[health](e).Points++
				if !w.Has[health](e) || w.Get[health](e) == nil {
					t.Fatal("a spawned entity lost its health")
				}
				w.WriteRaw(e, healthID)[0]++
				w.AddRaw(e, healthID, w.ReadRaw(e, healthID))
				spawned = append(spawned, e)
			}
			for range 50 {
				spawned = append(spawned, w.SpawnRaw(mask, values))
			}
			for i := range 50 {
				e := o.first.Reserve()
				o.first.Spawn(e)
				o.first.Add(e, health{Points: 1})
				o.first.Set(spawned[i], health{Points: 2})
				o.second.Remove[health](spawned[i+50])
				spawned = append(spawned, e)
				if observing {
					o.second.Reserve() // abandoned, reclaimed by the reset
				}
			}
			o.sink.Commit()
			for _, e := range spawned {
				w.Despawn(e)
			}
			if observing {
				for range o.changes.All() {
				}
				ObservationReset(nil, kernel.NoArg{}, w)
			}
		}
		round()
		round()
		if got := testing.AllocsPerRun(20, round); got != 0 {
			t.Fatalf("observing %v: a round of spawns, typed and raw access, commits and despawns allocated %v times", observing, got)
		}
	}
}
