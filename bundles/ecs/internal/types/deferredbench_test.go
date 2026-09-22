package types

import (
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// When to reach for a deferring handle, measured rather than estimated. The
// question has one shape: a System that changes the world holds the barrier for
// its whole run, so every other System in the frame that would have run beside
// it is excluded; a deferring System holds only the read, runs beside them, and
// pays instead — per change — one atomic add on the reservation cursor, one
// append to a typed buffer, and a second traversal of that buffer at the drain.
// Below some number of changes a tick the barrier it saves is worth more than
// the per-change work it adds, and above it the trade reverses. That number is
// the crossover, and it is what the arms below are for.
//
// Every arm carries the general drainer, subscribed the way the ecs plugin
// subscribes ecs.DrainOnUpdate: Last, on the Update, unconditionally. That is
// not a handicap on the deferring arm, it is what an app is: the drain node is
// in every composition whether anything defers or not, so leaving it out of the
// immediate arm would charge the deferring one for a barrier both pay.

// churnSet is the Component set the changing System creates with: a data
// Component and a Tag, neither of them touched by any other System in the
// composition. That is deliberate — the two arms differ in what they declare on
// *Entities, and nothing else in their lock sets may differ in a way the
// scheduler can see. It also keeps the spawned Entities out of every Query in
// the frame, so the walk is the same length at 1 changes a tick and at 500 and
// only the change work varies.
type churnSet struct {
	Homing   homing
	Disabled disabled
}

var churnValues = churnSet{Homing: homing{}}

// crossoverEntities is barrierEntities: the same population the BenchmarkBarrier
// arms are measured over, so the barrier figure read out of them is the barrier
// figure these arms are trading against.
const crossoverEntities = barrierEntities

type (
	crossoverSystem      kernel.Subscription[app.UpdateEvent]
	crossoverDrainSystem kernel.Subscription[app.UpdateEvent]
)

// crossoverChurn is one arm's per-tick work: retire what the last tick created, and
// create as many again, so the population is the one it started in and the free
// list and the dense rows are in the steady state a game runs in.
//
// immediate and deferring are written out as two funcs rather than one branching
// on a field, and every arm below is its own func for the same reason: a System
// func that closes over a flag costs three objects a frame through
// reflect.Value.Call, which is a measurement artefact large enough to swamp what
// is measured here.
type crossoverChurn struct {
	changes int
	live    []Entity
}

func (c *crossoverChurn) immediate(q *Query[velocityQuery], sp *Spawn[churnSet], we *WriteableEntities) {
	walk(q)
	for _, e := range c.live {
		we.Despawn(e)
	}
	c.live = c.live[:0]
	for range c.changes {
		c.live = append(c.live, sp.New(churnValues))
	}
}

func (c *crossoverChurn) deferring(q *Query[velocityQuery], sp *DeferredSpawn[churnSet], dd *DeferredDespawn) {
	walk(q)
	for _, e := range c.live {
		dd.Despawn(e)
	}
	c.live = c.live[:0]
	for range c.changes {
		c.live = append(c.live, sp.New(churnValues))
	}
}

// queueing is the contention arm's System: the same churn with no Query, so
// several of them declare reads alone and the scheduler runs them together. It
// is the only place in the design where parallel Systems meet on one word.
func (c *crossoverChurn) queueing(sp *DeferredSpawn[churnSet], dd *DeferredDespawn) {
	for _, e := range c.live {
		dd.Despawn(e)
	}
	c.live = c.live[:0]
	for range c.changes {
		c.live = append(c.live, sp.New(churnValues))
	}
}

// subscribeDrainer subscribes the general drainer as the ecs plugin subscribes
// ecs.DrainOnUpdate — Last, on the Update — spelled here because this package
// cannot import ecs's root.
func subscribeDrainer(registrar *kernel.Registrar) {
	registrar.Subscribe[crossoverDrainSystem](
		ToHandler[app.UpdateEvent](registrar, drainSystem)).Last()
}

// benchmarkCrossover is the whole frame: the two workers of the barrier
// benchmarks, the System under test walking its own Component in both arms, and
// the drainer. ids is sized past the high-water mark both arms reach, so nothing
// here is measuring growth.
func benchmarkCrossover(b *testing.B, changes int, system any) {
	subscribe := func(registrar *kernel.Registrar) {
		subscribeWorkers(registrar)
		registrar.Subscribe[crossoverSystem](ToHandler[app.UpdateEvent](registrar, system))
		subscribeDrainer(registrar)
	}
	entities, components, engine := newWorld(b, uint32(crossoverEntities+2*changes+64), subscribe)
	populate(entities, components, crossoverEntities)
	executioner := engine.Executioner()
	// Warm every pool the first frames fill, the deferral buffers included, so
	// what is measured is a steady state and not the first tick.
	for range 100 {
		frame(b, engine, 1)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait()
	}
}

func benchmarkCrossoverImmediate(b *testing.B, changes int) {
	state := &crossoverChurn{changes: changes, live: make([]Entity, 0, changes)}
	benchmarkCrossover(b, changes, state.immediate)
}

func benchmarkCrossoverDeferring(b *testing.B, changes int) {
	state := &crossoverChurn{changes: changes, live: make([]Entity, 0, changes)}
	benchmarkCrossover(b, changes, state.deferring)
}

func BenchmarkCrossoverImmediate1(b *testing.B)   { benchmarkCrossoverImmediate(b, 1) }
func BenchmarkCrossoverDeferring1(b *testing.B)   { benchmarkCrossoverDeferring(b, 1) }
func BenchmarkCrossoverImmediate10(b *testing.B)  { benchmarkCrossoverImmediate(b, 10) }
func BenchmarkCrossoverDeferring10(b *testing.B)  { benchmarkCrossoverDeferring(b, 10) }
func BenchmarkCrossoverImmediate100(b *testing.B) { benchmarkCrossoverImmediate(b, 100) }
func BenchmarkCrossoverDeferring100(b *testing.B) { benchmarkCrossoverDeferring(b, 100) }
func BenchmarkCrossoverImmediate500(b *testing.B) { benchmarkCrossoverImmediate(b, 500) }
func BenchmarkCrossoverDeferring500(b *testing.B) { benchmarkCrossoverDeferring(b, 500) }

// 1, 10, 100 and 500 are the sweep the question was asked over, 500 being the
// storm case. 2 000 through 5 000 are past it, and they are here because the
// crossover turned out not to be inside the sweep: two arms that never cross
// report no crossover, so the bracket is carried up until they do.
func BenchmarkCrossoverImmediate2000(b *testing.B) { benchmarkCrossoverImmediate(b, 2_000) }
func BenchmarkCrossoverDeferring2000(b *testing.B) { benchmarkCrossoverDeferring(b, 2_000) }
func BenchmarkCrossoverImmediate3000(b *testing.B) { benchmarkCrossoverImmediate(b, 3_000) }
func BenchmarkCrossoverDeferring3000(b *testing.B) { benchmarkCrossoverDeferring(b, 3_000) }
func BenchmarkCrossoverImmediate4000(b *testing.B) { benchmarkCrossoverImmediate(b, 4_000) }
func BenchmarkCrossoverDeferring4000(b *testing.B) { benchmarkCrossoverDeferring(b, 4_000) }
func BenchmarkCrossoverImmediate5000(b *testing.B) { benchmarkCrossoverImmediate(b, 5_000) }
func BenchmarkCrossoverDeferring5000(b *testing.B) { benchmarkCrossoverDeferring(b, 5_000) }

// The contention arm. Several deferring Systems, each queuing its own churn into
// its own buffer, declaring reads alone and so scheduled together: the one place
// the design has parallel Systems meeting, and they meet on the reservation
// cursor and nowhere else.

type (
	contenderASystem kernel.Subscription[app.UpdateEvent]
	contenderBSystem kernel.Subscription[app.UpdateEvent]
	contenderCSystem kernel.Subscription[app.UpdateEvent]
	contenderDSystem kernel.Subscription[app.UpdateEvent]
)

// benchmarkContended runs `systems` deferring Systems in one frame, each making
// `each` changes a tick. Read across 1, 2 and 4 at the same `each`, a frame that
// grew with the number of Systems would be the cursor serialising them.
func benchmarkContended(b *testing.B, systems, each int) {
	states := make([]*crossoverChurn, systems)
	for i := range states {
		states[i] = &crossoverChurn{changes: each, live: make([]Entity, 0, each)}
	}
	subscribe := func(registrar *kernel.Registrar) {
		registrar.Subscribe[contenderASystem](ToHandler[app.UpdateEvent](registrar, states[0].queueing))
		if systems > 1 {
			registrar.Subscribe[contenderBSystem](ToHandler[app.UpdateEvent](registrar, states[1].queueing))
		}
		if systems > 2 {
			registrar.Subscribe[contenderCSystem](ToHandler[app.UpdateEvent](registrar, states[2].queueing))
			registrar.Subscribe[contenderDSystem](ToHandler[app.UpdateEvent](registrar, states[3].queueing))
		}
		subscribeDrainer(registrar)
	}
	entities, components, engine := newWorld(b, uint32(crossoverEntities+2*systems*each+64), subscribe)
	populate(entities, components, crossoverEntities)
	executioner := engine.Executioner()
	for range 100 {
		frame(b, engine, 1)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait()
	}
}

func BenchmarkContendedOneSystem100(b *testing.B)   { benchmarkContended(b, 1, 100) }
func BenchmarkContendedOneSystem400(b *testing.B)   { benchmarkContended(b, 1, 400) }
func BenchmarkContendedTwoSystems100(b *testing.B)  { benchmarkContended(b, 2, 100) }
func BenchmarkContendedFourSystems100(b *testing.B) { benchmarkContended(b, 4, 100) }

// The idle arms are the control the contended ones are read against, and they
// are what makes the reading mean anything: the same Systems, the same
// signatures, the same lock sets and the same number of nodes for the engine to
// dispatch, making no changes at all. The engine charges a frame for every node
// whatever it does, so the difference between an arm and its idle twin is the
// change work and the cursor, and nothing else. Divided by the number of
// Systems, a figure flat across 1, 2 and 4 is a cursor that costs nothing at
// frame scale.
func BenchmarkContendedOneSystemIdle(b *testing.B)   { benchmarkContended(b, 1, 0) }
func BenchmarkContendedTwoSystemsIdle(b *testing.B)  { benchmarkContended(b, 2, 0) }
func BenchmarkContendedFourSystemsIdle(b *testing.B) { benchmarkContended(b, 4, 0) }

// BenchmarkReserve and BenchmarkReserveParallel price the word itself, off the
// frame: the reservation cursor is one atomic add and nothing else, and the pair
// is what the contended frame above is read against. Parallel runs GOMAXPROCS
// goroutines on the same cursor, which is more pressure than a frame can put on
// it — a frame has as many contenders as it has deferring Systems.
func BenchmarkReserve(b *testing.B) {
	en := newEntities(64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = en.reserve()
	}
}

func BenchmarkReserveParallel(b *testing.B) {
	en := newEntities(64)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = en.reserve()
		}
	})
}

