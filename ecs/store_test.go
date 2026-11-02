package ecs

import (
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
	if !positions.Remove(entities[0]) || positions.Remove(entities[0]) || lookup.Has(entities[0]) {
		t.Fatal("Remove did not remove once")
	}
	for i, e := range entities[1:] {
		if got := positions.Get(e); got == nil || got.X != float32(i+1) {
			t.Fatalf("entity %d holds %v after a swap-remove, want X %d", i+1, got, i+1)
		}
	}
	slot := w.world.entities.at(entities[0].index())
	if maskHas(&slot.mask, positions.store.component.ID) {
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
			w.stores = make([]*store, 64) // as if 64 were registered
			RegisterComponent[small, position](c)
		})
	})
}
