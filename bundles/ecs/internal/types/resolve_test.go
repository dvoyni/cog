package types

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// swapCmd is a raw kernel command, not a System, that replaces the value of the
// cell T names. It is how the tests below change what a System's handle points
// at between two of its invocations: ecs.Write refuses the ECS's own cells, so
// swapping a Store has to go around the ECS.
type swapCmd[T any] kernel.Command[T, struct{}]

func handleSwap[T any](registrar *kernel.Registrar) {
	registrar.HandleCommand[swapCmd[T]](func() (kernel.Lock, kernel.Execute[T, struct{}]) {
		cell := new(kernel.Write[T])
		return func(access kernel.ResourceAccess) { *cell = access.GetWrite[T]() },
			func(_ kernel.Kernel, with T) struct{} {
				cell.Set(with)
				return struct{}{}
			}
	})
}

type resolveSystem kernel.Subscription[app.UpdateEvent]

type resolveCmd kernel.Command[struct{}, float32]

// capturingGet is the control arm: a Get that resolves its Store on its first
// invocation and keeps it, which is the capture the per-tick resolve must not
// be. Run through the same swap, it has to observe the stale Store, or the swap
// harness cannot tell a resolve from a capture.
type capturingGet[T any] struct {
	handle   kernel.Read[*Store[T]]
	store    *Store[T]
	resolved bool
}

func (g *capturingGet[T]) prepare(en *Entities, access kernel.ResourceAccess) {
	_ = declareComponent[T](en, access, "capturingGet")
	g.handle = access.GetRead[*Store[T]]()
}

func (g *capturingGet[T]) resolve() {
	if !g.resolved {
		g.store, g.resolved = g.handle.Get(), true
	}
}

func (g *capturingGet[T]) Of(e Entity) (T, bool) { return g.store.Get(e) }

// bodySwap is a world whose body cell can be swapped between ticks: e holds A
// in the registered Store and B in second, and other is in second alone.
type bodySwap struct {
	engine *kernel.Engine
	second *Store[body]
	e      Entity
	other  Entity
}

const (
	bodyA = 1
	bodyB = 2
)

// newBodySwap builds the world. subscribe registers the System under test and
// reads e and other through the pointer it is handed, because neither exists
// until the world does.
func newBodySwap(t *testing.T, subscribe func(registrar *kernel.Registrar, world *bodySwap)) *bodySwap {
	t.Helper()
	world := &bodySwap{}
	entities, components, engine := newWorld(t, 16, func(registrar *kernel.Registrar) {
		handleSwap[*Store[body]](registrar)
		subscribe(registrar, world)
	})
	world.engine = engine
	world.e, world.other = entities.alloc(), entities.alloc()
	components.bodies.Set(world.e, body{X: bodyA})
	world.second = NewStore[body](entities, 16)
	world.second.Set(world.e, body{X: bodyB})
	world.second.Set(world.other, body{X: bodyB})
	return world
}

func (w *bodySwap) swap() {
	w.engine.Executioner().ExecuteCommand[swapCmd[*Store[body]]](w.second)
}

// acrossASwap ticks once, swaps the cell, ticks again, and returns what the
// System recorded on each tick.
func acrossASwap(t *testing.T, world *bodySwap, tick func(), seen *[]float32) (before, after float32) {
	t.Helper()
	tick()
	world.swap()
	tick()
	if len(*seen) != 2 {
		t.Fatalf("the System ran %d times over two ticks, want 2", len(*seen))
	}
	return (*seen)[0], (*seen)[1]
}

