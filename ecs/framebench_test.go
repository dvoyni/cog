package ecs

import (
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

type handSystem kernel.Subscription[app.UpdateEvent]

// handWritten is the baseline every number here is read against: the same work,
// the same locks and the same engine, with the loop written out by hand. It
// reaches the Stores directly, which only a test in this package can do — a
// System's only route to one is a Query.
func handWritten() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var entities kernel.Read[*Entities]
	var bodies kernel.Write[*Store[body]]
	var velocities kernel.Read[*Store[velocity]]
	return func(access kernel.ResourceAccess) {
			entities = access.GetRead[*Entities]()
			bodies = access.GetWrite[*Store[body]]()
			velocities = access.GetRead[*Store[velocity]]()
		}, func(_ kernel.Kernel, _ app.UpdateEvent) error {
			_ = entities
			driver, probed := bodies.Get(), velocities.Get()
			for row := len(driver.owners) - 1; row >= 0; row-- {
				other, ok := probed.probe(driver.owners[row])
				if !ok {
					continue
				}
				driver.dense[row].X += probed.dense[other].X
				driver.dense[row].Y += probed.dense[other].Y
			}
			return nil
		}
}

func subscribeHandWritten(registrar *kernel.Registrar, _ *Entities) {
	registrar.Subscribe[handSystem](handWritten)
}

// populate gives n entities a body and a velocity, before the frame that
// measures them: the tests and benchmarks here are about iteration, and
// spawning is a later ticket's.
func populate(entities *Entities, components *componentsPlugin, n int) {
	for range n {
		e := entities.alloc()
		components.bodies.Set(e, body{})
		components.velocities.Set(e, velocity{X: 1, Y: 2})
	}
}

// populateFiltered is the same population with a Tag on a third of it, for the
// Systems that carry a Without. The excluded entities are still in the driver's
// Store, which is the point: a filter narrows what is yielded and never what is
// walked.
func populateFiltered(entities *Entities, components *componentsPlugin, n int) {
	for i := range n {
		e := entities.alloc()
		components.bodies.Set(e, body{})
		components.velocities.Set(e, velocity{X: 1, Y: 2})
		if i%3 == 0 {
			components.disableds.Set(e, disabled{})
		}
	}
}

func subscribeActive(registrar *kernel.Registrar, world *Entities) {
	registrar.Subscribe[activeSystem](ToHandler[app.UpdateEvent](world,
		func(q *Query[activeQuery]) {
			for _, it := range q.All() {
				it.Body.X += it.Velocity.X
				it.Body.Y += it.Velocity.Y
			}
		}))
}

// frame publishes one real app.UpdateEvent and waits for it, which is the whole
// frame: publish, acquire every declared lock, run every System, wait.
func frame(tb testing.TB, engine *kernel.Engine, dt float64) {
	if err := engine.Executioner().PublishEvent(app.UpdateEvent{Dt: dt}).Wait(); err != nil {
		tb.Fatalf("publishing the update: %v", err)
	}
}

func benchmarkFrame(b *testing.B, n int, subscribe func(*kernel.Registrar, *Entities)) {
	benchmarkFrameWith(b, n, subscribe, populate)
}

func benchmarkFrameWith(b *testing.B, n int, subscribe func(*kernel.Registrar, *Entities),
	fill func(*Entities, *componentsPlugin, int),
) {
	entities, components, engine := newWorld(b, uint32(n), subscribe)
	fill(entities, components, n)
	executioner := engine.Executioner()

	b.ReportAllocs()
	b.ResetTimer()
	// The classic b.N form, deliberately: testing.B.Loop wraps loop-assigned
	// variables in runtime.KeepAlive, which pinned an accumulator to memory and
	// made two iteration shapes differing by 4x look identical.
	for i := 0; i < b.N; i++ {
		if err := executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
			b.Fatalf("publishing the update: %v", err)
		}
	}
}

func BenchmarkFrameQuery1k(b *testing.B)  { benchmarkFrame(b, 1_000, subscribeMove) }
func BenchmarkFrameQuery10k(b *testing.B) { benchmarkFrame(b, 10_000, subscribeMove) }

// BenchmarkFrameFiltered1k and its 10k twin are the two-Component Query with a
// Without on a third of the population: the same walk, one more probe, and one
// entity in three yielding nothing.
func BenchmarkFrameFiltered1k(b *testing.B) {
	benchmarkFrameWith(b, 1_000, subscribeActive, populateFiltered)
}

func BenchmarkFrameFiltered10k(b *testing.B) {
	benchmarkFrameWith(b, 10_000, subscribeActive, populateFiltered)
}

func BenchmarkFrameHandWritten1k(b *testing.B) { benchmarkFrame(b, 1_000, subscribeHandWritten) }
func BenchmarkFrameHandWritten10k(b *testing.B) {
	benchmarkFrame(b, 10_000, subscribeHandWritten)
}

// BenchmarkFrameNoSystem is the other end of the line: the same publication with
// nothing subscribed, so the engine's own charge is visible beside the ECS's.
func BenchmarkFrameNoSystem(b *testing.B) {
	benchmarkFrame(b, 1_000, func(*kernel.Registrar, *Entities) {})
}

// TestTheFrameSitsOnTheEnginesAllocationLine measures a steady state rather than
// an average over b.N, because an average can hide amortised growth: ten
// thousand frames, counted with MemStats, at two entity counts an order of
// magnitude apart. An allocation in the iteration would scale with the entity
// count, so identical counts at 1k and 10k are the claim.
func TestTheFrameSitsOnTheEnginesAllocationLine(t *testing.T) {
	const frames = 10_000
	measure := func(n int, subscribe func(*kernel.Registrar, *Entities),
		fill func(*Entities, *componentsPlugin, int),
	) float64 {
		entities, components, engine := newWorld(t, uint32(n), subscribe)
		fill(entities, components, n)
		executioner := engine.Executioner()
		// Warm every pool the first frames fill, so what is measured is steady
		// state and not the first tick.
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
	query1k := measure(1_000, subscribeMove, populate)
	query10k := measure(10_000, subscribeMove, populate)
	// A filter is a third Store probed per Entity and a third of the population
	// yielding nothing, so it is where an allocation would appear if planning a
	// filter had put one on the iteration path.
	filtered1k := measure(1_000, subscribeActive, populateFiltered)
	filtered10k := measure(10_000, subscribeActive, populateFiltered)
	t.Logf("objects a frame: hand-written %.3f, Query at 1k %.3f, Query at 10k %.3f, filtered Query at 1k %.3f, at 10k %.3f",
		hand, query1k, query10k, filtered1k, filtered10k)

	if query1k > hand+0.05 {
		t.Fatalf("the Query costs %.3f objects a frame against the hand-written %.3f", query1k, hand)
	}
	if query10k > query1k+0.05 {
		t.Fatalf("allocation scales with entity count: %.3f a frame at 1k, %.3f at 10k", query1k, query10k)
	}
	if query10k > 6.5 {
		t.Fatalf("the frame costs %.3f objects, above the engine's 6-per-frame line", query10k)
	}
	if filtered1k > hand+0.05 {
		t.Fatalf("a filtered Query costs %.3f objects a frame against the hand-written %.3f", filtered1k, hand)
	}
	if filtered10k > filtered1k+0.05 {
		t.Fatalf("a filter allocates per Entity: %.3f a frame at 1k, %.3f at 10k", filtered1k, filtered10k)
	}
}