// The drain, off the frame. What a steady-state drain costs and what it
// allocates are two questions with one answer here: the buffers are cut to zero
// length and keep their capacity, so after the first few ticks nothing is
// allocated at all, and -benchmem showing 0 B/op is the showing rather than a
// test asserting it.

type deferredHandleSystem kernel.Subscription[app.UpdateEvent]

// deferredHandles runs one tick of a System that does nothing but keep its own
// parameters, so a benchmark can queue and drain directly and count what they
// cost. Only a test in this package can hold them outside a frame; a System's
// only route to any of them is its signature.
func deferredHandles[S any](tb testing.TB, ids uint32) (*DeferredSpawn[S], *DeferredDespawn, *WriteableEntities) {
	tb.Helper()
	var spawn *DeferredSpawn[S]
	var despawn *DeferredDespawn
	var writeable *WriteableEntities
	_, _, engine := newWorld(tb, ids, func(registrar *kernel.Registrar) {
		registrar.Subscribe[deferredHandleSystem](ToHandler[app.UpdateEvent](registrar,
			func(sp *DeferredSpawn[S], dd *DeferredDespawn, we *WriteableEntities) {
				spawn, despawn, writeable = sp, dd, we
			}))
	})
	frame(tb, engine, 1)
	return spawn, despawn, writeable
}