// TestAGetResolvesItsStoreEveryTick is the per-tick resolve's whole contract
// for the accessors: the Store is read out of its cell once an invocation, not
// once a call and never once at registration, so a cell replaced between two
// ticks is what the second tick sees. The control arm, which captures on its
// first resolve, is what shows the harness can see the difference.
func TestAGetResolvesItsStoreEveryTick(t *testing.T) {
	var seen []float32
	world := newBodySwap(t, func(registrar *kernel.Registrar, world *bodySwap) {
		registrar.Subscribe[resolveSystem](ToHandler[app.UpdateEvent](registrar, func(bodies *Get[body]) {
			value, _ := bodies.Of(world.e)
			seen = append(seen, value.X)
		}))
	})
	before, after := acrossASwap(t, world, func() { frame(t, world.engine, 1) }, &seen)
	if before != bodyA || after != bodyB {
		t.Fatalf("a Get saw %v then %v across a swap of its Store, want %v then %v", before, after, bodyA, bodyB)
	}
}

func TestACapturingParameterIsCaughtByTheSwap(t *testing.T) {
	var seen []float32
	world := newBodySwap(t, func(registrar *kernel.Registrar, world *bodySwap) {
		registrar.Subscribe[resolveSystem](ToHandler[app.UpdateEvent](registrar, func(bodies *capturingGet[body]) {
			value, _ := bodies.Of(world.e)
			seen = append(seen, value.X)
		}))
	})
	before, after := acrossASwap(t, world, func() { frame(t, world.engine, 1) }, &seen)
	if before != bodyA || after != bodyA {
		t.Fatalf("the capturing control saw %v then %v, want %v both times: "+
			"the swap harness cannot detect a Store captured once", before, after, bodyA)
	}
}

func TestASetResolvesItsStoreEveryTick(t *testing.T) {
	var seen []float32
	world := newBodySwap(t, func(registrar *kernel.Registrar, world *bodySwap) {
		registrar.Subscribe[resolveSystem](ToHandler[app.UpdateEvent](registrar, func(bodies *Set[body]) {
			value, _ := bodies.Of(world.e)
			seen = append(seen, value.X)
		}))
	})
	before, after := acrossASwap(t, world, func() { frame(t, world.engine, 1) }, &seen)
	if before != bodyA || after != bodyB {
		t.Fatalf("a Set saw %v then %v across a swap of its Store, want %v then %v", before, after, bodyA, bodyB)
	}
}

// TestARemoveResolvesItsStoreEveryTick removes an Entity only the swapped-in
// Store holds, so the first tick misses and the second finds it.
func TestARemoveResolvesItsStoreEveryTick(t *testing.T) {
	var seen []float32
	world := newBodySwap(t, func(registrar *kernel.Registrar, world *bodySwap) {
		registrar.Subscribe[resolveSystem](ToHandler[app.UpdateEvent](registrar, func(bodies *Remove[body]) {
			removed := float32(0)
			if bodies.From(world.other) {
				removed = 1
			}
			seen = append(seen, removed)
		}))
	})
	before, after := acrossASwap(t, world, func() { frame(t, world.engine, 1) }, &seen)
	if before != 0 || after != 1 {
		t.Fatalf("a Remove reported %v then %v across a swap of its Store, want 0 then 1", before, after)
	}
}

// TestACommandResolvesItsStoreEveryInvocation is the same swap for a System
// driven through ToExecute, which shares the call site with ToHandler.
func TestACommandResolvesItsStoreEveryInvocation(t *testing.T) {
	var seen []float32
	world := newBodySwap(t, func(registrar *kernel.Registrar, world *bodySwap) {
		registrar.HandleCommand[resolveCmd](ToExecute[struct{}, float32](registrar, func(bodies *Get[body], answer *Resp[float32]) {
			value, _ := bodies.Of(world.e)
			answer.Set(value.X)
		}))
	})
	invoke := func() {
		seen = append(seen, world.engine.Executioner().ExecuteCommand[resolveCmd](struct{}{}))
	}
	before, after := acrossASwap(t, world, invoke, &seen)
	if before != bodyA || after != bodyB {
		t.Fatalf("a command's Get saw %v then %v across a swap of its Store, want %v then %v", before, after, bodyA, bodyB)
	}
}

