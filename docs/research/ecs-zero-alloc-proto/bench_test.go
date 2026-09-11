package proto

import (
	"context"
	"runtime"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// tick is the real fixed-timestep event, at nox's 30Hz.
var tick = app.UpdateEvent{Dt: 1.0 / 30.0, Last: true}

// startEngine runs a real engine to readiness, exactly as the validated cog#241
// harness does.
func startEngine(tb testing.TB, p protoPlugin) *kernel.Engine {
	tb.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(cancel)
	e := kernel.New(nil).
		Handler(func(err error) bool { tb.Fatalf("kernel error: %v", err); return true }).
		WithPlugins(p)
	go e.Run(ctx)
	<-e.Ready()
	return e
}

// benchTick measures one whole frame: publish the tick, acquire every declared
// lock, run every System, and wait for the publication to finish.
func benchTick(b *testing.B, entities int, m mode) {
	b.Helper()
	e := startEngine(b, protoPlugin{entities: entities, mode: m})
	ex := e.Executioner()
	ex.PublishEvent(tick).Wait() // warm the publication plan and the Stores
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ex.PublishEvent(tick).Wait()
	}
}

func benchTickAt(b *testing.B, entities int, m mode, procs int) {
	b.Helper()
	old := runtime.GOMAXPROCS(procs)
	b.Cleanup(func() { runtime.GOMAXPROCS(old) })
	benchTick(b, entities, m)
}

func TestReportEnv(t *testing.T) {
	t.Logf("NumCPU=%d GOMAXPROCS=%d", runtime.NumCPU(), runtime.GOMAXPROCS(0))
}

// ---------------------------------------------------------------------------
// The headline: one System over two Components, through the reflective builder,
// on a real engine. 1k and 10k, as the ticket asks.
// ---------------------------------------------------------------------------

func BenchmarkTick_Reflected_1k(b *testing.B)  { benchTick(b, 1000, modeReflected) }
func BenchmarkTick_Reflected_10k(b *testing.B) { benchTick(b, 10000, modeReflected) }

// The same System with the call baked by a generic builder instead of
// reflected. The only difference between the two is the call.
func BenchmarkTick_Baked_1k(b *testing.B)  { benchTick(b, 1000, modeBaked) }
func BenchmarkTick_Baked_10k(b *testing.B) { benchTick(b, 10000, modeBaked) }

// An empty world through each builder. The whole-frame gap between reflected
// and baked held constant from 1k to 10k, so it is fixed per tick rather than
// per entity; these two say how much of a frame it is before any entity exists.
func BenchmarkTick_Reflected_0(b *testing.B) { benchTick(b, 0, modeReflected) }
func BenchmarkTick_Baked_0(b *testing.B)     { benchTick(b, 0, modeBaked) }

// The same pair pinned to one processor. reflect.Value.Call takes its argument
// frame from a per-P pool, and a publication runs on a goroutine started for
// that publication, so the two need not land on the same P. If the gap is
// P-locality rather than the call, pinning collapses it.
func BenchmarkTick_Reflected_0_P1(b *testing.B) { benchTickAt(b, 0, modeReflected, 1) }
func BenchmarkTick_Baked_0_P1(b *testing.B)     { benchTickAt(b, 0, modeBaked, 1) }

// A System that does nothing, through each builder: the call and nothing else.
func BenchmarkTick_NoopReflected(b *testing.B) { benchTick(b, 1000, modeNoopReflected) }
func BenchmarkTick_NoopBaked(b *testing.B)     { benchTick(b, 1000, modeNoopBaked) }

// Everything ToHandler does except the reflected call, which attributes the gap.
func BenchmarkTick_Hybrid_0(b *testing.B)   { benchTick(b, 0, modeHybrid) }
func BenchmarkTick_Hybrid_1k(b *testing.B)  { benchTick(b, 1000, modeHybrid) }
func BenchmarkTick_Hybrid_10k(b *testing.B) { benchTick(b, 10000, modeHybrid) }

// The baselines that say whose allocations these are: an engine with nothing
// subscribed, and the same work written by hand with no ECS in the way.
func BenchmarkTick_None(b *testing.B)     { benchTick(b, 1000, modeNone) }
func BenchmarkTick_Bare_1k(b *testing.B)  { benchTick(b, 1000, modeBare) }
func BenchmarkTick_Bare_10k(b *testing.B) { benchTick(b, 10000, modeBare) }

// Three Components, which leaves the unrolled two-Component path.
func BenchmarkTick_Three_1k(b *testing.B)  { benchTick(b, 1000, modeThree) }
func BenchmarkTick_Three_10k(b *testing.B) { benchTick(b, 10000, modeThree) }