// benchmarkDrainCycle is one queue-and-drain cycle: `changes` Spawns queued,
// `changes` Despawns of the last cycle's Entities queued, and one drain. The
// population is what it was at the end of every cycle, so the free list recycles
// and the drain is the steady-state one.
func benchmarkDrainCycle(b *testing.B, changes int) {
	spawn, despawn, writeable := deferredHandles[churnSet](b, uint32(4*changes+64))
	live := make([]Entity, 0, changes)
	cycle := func() {
		for _, e := range live {
			despawn.Despawn(e)
		}
		live = live[:0]
		for range changes {
			live = append(live, spawn.New(churnValues))
		}
		writeable.Drain()
	}
	// Reach the capacity both buffers settle at before the timer starts, so what
	// is measured is a steady state and not the growth of a buffer.
	for range 4 {
		cycle()
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cycle()
	}
}

func BenchmarkDrainCycle1(b *testing.B)   { benchmarkDrainCycle(b, 1) }
func BenchmarkDrainCycle10(b *testing.B)  { benchmarkDrainCycle(b, 10) }
func BenchmarkDrainCycle100(b *testing.B) { benchmarkDrainCycle(b, 100) }
func BenchmarkDrainCycle500(b *testing.B) { benchmarkDrainCycle(b, 500) }

// BenchmarkDrainIdle is the absent empty-drain skip priced: two enrolled buffers
// with nothing in them, walked every drain. It is what a drain costs an app that
// queued nothing this tick, and it is a length check per enrolled buffer.
func BenchmarkDrainIdle(b *testing.B) {
	_, _, writeable := deferredHandles[churnSet](b, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		writeable.Drain()
	}
}

// benchmarkImmediateCycle is benchmarkDrainCycle's control: the same changes
// made at the call, off the frame, so the per-change difference between an
// immediate change and a deferred one is legible without the barrier in it. The
// barrier is what the whole-frame arms above measure; this is the other half of
// the crossover arithmetic.
func benchmarkImmediateCycle(b *testing.B, changes int) {
	spawn, writeable, _ := handles[churnSet](b, uint32(4*changes+64))
	live := make([]Entity, 0, changes)
	cycle := func() {
		for _, e := range live {
			writeable.Despawn(e)
		}
		live = live[:0]
		for range changes {
			live = append(live, spawn.New(churnValues))
		}
	}
	for range 4 {
		cycle()
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cycle()
	}
}

func BenchmarkImmediateCycle1(b *testing.B)   { benchmarkImmediateCycle(b, 1) }
func BenchmarkImmediateCycle10(b *testing.B)  { benchmarkImmediateCycle(b, 10) }
func BenchmarkImmediateCycle100(b *testing.B) { benchmarkImmediateCycle(b, 100) }
func BenchmarkImmediateCycle500(b *testing.B) { benchmarkImmediateCycle(b, 500) }
