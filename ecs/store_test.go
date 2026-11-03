package ecs

import (
	"math/rand/v2"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

func TestStoreAddGetRemoveSwapsLastRow(t *testing.T) {
	var w World[game]
	var positions Store[game, position]
	var lookup Lookup[game, position]
	spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, position](c)
		w = declare[World[game]](c)
		positions = declare[Store[game, position]](c)
		lookup = declare[Lookup[game, position]](c)
	})
	var entities []Entity
	for i := range 300 { // spans two chunks
		e := w.Spawn()
		entities = append(entities, e)
		positions.Add(e, position{X: float32(i)})
	}
	positions.Write(entities[5]).Y = 9
	if got, ok := lookup.Get(entities[5]); !ok || got != (position{X: 5, Y: 9}) {
		t.Fatalf("Lookup answered %v, %v after a write, want {5 9}", got, ok)
	}
	if _, ok := lookup.Get(w.Spawn()); ok {
		t.Fatal("Lookup answered an entity without the component")
	}
	if !positions.Remove(entities[0]) || positions.Remove(entities[0]) || lookup.Has(entities[0]) {
		t.Fatal("Remove did not remove once")
	}
	for i, e := range entities[1:] {
		if got := positions.Get(e); got == nil || got.X != float32(i+1) {
			t.Fatalf("entity %d holds %v after a swap-remove, want X %d", i+1, got, i+1)
		}
	}
	slot := w.world.entities.slots.at(entities[0].index())
	if maskHas(&slot.componentMask, positions.store.component.ID) {
		t.Fatal("Remove left the entity's mask bit set")
	}
	positions.Add(entities[1], position{X: 42})
	if positions.store.len() != 299 || positions.Get(entities[1]).X != 42 {
		t.Fatal("Add of a component the entity has did not replace it in place")
	}
}

func TestStoreAllocatesNothingOnceGrown(t *testing.T) {
	var w World[game]
	var positions Store[game, position]
	spawn(t, func(c *kernel.Configurator) {
		RegisterComponent[game, position](c)
		w = declare[World[game]](c)
		positions = declare[Store[game, position]](c)
	})
	entities := make([]Entity, 500)
	for i := range entities {
		entities[i] = w.Spawn()
	}
	cycle := func() {
		for _, e := range entities {
			positions.Add(e, position{X: 1})
		}
		for _, e := range entities {
			positions.Write(e).X++
			positions.Remove(e)
		}
	}
	cycle()
	if got := testing.AllocsPerRun(10, cycle); got != 0 {
		t.Fatalf("Add, Write and Remove allocated %v times", got)
	}
}

func TestRegisterComponentCompositionErrors(t *testing.T) {
	type small [1]uint64
	s := kernel.NewSupervisor(nil)
	t.Cleanup(s.Quit)
	spawnWith := func(init func(c *kernel.Configurator)) {
		s.Spawn(nil, 1, NewPlugin(), testPlugin{init: func(c *kernel.Configurator) {
			RegisterWorld[small](c, WorldOptions{})
			init(c)
		}})
	}
	expectPanic(t, "not pointer-free", func() {
		spawnWith(func(c *kernel.Configurator) { RegisterComponent[small, struct{ Name string }](c) })
	})
	expectPanic(t, "not registered", func() {
		spawnWith(func(c *kernel.Configurator) { RegisterComponent[game, position](c) })
	})
	expectPanic(t, "twice", func() {
		spawnWith(func(c *kernel.Configurator) {
			RegisterComponent[small, position](c)
			RegisterComponent[small, position](c)
		})
	})
	expectPanic(t, "no more than 64", func() {
		spawnWith(func(c *kernel.Configurator) {
			w := registeredWorld[small](c)
			w.stores = make([]*storeResource, 64) // as if 64 were registered
			RegisterComponent[small, position](c)
		})
	})
}

