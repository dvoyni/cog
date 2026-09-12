package ecs

import (
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// What reaching another Entity costs. The spec recorded these numbers from a
// model of the Store rather than from a real engine, and said so: Get, Set and
// Remove were the three handles the zero-allocation prototype never built, so
// their allocation behaviour in situ was inferred from the other handles. These
// benchmarks are what settles that — the same whole-frame harness the Query and
// the Spawn are measured on, publish to wait, on a real kernel.Engine driven by
// a real app.UpdateEvent.

// populateHoming gives n Entities a body, a velocity and a Reference to another
// one of them, chosen by a stride coprime with the population so the probed rows
// are hit in an order unrelated to the walk. That scatter is the realistic case
// and the one the spec measured: a target is not the Entity next to you.
func populateHoming(entities *Entities, components *componentsPlugin, n int) {
	ids := make([]Entity, n)
	for i := range ids {
		e := entities.alloc()
		ids[i] = e
		components.bodies.Set(e, body{X: float32(i)})
		components.velocities.Set(e, velocity{X: 1, Y: 2})
	}
	for i, e := range ids {
		components.homings.Set(e, homing{Target: ids[(i*7919+13)%n]})
	}
}

// subscribeHoming is the real homing shape: a two-Component Query over the near
// Entity, and one scattered probe per Entity through the Reference it carries.
func subscribeHoming(registrar *kernel.Registrar, world *Entities) {
	registrar.Subscribe[homingSystem](ToHandler[app.UpdateEvent](world,
		func(q *Query[homingQuery], bodies *Get[body]) {
			for _, it := range q.All() {
				target, ok := bodies.Of(it.Homing.Target)
				if !ok {
					continue
				}
				it.Body.Y += target.X
			}
		}))
}

// subscribeHomingHandWritten is that shape with the loop and the probe written
// out, which only a test in this package can do. It is the baseline the accessor
// is read against: the same walk, the same scattered probe, no handle.
func subscribeHomingHandWritten(registrar *kernel.Registrar, _ *Entities) {
	registrar.Subscribe[handSystem](func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
		var entities kernel.Read[*Entities]
		var bodies kernel.Write[*Store[body]]
		var homings kernel.Read[*Store[homing]]
		return func(access kernel.ResourceAccess) {
				entities = access.GetRead[*Entities]()
				bodies = access.GetWrite[*Store[body]]()
				homings = access.GetRead[*Store[homing]]()
			}, func(_ kernel.Kernel, _ app.UpdateEvent) error {
				_ = entities
				driver, probed := homings.Get(), bodies.Get()
				for row := len(driver.owners) - 1; row >= 0; row-- {
					e := driver.owners[row]
					near, ok := probed.probe(e)
					if !ok {
						continue
					}
					far, ok := probed.probe(driver.dense[row].Target)
					if !ok {
						continue
					}
					probed.dense[near].Y += probed.dense[far].X
				}
				return nil
			}
	})
}

// subscribeChurning is the other half: a Component added and taken away again
// every Entity, every tick, through the two handles that do it. The whole of
// what it declares is write{*Store[collider]} — there is no barrier here,
// because adding a Component names one Store and a spawn names the authority.
func subscribeChurning(registrar *kernel.Registrar, world *Entities) {
	registrar.Subscribe[accessorSystem](ToHandler[app.UpdateEvent](world,
		func(q *Query[moveQuery], colliders *Set[collider], strip *Remove[collider]) {
			for e, it := range q.All() {
				colliders.UpdateFor(e, collider{Radius: it.Body.X})
				value, _ := colliders.Of(e)
				it.Body.Y = value.Radius
				strip.From(e)
			}
		}))
}

func BenchmarkFrameHoming1k(b *testing.B) {
	benchmarkFrameWith(b, 1_000, subscribeHoming, populateHoming)
}

func BenchmarkFrameHoming10k(b *testing.B) {
	benchmarkFrameWith(b, 10_000, subscribeHoming, populateHoming)
}