// resourceSwap runs system on two ticks with the *modelNames cell replaced
// between them by a raw kernel command, and returns what it recorded.
func resourceSwap(t *testing.T, system any, seen *[]float32) (before, after float32) {
	t.Helper()
	first, second := &modelNames{Scale: bodyA}, &modelNames{Scale: bodyB}
	_, _, engine := newWorldWith(t, 8,
		func(registrar *kernel.Registrar) {
			handleSwap[*modelNames](registrar)
			registrar.Subscribe[resolveSystem](ToHandler[app.UpdateEvent](registrar, system))
		},
		boundDeps, &bindingPlugin{log: &drawLog{}, names: first})
	frame(t, engine, 1)
	engine.Executioner().ExecuteCommand[swapCmd[*modelNames]](second)
	frame(t, engine, 1)
	if len(*seen) != 2 {
		t.Fatalf("the System ran %d times over two ticks, want 2", len(*seen))
	}
	return (*seen)[0], (*seen)[1]
}

func TestAReadResolvesItsResourceEveryTick(t *testing.T) {
	var seen []float32
	before, after := resourceSwap(t, func(table *Read[*modelNames]) {
		seen = append(seen, table.Get().Scale)
	}, &seen)
	if before != bodyA || after != bodyB {
		t.Fatalf("a Read saw %v then %v across a swap of its resource, want %v then %v", before, after, bodyA, bodyB)
	}
}

func TestAWriteResolvesItsResourceEveryTick(t *testing.T) {
	var seen []float32
	before, after := resourceSwap(t, func(table *Write[*modelNames]) {
		seen = append(seen, table.Get().Scale)
	}, &seen)
	if before != bodyA || after != bodyB {
		t.Fatalf("a Write saw %v then %v across a swap of its resource, want %v then %v", before, after, bodyA, bodyB)
	}
}

// TestAWriteReadsBackWhatItSet is what caching must not change: a Set followed
// by a Get on the same parameter reads the value set, inside one invocation,
// and the cell holds it afterwards.
func TestAWriteReadsBackWhatItSet(t *testing.T) {
	first, second := &modelNames{Scale: bodyA}, &modelNames{Scale: bodyB}
	var read *modelNames
	_, _, engine := newWorldWith(t, 8,
		func(registrar *kernel.Registrar) {
			registrar.Subscribe[resolveSystem](ToHandler[app.UpdateEvent](registrar, func(table *Write[*modelNames]) {
				table.Set(second)
				read = table.Get()
			}))
		},
		boundDeps, &bindingPlugin{log: &drawLog{}, names: first})
	frame(t, engine, 1)
	if read != second {
		t.Fatalf("Get after Set on one Write read %+v, want the value set, %+v", read, second)
	}
}

// TestAResolvedHandleIsAnOptInCapability pins which parameter types resolve per
// invocation. A Query binds its Stores once a run in All, and Hooks its Store
// once a run in beginRun, so neither has anything to resolve.
func TestAResolvedHandleIsAnOptInCapability(t *testing.T) {
	resolverType := reflect.TypeFor[resolver]()
	for _, resolving := range []reflect.Type{
		reflect.TypeFor[*Get[body]](), reflect.TypeFor[*Set[body]](), reflect.TypeFor[*Remove[body]](),
		reflect.TypeFor[*Spawn[spawnSet]](), reflect.TypeFor[*WriteableEntities](),
		reflect.TypeFor[*Read[*modelNames]](), reflect.TypeFor[*Write[*modelNames]](),
	} {
		if !resolving.Implements(resolverType) {
			t.Errorf("%v does not resolve its handles per invocation", resolving)
		}
	}
	for _, binding := range []reflect.Type{
		reflect.TypeFor[*Query[moveQuery]](), reflect.TypeFor[*Hooks[body, HookAddedChanged]](),
	} {
		if binding.Implements(resolverType) {
			t.Errorf("%v resolves per invocation, but it already binds once a run", binding)
		}
	}
}
