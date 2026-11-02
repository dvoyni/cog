package ecs

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

type tick struct{ Dt float32 }

type settings struct{ Speed float32 }

func move(k *kernel.Kernel, t tick, q Query2[game, *position, velocity], s kernel.Read[settings]) kernel.NoResult {
	for _, row := range q.All() {
		pos, vel := row.Get()
		pos.X += vel.X * t.Dt * s.Get().Speed
	}
	return kernel.NoResult{}
}

func TestSystemRunsAsATask(t *testing.T) {
	var task *kernel.Task[tick, kernel.NoResult]
	var lookup Lookup[game, position]
	h, entities := queryWorld(t, 4, func(c *kernel.Configurator) {
		c.InitResource(settings{Speed: 2})
		task = c.RegisterTask2(move)
		lookup = declare[Lookup[game, position]](c)
	})
	h.run(func(ex *kernel.Executor) kernel.JobHandle {
		return ex.Schedule(task, tick{Dt: 0.5}).Handle()
	})
	for i, e := range entities {
		want := float32(i)
		if i%2 == 0 {
			want++
		}
		if got, _ := lookup.Get(e); got.X != want {
			t.Fatalf("entity %d holds X %v after the system, want %v", i, got.X, want)
		}
	}
}

func TestSystemCallAllocatesNothing(t *testing.T) {
	var q Query2[game, *position, velocity]
	var s kernel.Read[settings]
	queryWorld(t, 1000, func(c *kernel.Configurator) {
		c.InitResource(settings{Speed: 1})
		q = declare[Query2[game, *position, velocity]](c)
		s = declare[kernel.Read[settings]](c)
	})
	if got := testing.AllocsPerRun(100, func() { move(nil, tick{Dt: 1}, q, s) }); got != 0 {
		t.Fatalf("a system call allocated %v times", got)
	}
}

// toggle adds T to every entity with a position that lacks it, and removes
// it from every one that has it.
func toggle[T any](k *kernel.Kernel, _ kernel.NoArg, q Query1[game, position], s Store[game, T]) kernel.NoResult {
	for e := range q.All() {
		if !s.Remove(e) {
			s.Add(e, *new(T))
		}
	}
	return kernel.NoResult{}
}

func TestSystemsOnDifferentStoresKeepMasksApart(t *testing.T) {
	var toggleVelocity, toggleFrozen *kernel.Task[kernel.NoArg, kernel.NoResult]
	var w World[game]
	h, entities := queryWorld(t, 2000, func(c *kernel.Configurator) {
		toggleVelocity = c.RegisterTask2(toggle[velocity])
		toggleFrozen = c.RegisterTask2(toggle[frozen])
		w = declare[World[game]](c)
	})
	const rounds = 5 // odd, so each ends toggled
	for range rounds {
		h.run(func(ex *kernel.Executor) kernel.JobHandle {
			a := ex.Schedule(toggleVelocity, kernel.NoArg{})
			b := ex.Schedule(toggleFrozen, kernel.NoArg{})
			return ex.Join(a.Handle(), b.Handle())
		})
	}
	velocityID := w.world.storesByType[reflect.TypeFor[velocity]()].component.ID
	frozenID := w.world.storesByType[reflect.TypeFor[frozen]()].component.ID
	for i, e := range entities {
		mask := &w.world.entities.at(e.index()).mask
		if maskHas(mask, velocityID) != (i%2 != 0) || maskHas(mask, frozenID) != (i%3 != 0) {
			t.Fatalf("entity %d's mask is %b after toggling both stores", i, *mask)
		}
	}
}