// A Without filter, which cog#239 established contributes a real read.
func BenchmarkTick_Filtered_1k(b *testing.B)  { benchTick(b, 1000, modeFiltered) }
func BenchmarkTick_Filtered_10k(b *testing.B) { benchTick(b, 10000, modeFiltered) }

// ---------------------------------------------------------------------------
// With a structural change every frame, because nox's real workload never holds
// still: one projectile spawned and one despawned per tick.
// ---------------------------------------------------------------------------

func BenchmarkTick_Structural_1k(b *testing.B)  { benchTick(b, 1000, modeStructural) }
func BenchmarkTick_Structural_10k(b *testing.B) { benchTick(b, 10000, modeStructural) }

// ---------------------------------------------------------------------------
// Two Systems with disjoint write sets, at one processor and at N, to confirm
// the scheduler runs them concurrently and that concurrency itself allocates
// nothing.
// ---------------------------------------------------------------------------

func BenchmarkTick_TwoDisjoint_1k_P1(b *testing.B) { benchTickAt(b, 1000, modeTwoDisjoint, 1) }
func BenchmarkTick_TwoDisjoint_1k_PN(b *testing.B) {
	benchTickAt(b, 1000, modeTwoDisjoint, runtime.NumCPU())
}
func BenchmarkTick_TwoDisjoint_10k_P1(b *testing.B) { benchTickAt(b, 10000, modeTwoDisjoint, 1) }
func BenchmarkTick_TwoDisjoint_10k_PN(b *testing.B) {
	benchTickAt(b, 10000, modeTwoDisjoint, runtime.NumCPU())
}

// ---------------------------------------------------------------------------
// Steady state. b.N already averages over many frames, but an average can hide
// amortised growth; this reports the absolute bytes and objects a long run
// leaves behind.
// ---------------------------------------------------------------------------

func steadyState(t *testing.T, entities, frames int, m mode) {
	t.Helper()
	e := startEngine(t, protoPlugin{entities: entities, mode: m})
	ex := e.Executioner()
	for range 100 {
		ex.PublishEvent(tick).Wait() // warm up
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range frames {
		ex.PublishEvent(tick).Wait()
	}
	runtime.ReadMemStats(&after)
	objects := after.Mallocs - before.Mallocs
	bytes := after.TotalAlloc - before.TotalAlloc
	t.Logf("%d entities, %d frames: %d objects, %d bytes, %.3f objects/frame, %.1f bytes/frame",
		entities, frames, objects, bytes,
		float64(objects)/float64(frames), float64(bytes)/float64(frames))
}

func TestSteadyState_Reflected_1k(t *testing.T)  { steadyState(t, 1000, 10000, modeReflected) }
func TestSteadyState_Reflected_10k(t *testing.T) { steadyState(t, 10000, 10000, modeReflected) }
func TestSteadyState_Structural_1k(t *testing.T) { steadyState(t, 1000, 10000, modeStructural) }
func TestSteadyState_TwoDisjoint(t *testing.T)   { steadyState(t, 1000, 10000, modeTwoDisjoint) }

// ---------------------------------------------------------------------------
// Correctness. A zero-allocation loop that computes the wrong answer proves
// nothing, so the shape is checked before its cost is believed.
// ---------------------------------------------------------------------------

func TestWritesLandThroughAPointerField(t *testing.T) {
	e := startEngine(t, protoPlugin{entities: 100, mode: modeReflected})
	bodies := last.bodies
	// The dense order is unspecified, so the handle comes from the Store rather
	// than from an assumed index.
	e0 := bodies.OwnerAt(0)
	e.Executioner().PublishEvent(tick).Wait()
	got, ok := bodies.Get(e0)
	if !ok {
		t.Fatal("the first seeded entity has no Body")
	}
	// One tick of Vx=1 at dt=1/30 moves Px by 1/30 from its seeded value of 0.
	if want := tick.Dt; got.Px != want {
		t.Fatalf("write did not land: Px = %v, want %v", got.Px, want)
	}
}

func TestFilterExcludesItsComponent(t *testing.T) {
	const n = 1000
	e := startEngine(t, protoPlugin{entities: n, mode: modeFiltered})
	e.Executioner().PublishEvent(tick).Wait()
	// Every tenth entity was given Frozen, so the filter must drop exactly those.
	if want := n - n/10; sinkI != want {
		t.Fatalf("filter visited %d entities, want %d", sinkI, want)
	}
}

func TestStructuralChangeKeepsTheWorldSteady(t *testing.T) {
	const n = 1000
	e := startEngine(t, protoPlugin{entities: n, mode: modeStructural})
	ex := e.Executioner()
	en := last.entities
	for range 500 {
		ex.PublishEvent(tick).Wait()
	}
	// One spawn and one despawn a tick: the population settles at n+1 and stays.
	if got := en.Live(); got != n+1 {
		t.Fatalf("live entities drifted to %d over 500 ticks, want %d", got, n+1)
	}
}
