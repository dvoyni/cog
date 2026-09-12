package ecs

import (
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// What structural change costs, and where the cost actually is. Two questions
// are measured here and they have different answers: a spawn is a handful of
// nanoseconds and no allocation, while the barrier it declares is a whole
// scheduling round — paid once per frame, by the declaration, whether the System
// spawns or not.

// wideBundle is the four-field Bundle, the last of them a Tag: a Component with
// no fields is an ordinary Store and an ordinary Bundle field.
type wideBundle struct {
	Body     body
	Velocity velocity
	Collider collider
	Solid    solid
}

// spawnBatch is how many Entities a benchmark holds live at once. The loops
// below refill or drain with the timer stopped, which keeps the measurement on
// one operation and keeps the index space bounded however long b.N runs.
const spawnBatch = 4096

func BenchmarkSpawnTwoFields(b *testing.B) {
	spawn, writeable, _ := handles[spawnBundle](b, spawnBatch)
	bundle := spawnBundle{Body: body{X: 1}, Velocity: velocity{X: 2}}
	benchmarkSpawn(b, writeable, func() Entity { return spawn.New(bundle) })
}

func BenchmarkSpawnFourFields(b *testing.B) {
	spawn, writeable, _ := handles[wideBundle](b, spawnBatch)
	bundle := wideBundle{Body: body{X: 1}, Velocity: velocity{X: 2}, Collider: collider{Radius: 3}}
	benchmarkSpawn(b, writeable, func() Entity { return spawn.New(bundle) })
}

// BenchmarkSpawnTwoFieldsEscaping is the trap priced: the same spawn with the
// bundle reached through the address of the parameter instead of through the
// Spawn's own field.
func BenchmarkSpawnTwoFieldsEscaping(b *testing.B) {
	spawn, writeable, _ := handles[spawnBundle](b, spawnBatch)
	bundle := spawnBundle{Body: body{X: 1}, Velocity: velocity{X: 2}}
	benchmarkSpawn(b, writeable, func() Entity { return spawn.newEscaping(bundle) })
}

// BenchmarkSpawnTwoFieldsHandWritten is the baseline the others are read
// against: the same two Stores written directly, which only a test in this
// package can do.
func BenchmarkSpawnTwoFieldsHandWritten(b *testing.B) {
	_, writeable, components := handles[spawnBundle](b, spawnBatch)
	entities := writeable.entities.Get()
	benchmarkSpawn(b, writeable, func() Entity {
		e := entities.alloc()
		components.bodies.Set(e, body{X: 1})
		components.velocities.Set(e, velocity{X: 2})
		return e
	})
}

// benchmarkSpawn measures the spawn alone, draining the batch with the timer
// stopped. The steady state is the one a game runs in: the free list has ids and
// the Stores have rows, so nothing here is measuring growth.
func benchmarkSpawn(b *testing.B, writeable *WriteableEntities, create func() Entity) {
	live := make([]Entity, 0, spawnBatch)
	drain := func() {
		for _, e := range live {
			writeable.Despawn(e)
		}
		live = live[:0]
	}
	for range spawnBatch {
		live = append(live, create())
	}
	drain()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(live) == spawnBatch {
			b.StopTimer()
			drain()
			b.StartTimer()
		}
		live = append(live, create())
	}
	b.StopTimer()
	drain()
}

// BenchmarkDespawnOnly prices the traversal on its own: a despawn asks every
// enrolled Store, naming no Component, through an interface carrying exactly one
// method.
func BenchmarkDespawnOnly(b *testing.B) {
	spawn, writeable, _ := handles[spawnBundle](b, spawnBatch)
	bundle := spawnBundle{Body: body{X: 1}, Velocity: velocity{X: 2}}
	live := make([]Entity, 0, spawnBatch)
	refill := func() {
		for len(live) < spawnBatch {
			live = append(live, spawn.New(bundle))
		}
	}
	refill()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(live) == 0 {
			b.StopTimer()
			refill()
			b.StartTimer()
		}
		writeable.Despawn(live[len(live)-1])
		live = live[:len(live)-1]
	}
}

// The barrier, on a real frame. Three Systems over 2 000 Entities: two workers
// writing different Components, and a third that iterates the same Entities in
// every arm and differs only in what it declares.

type workerCSystem kernel.Subscription[app.UpdateEvent]

func subscribeWorkers(registrar *kernel.Registrar, world *Entities) {
	registrar.Subscribe[workerASystem](ToHandler[app.UpdateEvent](world,
		func(q *Query[bodyQuery]) {
			for _, it := range q.All() {
				it.Body.X++
			}
		}))
	registrar.Subscribe[workerBSystem](ToHandler[app.UpdateEvent](world,
		func(q *Query[colliderQuery]) {
			for _, it := range q.All() {
				it.Collider.Radius++
			}
		}))
}

