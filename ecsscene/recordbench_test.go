package ecsscene

import (
	"reflect"
	"runtime"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/app"
)

// The numbers here are the ticket's bar, and they are measured on a real engine
// driven by a real app.UpdateEvent with the real scene plugin composed beside
// the binding — publish, acquire every declared lock, run every System and
// every flush, wait.
//
// The frame measured has no camera and no resident model, which is deliberate:
// what the binding costs is the walk, the manifest probe and the append into
// scene's queue, and what scene's own flush costs once it has a camera to
// decide for is scene's number rather than the binding's. The recording work is
// identical either way — scene records the call before it knows whether the
// path is resident, and a non-resident model is skipped at expansion.

// allocationsDuring counts the objects allocated while f runs, across every
// goroutine, which is what a per-entity allocation would show up in.
func allocationsDuring(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.Mallocs - before.Mallocs
}

// benchmarkFrame is the classic b.N form deliberately: testing.B.Loop keeps its
// loop-assigned values alive, which is exactly what an allocation claim must not
// have helping it.
func benchmarkFrame(b *testing.B, n int, animated bool) {
	h := newRecordingHarness(b, n, animated)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.frame(b)
	}
}

// newRecordingHarness is the binding under measurement: n drawables, the crate
// registered in the manifest, no backend and no camera.
func newRecordingHarness(tb testing.TB, n int, animated bool) *harness {
	tb.Helper()
	ids := uint32(n) + 8
	h := newHarnessOver(tb, fstest.MapFS{}, testConfig(), ids)
	if n > 0 {
		h.spawn(tb, spawnRequest{
			Count: n, Model: crate, Step: 0.5, Animated: animated,
			Animation: Animation{Plays: [MaxPlays]Play{
				{Clip: walk, Weight: 1, Loop: true}, {Clip: idle, Weight: 0.25},
			}},
		})
	}
	// Warm every arena the first frames grow, so what is measured is the steady
	// state and not the first tick: scene's queue keeps its backing across
	// frames, and so does the System's scratch.
	for range 100 {
		h.frame(tb)
	}
	return h
}

func BenchmarkFrameEmpty(b *testing.B)        { benchmarkFrame(b, 0, false) }
func BenchmarkFrame100(b *testing.B)          { benchmarkFrame(b, 100, false) }
func BenchmarkFrame1000(b *testing.B)         { benchmarkFrame(b, 1_000, false) }
func BenchmarkFrame5000(b *testing.B)         { benchmarkFrame(b, 5_000, false) }
func BenchmarkFrameAnimated1000(b *testing.B) { benchmarkFrame(b, 1_000, true) }
func BenchmarkFrameAnimated5000(b *testing.B) { benchmarkFrame(b, 5_000, true) }

// TestTheRecordedFrameAllocatesNothingPerDrawable is the ticket's allocation
// criterion, stated as the only form of it that means anything: the whole-frame
// count is identical with nothing to record and with five thousand drawables,
// so nothing in the binding scales with the entity count.
//
// It is measured as a steady state over ten thousand frames rather than as
// allocs/op, because allocs/op rounds and a fraction of an object a frame is
// exactly what a rare growth would look like.
func TestTheRecordedFrameAllocatesNothingPerDrawable(t *testing.T) {
	const frames = 10_000
	measure := func(n int, animated bool) float64 {
		h := newRecordingHarness(t, n, animated)
		mallocs := allocationsDuring(func() {
			for range frames {
				h.frame(t)
			}
		})
		return float64(mallocs) / frames
	}

	empty := measure(0, false)
	full := measure(5_000, false)
	animated := measure(5_000, true)
	t.Logf("objects a frame: nothing to record %.3f, 5 000 drawables %.3f, 5 000 animated drawables %.3f; %d subscribers to the tick",
		empty, full, animated, tickSubscribers(t))

	if full > empty+0.05 {
		t.Fatalf("recording 5 000 drawables costs %.3f objects a frame against %.3f with nothing to record",
			full, empty)
	}
	if animated > empty+0.05 {
		t.Fatalf("the play scratch costs %.3f objects a frame against %.3f with nothing to record",
			animated, empty)
	}
}

// TestTheRecordedFrameIsLinearInDrawables is the other half of the bar, which
// allocs/op alone would not catch: the worst iteration shape measured while this
// design was being settled cost 4x in time while scoring two allocations.
//
// It asserts the shape rather than a figure — the per-drawable cost at five
// thousand is no worse than at five hundred, within the noise a wall clock has
// on a loaded machine — and logs the figure, which is what the ticket asks to be
// recorded.
func TestTheRecordedFrameIsLinearInDrawables(t *testing.T) {
	if testing.Short() {
		t.Skip("the timing arm runs thousands of frames")
	}
	perDrawable := func(n int) float64 {
		h := newRecordingHarness(t, n, false)
		empty := newRecordingHarness(t, 0, false)
		const frames = 2_000
		base := timeFrames(t, empty, frames)
		full := timeFrames(t, h, frames)
		return float64(full-base) / float64(frames) / float64(n)
	}
	small := perDrawable(500)
	large := perDrawable(5_000)
	t.Logf("nanoseconds a drawable: at 500 %.1f, at 5 000 %.1f", small, large)
	if large > small*2 {
		t.Fatalf("the recording is superlinear: %.1f ns a drawable at 500, %.1f at 5 000", small, large)
	}
}

// timeFrames is the wall clock over a fixed number of whole frames, in
// nanoseconds. A benchmark cannot answer the per-drawable question directly:
// the engine's own per-publication charge and its scheduling variance are
// several microseconds, where the thing being measured is tens of nanoseconds
// an Entity, so the baseline has to be subtracted from the same shape of run.
func timeFrames(tb testing.TB, h *harness, frames int) time.Duration {
	tb.Helper()
	start := time.Now()
	for range frames {
		h.frame(tb)
	}
	return time.Since(start)
}

// tickSubscribers counts what the engine runs per tick, because the allocation
// floor above is the engine's own per-publication and per-subscriber charge and
// is meaningless without the number of subscribers it was measured over.
func tickSubscribers(tb testing.TB) int {
	tb.Helper()
	h := newHarnessOver(tb, fstest.MapFS{}, testConfig(), 8)
	count := 0
	for _, sub := range h.engine.Describe().Subscriptions {
		if sub.Event == reflect.TypeFor[app.UpdateEvent]() {
			count++
		}
	}
	return count
}

// benchmarkDrawnFrame is the whole frame with somewhere to draw: a camera, a
// resident model, and scene deciding, culling, sorting and packing every
// drawable the binding recorded. Against the frames above it prices what the
// bound plugin does with what it was handed, which includes the path scene
// re-resolves per draw per frame — 46.9 ns of it, recorded as
// https://github.com/dvoyni/cog/issues/263 and not the binding's to fix.
func benchmarkDrawnFrame(b *testing.B, n int) {
	h := newDrawingHarness(b, uint32(n)+8)
	h.spawn(b, spawnRequest{Count: n, Model: crate, Step: 0.001})
	h.frameUntil(b, "the crate to become resident", func() bool {
		passes := h.passes(b)
		return len(passes) == 1 && passes[0].Instances == n
	})
	for range 100 {
		h.frame(b)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.frame(b)
	}
}

func BenchmarkDrawnFrame1000(b *testing.B) { benchmarkDrawnFrame(b, 1_000) }
func BenchmarkDrawnFrame5000(b *testing.B) { benchmarkDrawnFrame(b, 5_000) }
