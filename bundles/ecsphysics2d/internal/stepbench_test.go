package internal

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// TestTheStepSitsOnTheEnginesAllocationLine measures a steady state rather than
// an average over b.N, because an average can hide amortised growth: ten
// thousand ticks counted through a MemStats delta, at three workload sizes an
// order of magnitude apart.
//
// Zero heap allocation on the hot path is one of the four requirements the
// design is bound by, and it is measured rather than asserted. What is measured
// is the *slope*: an engine with no Bodies at all still pays the kernel's own
// per-tick dispatch, once per subscription, and that is the engine's line
// rather than physics'. An allocation inside the step would scale with the Body
// count, so the claim is that the empty engine, N=256 and N=1024 all cost the
// same. cp allocates 164 objects and 12.5 KB a step at N=256 and 656 and 49.8
// KB at N=1024; the port allocates none of its own at either size.
func TestTheStepSitsOnTheEnginesAllocationLine(t *testing.T) {
	const ticks = 10_000

	measure := func(n int) float64 {
		h := newHarnessWith(t, nil, uint32(2*max(n, 1)))
		populate(t, h, n)
		h.game.push = m.Vec2d{X: 10}
		// Warm every pool the first ticks fill, so what is measured is the
		// steady state and not the first tick.
		h.frames(t, 100)
		mallocs := allocationsDuring(func() {
			for range ticks {
				if err := h.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait(); err != nil {
					t.Fatalf("publishing the update: %v", err)
				}
			}
		})
		return float64(mallocs) / ticks
	}

	empty, small, large := measure(0), measure(256), measure(1024)
	t.Logf("objects a step: %.3f with no Bodies, %.3f at N=256, %.3f at N=1024 — "+
		"the first is the kernel's own dispatch over the nine subscriptions this engine composes",
		empty, small, large)

	if small > empty+0.05 {
		t.Errorf("256 Bodies cost %.3f objects a step against %.3f with none", small, empty)
	}
	if large > small+0.05 {
		t.Errorf("allocation scales with the Body count: %.3f a step at N=256, %.3f at N=1024", small, large)
	}
	if large-empty > 0.05 {
		t.Errorf("the step's own cost is %.3f objects a tick, want none", large-empty)
	}
}

// BenchmarkTheStep is the cost of one whole tick over a scene of moving Bodies,
// half of them Dynamic under a Force and half Kinematic. Wall-clock is asserted
// nowhere: a whole-frame benchmark here swings about ±10% by run order, so cp's
// numbers are compared against interleaved A/B and recorded, never asserted.
func BenchmarkTheStep(b *testing.B) {
	for _, n := range []int{256, 1024} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			h := newHarnessWith(b, nil, uint32(2*n))
			populate(b, h, n)
			h.game.push = m.Vec2d{X: 10}
			h.frames(b, 100)

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := h.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait(); err != nil {
					b.Fatalf("publishing the update: %v", err)
				}
			}
		})
	}
}

// populate spawns n Bodies, half Dynamic under a Force and half Kinematic, plus
// a quarter as many Static ones for the walk to step over. A count of zero
// spawns nothing, which is the engine's own line to measure the rest against.
func populate(t testing.TB, h *harness, n int) {
	t.Helper()
	if n == 0 {
		return
	}
	h.spawn(t, spawnRequest{Kind: kindDynamic, Count: n / 2, Body: dynamic(t, 2, 8, 15, 0.3)})
	h.spawn(t, spawnRequest{
		Kind:     kindKinematic,
		Count:    n / 2,
		Velocity: ecsphysics2d.Velocity{Linear: m.Vec2d{X: 1}},
	})
	h.spawn(t, spawnRequest{Kind: kindStatic, Count: n / 4})
}

// allocationsDuring counts the objects f allocates, the way ecs's own frame
// measurement does.
func allocationsDuring(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.Mallocs - before.Mallocs
}