// subscribeThird adds the System under test to the two workers. Every arm walks
// the same Query over the same Entities; what changes is the declaration.
func subscribeThird(system any) func(*kernel.Registrar, *Entities) {
	return func(registrar *kernel.Registrar, world *Entities) {
		subscribeWorkers(registrar, world)
		if system != nil {
			registrar.Subscribe[workerCSystem](ToHandler[app.UpdateEvent](world, system))
		}
	}
}

// velocityQuery is the third System's own Component, disjoint from both
// workers'. That is what leaves *Entities as the only thing the arms differ by:
// with a Component the workers write, the third would already be serialised
// against one of them and the barrier would look cheaper than it is.
type velocityQuery struct{ Velocity *velocity }

func walk(q *Query[velocityQuery]) {
	for _, it := range q.All() {
		it.Velocity.X++
	}
}

const barrierEntities = 2_000

// BenchmarkBarrierAbsent is the floor: the two workers alone.
func BenchmarkBarrierAbsent(b *testing.B) {
	benchmarkFrame(b, barrierEntities, subscribeThird(nil))
}

// BenchmarkBarrierReading is the third System declaring read{*Entities}, which
// is what an ordinary Query takes.
func BenchmarkBarrierReading(b *testing.B) {
	benchmarkFrame(b, barrierEntities, subscribeThird(walk))
}

// BenchmarkBarrierDeclared is the same work with a Spawn parameter the System
// never uses. The difference from BenchmarkBarrierReading is the barrier, and
// nothing else.
func BenchmarkBarrierDeclared(b *testing.B) {
	benchmarkFrame(b, barrierEntities, subscribeThird(
		func(q *Query[velocityQuery], sp *Spawn[spawnBundle]) { walk(q) }))
}

// BenchmarkBarrierSpawning is the same again, actually spawning and despawning
// one Entity a tick. Against BenchmarkBarrierDeclared it prices the structural
// change itself, with the declaration already paid for.
func BenchmarkBarrierSpawning(b *testing.B) {
	benchmarkFrame(b, barrierEntities, subscribeThird(
		func(q *Query[velocityQuery], sp *Spawn[spawnBundle], we *WriteableEntities) {
			walk(q)
			we.Despawn(sp.New(spawnBundle{Body: body{X: 1}, Velocity: velocity{X: 1}}))
		}))
}

// TestStructuralChangeStaysOnTheEnginesAllocationLine measures a steady state
// rather than an average over b.N, because an average can hide amortised
// growth. The claim is that a frame with a structural change in it costs what
// the engine charges for its subscribers and nothing more, and that ten thousand
// spawns and ten thousand despawns in one tick add nothing to that — the free
// list recycles the ids and the Store reuses the dense row, so growth stops at
// the high-water mark.
func TestStructuralChangeStaysOnTheEnginesAllocationLine(t *testing.T) {
	// Both arms run the same number of frames, so the two numbers carry the
	// same noise floor: a stray object somewhere in the engine is worth
	// 1/frames either way, and only a per-spawn allocation could separate them.
	const frames = 1_000
	measure := func(perTick int, ids uint32, frames int) float64 {
		live := make([]Entity, 0, perTick)
		_, _, engine := newWorld(t, ids, func(registrar *kernel.Registrar, world *Entities) {
			registrar.Subscribe[churnSystem](ToHandler[app.UpdateEvent](world,
				func(sp *Spawn[spawnBundle], we *WriteableEntities) {
					for _, e := range live {
						we.Despawn(e)
					}
					live = live[:0]
					for range perTick {
						live = append(live, sp.New(spawnBundle{Body: body{X: 1}, Velocity: velocity{X: 2}}))
					}
				}))
		})
		executioner := engine.Executioner()
		// Warm every pool the first frames fill, and reach the high-water mark
		// the free list and the dense arrays settle at.
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
		return float64(mallocs) / float64(frames)
	}

	one := measure(1, 64, frames)
	tenThousand := measure(10_000, 16_384, frames)
	t.Logf("objects a frame with one subscriber: 1 spawn and despawn a tick %.3f, 10 000 a tick %.3f",
		one, tenThousand)

	if one > 6.5 {
		t.Fatalf("a frame with a structural change costs %.3f objects, above the engine's 6-per-frame line", one)
	}
	// A tenth of an object a frame against ten thousand spawns and ten thousand
	// despawns: anything charged per structural change would be four orders of
	// magnitude above this bar, so what is left is the engine's own jitter.
	if tenThousand > one+0.1 {
		t.Fatalf("allocation scales with the number of structural changes: %.3f a frame at 1, %.3f at 10 000",
			one, tenThousand)
	}
	if tenThousand > 6.5 {
		t.Fatalf("a frame with 10 000 structural changes costs %.3f objects, above the engine's 6-per-frame line",
			tenThousand)
	}
}