func BenchmarkFrameHomingHandWritten1k(b *testing.B) {
	benchmarkFrameWith(b, 1_000, subscribeHomingHandWritten, populateHoming)
}

func BenchmarkFrameHomingHandWritten10k(b *testing.B) {
	benchmarkFrameWith(b, 10_000, subscribeHomingHandWritten, populateHoming)
}

func BenchmarkFrameChurning1k(b *testing.B) {
	benchmarkFrameWith(b, 1_000, subscribeChurning, populate)
}

func BenchmarkFrameChurning10k(b *testing.B) {
	benchmarkFrameWith(b, 10_000, subscribeChurning, populate)
}

// The handles alone, outside a frame, so the per-call cost is visible beside the
// per-frame one. A Store reached directly is the baseline; the difference is the
// handle's own — a type assertion out of the resource cell and nothing else.

const accessorBatch = 4096

func BenchmarkGetOf(b *testing.B) {
	world, ids := accessorPopulation(b, accessorBatch)
	b.ReportAllocs()
	b.ResetTimer()
	var sink float32
	for i := 0; i < b.N; i++ {
		value, _ := world.bodyGet.Of(ids[i%accessorBatch])
		sink += value.X
	}
	_ = sink
}

// BenchmarkGetOfHandWritten is the same probe on the same Store with the
// resource cell dereferenced once, outside the loop, instead of on every call.
// The difference from BenchmarkGetOf is therefore exactly one thing — the type
// assertion out of the any-typed cell that a handle's Get performs — which is
// what lets that cost be attributed rather than guessed at.
func BenchmarkGetOfHandWritten(b *testing.B) {
	world, ids := accessorPopulation(b, accessorBatch)
	store := world.components.bodies
	b.ReportAllocs()
	b.ResetTimer()
	var sink float32
	for i := 0; i < b.N; i++ {
		value, _ := store.Get(ids[i%accessorBatch])
		sink += value.X
	}
	_ = sink
}

func BenchmarkSetRef(b *testing.B) {
	world, ids := accessorPopulation(b, accessorBatch)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ref, _ := world.bodySet.Ref(ids[i%accessorBatch])
		ref.X++
	}
}

// BenchmarkSetUpdateForReplacing is the value write, where the Entity already
// has the Component: one probe and one store, and no structural change at all.
func BenchmarkSetUpdateForReplacing(b *testing.B) {
	world, ids := accessorPopulation(b, accessorBatch)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		world.bodySet.UpdateFor(ids[i%accessorBatch], body{X: 1})
	}
}

// BenchmarkSetUpdateForInsertingAndRemoving is the structural pair: a Component
// added to an Entity that has none and taken away again, which is what the
// absence of a command buffer means in practice.
func BenchmarkSetUpdateForInsertingAndRemoving(b *testing.B) {
	world, ids := accessorPopulation(b, accessorBatch)
	// The steady state a game runs in: the Store has its rows and the reserve
	// hint has been paid for, so nothing here is measuring growth.
	for _, e := range ids {
		world.colliderSet.UpdateFor(e, collider{Radius: 1})
		world.colliderRemove.From(e)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := ids[i%accessorBatch]
		world.colliderSet.UpdateFor(e, collider{Radius: 1})
		world.colliderRemove.From(e)
	}
}

// accessorPopulation is a world with the accessors held and a population that
// already has the Component, so a benchmark measures the probe rather than the
// growth. The ids are returned shuffled by the same stride the homing benchmarks
// scatter with, so the dense rows are hit out of order.
func accessorPopulation(tb testing.TB, n int) (*accessorWorld, []Entity) {
	tb.Helper()
	world := accessors(tb, uint32(n))
	ordered := make([]Entity, n)
	for i := range ordered {
		e := world.entities.alloc()
		ordered[i] = e
		world.components.bodies.Set(e, body{X: float32(i)})
	}
	ids := make([]Entity, n)
	for i := range ids {
		ids[i] = ordered[(i*7919+13)%n]
	}
	return world, ids
}