// storeDeltas answers the delta of every row of s that has one, live or in
// the tail, by owner, converted to health, visiting the rows a delta query
// visits. A store that does not observe has none, and an owner visited
// twice fails t.
func storeDeltas(t *testing.T, s *storeResource) map[Entity]observedDelta[health] {
	got := map[Entity]observedDelta[health]{}
	if !s.observed() {
		return got
	}
	visited := map[Entity]bool{}
	for i := range s.walkedRows() {
		row := i
		if !s.changedEntitiesOverflowed {
			row = s.listedRow(i)
		}
		owner := *s.owners.At(row)
		if visited[owner] {
			t.Fatalf("entity %#x visited twice", uint64(owner))
		}
		visited[owner] = true
		delta := s.delta(row)
		if delta == DeltaNone {
			continue
		}
		converted := observedDelta[health]{Delta: delta, Value: *(*health)(s.address(row))}
		if delta != DeltaAdded {
			converted.Previous = *(*health)(s.shadow(row))
		}
		got[owner] = converted
	}
	return got
}

// TestStoreChangesMatchAReferenceModel drives one store directly. Its
// entities share 16 indices: despawning one moves on to the next generation
// at its index, so a removed row in the tail and a live row often share an
// index.
func TestStoreChangesMatchAReferenceModel(t *testing.T) {
	for seed := range uint64(10) {
		s := &newStoreOf[game, health](0).storeResource
		s.registerObserver()
		m := &changeModel{atReset: map[Entity]health{}, now: map[Entity]health{}, last: map[Entity]health{}}
		random := rand.New(rand.NewPCG(seed, 5))
		var generations [16]uint32
		for i := range generations {
			generations[i] = 1
		}
		pick := func() Entity {
			index := random.IntN(len(generations))
			return makeEntity(uint32(index), generations[index])
		}
		put := func(e Entity, value health) {
			row, ok := s.row(e)
			if ok {
				s.markChanged(row, e)
			} else {
				row = s.add(e)
			}
			*(*health)(s.address(row)) = value
			m.now[e] = value
		}
		for range 10 { // before observation starts, so every row counts as added
			put(pick(), health{Points: random.Int32N(4)})
		}
		s.startObservation()
		for step := range 5000 {
			switch op := random.IntN(100); {
			case op < 35:
				put(pick(), health{Points: random.Int32N(4)})
			case op < 55:
				e := pick()
				if row, ok := s.row(e); ok {
					s.remove(e, row)
					m.remove(e)
				}
			case op < 65:
				e := pick()
				if row, ok := s.row(e); ok {
					s.remove(e, row)
					m.remove(e)
				}
				generations[e.index()]++ // despawned: the index moves on
			case op < 92:
				e := pick()
				if row, ok := s.row(e); ok {
					s.markChanged(row, e)
					value := (*health)(s.address(row))
					value.Points = random.Int32N(4) // often back to what it was
					m.now[e] = *value
				}
			default:
				s.resetObservation()
				m.atReset = map[Entity]health{}
				for e, value := range m.now {
					m.atReset[e] = value
				}
				m.last = map[Entity]health{}
			}
			got := storeDeltas(t, s)
			want := m.expected()
			if len(got) != len(want) {
				t.Fatalf("seed %d step %d: %d changes, want %d:\n got %v\nwant %v", seed, step, len(got), len(want), got, want)
			}
			for e, change := range want {
				if got[e] != change {
					t.Fatalf("seed %d step %d: entity %#x changed %+v, want %+v", seed, step, uint64(e), got[e], change)
				}
			}
			for e, value := range m.now {
				if row, ok := s.row(e); !ok || *(*health)(s.address(row)) != value {
					t.Fatalf("seed %d step %d: entity %#x lost its value %v", seed, step, uint64(e), value)
				}
			}
			if s.len() != len(m.now) {
				t.Fatalf("seed %d step %d: %d live rows, want %d", seed, step, s.len(), len(m.now))
			}
		}
	}
}

func TestStoreObservesOnlyWithAnObserver(t *testing.T) {
	s := &newStoreOf[game, health](0).storeResource
	s.add(makeEntity(1, 1))
	s.startObservation()
	if s.observed() || len(storeDeltas(t, s)) != 0 {
		t.Fatal("a store nobody observes started observing")
	}
	s.registerObserver()
	if s.observed() {
		t.Fatal("a store observed before the world's first reset")
	}
	s.startObservation()
	if !s.observed() || storeDeltas(t, s)[makeEntity(1, 1)].Delta != DeltaAdded {
		t.Fatal("the first reset did not start observing with every row added")
	}
}
