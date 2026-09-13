package ecssceneimpl

import (
	"reflect"
	"runtime"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// The numbers here are measured on a real engine driven by a real
// app.UpdateEvent with the real scene plugin composed beside the binding —
// publish, acquire every declared lock, run every System and every flush, wait.
//
// The frame has no camera and no resident model, which is deliberate: what the
// binding costs is the walk, the probes, the copy-out into scratch and the
// record into scene's queue, and what scene does with a draw once it has a
// camera to decide for is scene's number. Scene records a Model call — plays,
// overrides and material copied into its arenas — before it knows whether the
// path is resident.

// population is one arm of the cost table: n Model Entities, each carrying the
// optional Components the arm names.
type population struct {
	n         int
	animated  bool
	params    bool
	material  bool
	nameInLog string
}

var (
	arms = []population{
		{n: 0, nameInLog: "nothing to record"},
		{n: 5_000, nameInLog: "5 000 models"},
		{n: 5_000, animated: true, nameInLog: "5 000 animated"},
		{n: 5_000, params: true, nameInLog: "5 000 with Params"},
		{n: 5_000, material: true, nameInLog: "5 000 with Material"},
	}

	// benchAnimation blends two clips, which is the common walk-into-run case.
	benchAnimation = ecsscene.Animation{Plays: [ecsscene.MaxPlays]scene.ClipPlay{
		{Clip: "Walk", Weight: 1, Loop: true}, {Clip: "Run", Weight: 0.25, Loop: true},
	}}
	// benchParams is one tint, which is the per-Entity variation case.
	benchParams = ecsscene.Params{Values: ecs.NewList(gfx.ColorParam("baseColorFactor", m.Color{R: 1, A: 1}))}
	// benchMaterial is two pass tags with one parameter each, so the per-tag
	// scratch rule is exercised on every draw.
	benchMaterial = ecsscene.Material{Tags: ecs.NewList(
		ecsscene.MaterialTag{Shader: gfx.ShaderWithText("forward"), State: gpu.StateOpaque3D,
			Params: ecs.NewList(gfx.FloatParam("fade", 1))},
		ecsscene.MaterialTag{Tag: "shadow", Shader: gfx.ShaderWithText("shadow"), State: gpu.StateOpaque3D,
			Params: ecs.NewList(gfx.FloatParam("bias", 0.01))},
	)}
)

// newRecordingHarness is the binding under measurement, warmed past every
// arena the first frames grow: scene's queue keeps its backing across frames,
// and so does the recording scratch.
func newRecordingHarness(tb testing.TB, arm population) *harness {
	tb.Helper()
	h := newHarnessOver(tb, fstest.MapFS{}, uint32(arm.n)+8)
	if arm.n > 0 {
		request := spawnRequest{
			Count: arm.n, Step: 0.5,
			Model: &ecsscene.Model{Ref: scene.ModelRef{Path: crateModel}},
		}
		if arm.animated {
			request.Animation = &benchAnimation
		}
		if arm.params {
			request.Params = &benchParams
		}
		if arm.material {
			request.Material = &benchMaterial
		}
		h.spawn(tb, request)
	}
	for range 100 {
		h.frame(tb)
	}
	return h
}

// allocationsDuring counts the objects allocated while f runs, across every
// goroutine, which is what a per-Entity allocation would show up in.
func allocationsDuring(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.Mallocs - before.Mallocs
}

// TestRecordingAllocatesNothingPerEntity is the allocation criterion in the
// only form that means anything: the whole-frame count is the same with nothing
// to record as with five thousand Entities, and the same again when every one
// of them is animated, carries Params or carries a Material, so nothing in the
// binding scales with the Entity count.
//
// It is a steady state over thousands of frames rather than allocs/op, which
// rounds: a fraction of an object a frame is what a rare growth looks like. The
// harness subscribes no second recorder, because one waiting on scene's queue
// behind the binding costs the kernel's scheduler an allocation per blocked
// dispatch, and that would grow with frame length rather than with anything
// the binding does.
//
// Under load — this package's tests beside another package's, say — the
// runtime and the testing process add a few hundredths of an object a frame at
// random, enough to cross the bar once in a dozen runs. An allocation in the
// binding is not random: it is there in every frame of every round. So the
// arms are measured in interleaved rounds, each arm keeps its quietest, and a
// further round is taken only while some arm is still over the bar. Taking the
// minimum can hide noise and cannot hide a real allocation.
func TestRecordingAllocatesNothingPerEntity(t *testing.T) {
	const frames, maxRounds, bar = 3_000, 4, 0.05
	harnesses := make([]*harness, len(arms))
	for i, arm := range arms {
		harnesses[i] = newRecordingHarness(t, arm)
	}
	quietest := make([]float64, len(arms))
	over := func() []int {
		var arms []int
		for i := 1; i < len(quietest); i++ {
			if quietest[i] > quietest[0]+bar {
				arms = append(arms, i)
			}
		}
		return arms
	}
	for round := 0; round < maxRounds; round++ {
		for i, h := range harnesses {
			mallocs := allocationsDuring(func() {
				for range frames {
					h.frame(t)
				}
			})
			perFrame := float64(mallocs) / frames
			if round == 0 || perFrame < quietest[i] {
				quietest[i] = perFrame
			}
		}
		if len(over()) == 0 {
			t.Logf("settled after %d round(s)", round+1)
			break
		}
	}
	for i, arm := range arms {
		t.Logf("objects a frame, %s: %.3f", arm.nameInLog, quietest[i])
	}
	t.Logf("%d subscribers to the tick", tickSubscribers(t))
	for _, i := range over() {
		t.Errorf("%s costs %.3f objects a frame against %.3f with nothing to record, in its quietest of %d rounds",
			arms[i].nameInLog, quietest[i], quietest[0], maxRounds)
	}
}

// tickSubscribers counts what the engine runs per tick, because the allocation
// floor above is the engine's own per-publication and per-subscriber charge and
// means nothing without the number of subscribers it was measured over.
func tickSubscribers(tb testing.TB) int {
	tb.Helper()
	h := newHarnessOver(tb, fstest.MapFS{}, 8)
	count := 0
	for _, sub := range h.engine.Describe().Subscriptions {
		if sub.Event == reflect.TypeFor[app.UpdateEvent]() {
			count++
		}
	}
	return count
}

// benchmarkFrame is the classic b.N form deliberately: testing.B.Loop keeps its
// loop-assigned values alive, which is exactly what an allocation figure must
// not have helping it.
func benchmarkFrame(b *testing.B, arm population) {
	h := newRecordingHarness(b, arm)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.frame(b)
	}
}

func BenchmarkFrameEmpty(b *testing.B)        { benchmarkFrame(b, arms[0]) }
func BenchmarkFrame5000(b *testing.B)         { benchmarkFrame(b, arms[1]) }
func BenchmarkFrameAnimated5000(b *testing.B) { benchmarkFrame(b, arms[2]) }
func BenchmarkFrameParams5000(b *testing.B)   { benchmarkFrame(b, arms[3]) }
func BenchmarkFrameMaterial5000(b *testing.B) { benchmarkFrame(b, arms[4]) }