// TestTheAccessorsStayOnTheEnginesAllocationLine is the Gap the spec recorded,
// closed with a measurement instead of an inference. The bar is the engine's
// own: 2 allocations per publication plus 4 per subscriber, identical at 1 000
// and at 10 000 Entities — because an allocation inside an accessor would scale
// with how many times it is called, which is once per Entity here.
//
// It measures a steady state over ten thousand frames rather than an average
// over b.N, because an average can hide amortised growth.
func TestTheAccessorsStayOnTheEnginesAllocationLine(t *testing.T) {
	const frames = 10_000
	measure := func(n int, subscribe func(*kernel.Registrar, *Entities),
		fill func(*Entities, *componentsPlugin, int),
	) float64 {
		entities, components, engine := newWorld(t, uint32(n), subscribe)
		fill(entities, components, n)
		executioner := engine.Executioner()
		for range 100 {
			frame(t, engine, 1)
		}
		mallocs := allocationsDuring(func() {
			for range frames {
				if err := executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
					t.Fatalf("publishing the update: %v", err)
				}
			}
		})
		return float64(mallocs) / frames
	}

	hand := measure(1_000, subscribeHandWritten, populate)
	homing1k := measure(1_000, subscribeHoming, populateHoming)
	homing10k := measure(10_000, subscribeHoming, populateHoming)
	// The churning arm adds and removes a Component per Entity per tick, so it is
	// where an allocation charged per structural change would appear — ten
	// thousand of them a frame at 10k.
	churn1k := measure(1_000, subscribeChurning, populate)
	churn10k := measure(10_000, subscribeChurning, populate)
	t.Logf("objects a frame: hand-written %.3f, a Get through a Reference at 1k %.3f, at 10k %.3f, "+
		"an UpdateFor and a From per Entity at 1k %.3f, at 10k %.3f",
		hand, homing1k, homing10k, churn1k, churn10k)

	for _, arm := range []struct {
		name       string
		small, big float64
	}{
		{"a Get through a Reference", homing1k, homing10k},
		{"an UpdateFor and a From per Entity", churn1k, churn10k},
	} {
		if arm.small > hand+0.05 {
			t.Fatalf("%s costs %.3f objects a frame against the hand-written %.3f", arm.name, arm.small, hand)
		}
		if arm.big > arm.small+0.05 {
			t.Fatalf("%s allocates per Entity: %.3f a frame at 1k, %.3f at 10k", arm.name, arm.small, arm.big)
		}
		if arm.big > 6.5 {
			t.Fatalf("%s costs %.3f objects a frame, above the engine's 6-per-frame line", arm.name, arm.big)
		}
	}
}

// TestAnAccessorCallAllocatesNothing prices the call itself rather than the
// frame, which is the finer instrument: a frame's six objects would hide one
// small allocation per Entity behind the engine's own noise only if the
// population were tiny, and this counts the calls directly.
func TestAnAccessorCallAllocatesNothing(t *testing.T) {
	world, ids := accessorPopulation(t, 64)
	for _, e := range ids {
		world.colliderSet.UpdateFor(e, collider{Radius: 1})
		world.colliderRemove.From(e)
	}
	i := 0
	next := func() Entity {
		i = (i + 1) % len(ids)
		return ids[i]
	}

	for _, accessor := range []struct {
		name string
		call func()
	}{
		{"Get.Of", func() { world.bodyGet.Of(next()) }},
		{"Set.Of", func() { world.bodySet.Of(next()) }},
		{"Set.Ref", func() { world.bodySet.Ref(next()) }},
		{"Set.UpdateFor, replacing", func() { world.bodySet.UpdateFor(next(), body{X: 1}) }},
		{"Set.UpdateFor and Remove.From, a structural pair", func() {
			e := next()
			world.colliderSet.UpdateFor(e, collider{Radius: 1})
			world.colliderRemove.From(e)
		}},
	} {
		if objects := testing.AllocsPerRun(1000, accessor.call); objects != 0 {
			t.Fatalf("%s allocated %v objects a call, want 0", accessor.name, objects)
		}
	}
}
