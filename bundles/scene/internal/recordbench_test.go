package internal

import (
	"reflect"
	"runtime"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx"
)

// The numbers here are measured on a real engine driven by a real
// app.UpdateEvent and app.RenderEvent with the real model and gfx plugins
// composed beside the binding — publish, acquire every declared lock, run every
// System, wait.
//
// The allocation test's frame has no camera and no resident model, on a
// backend that never comes up. Since the recording System draws into gfx
// itself, such a frame keys nothing and records nothing: the load System waits
// for the backend and the recording System skips a frame it could not draw. What the test still holds is that the frame's
// fixed cost does not grow with the population. The benches report the drawn
// frame's allocations.
//
// The benches' frame draws: a camera, the model resident, and the recording
// System's bucketing, cull, sort, pack and emit into gfx, which replays into a
// backend that discards. It is the whole frame a game pays for, so the
// redesign has a before and an after measured by the same code.

// population is one arm of the cost table: n Model Entities, each carrying the
// optional Components the arm names.
type population struct {
	n        int
	animated bool
	params   bool
	material bool
	// sight sets a default scene shader with a param of its own, so every
	// file material is resolved under it rather than drawn as loaded.
	sight     bool
	nameInLog string
}

var (
	arms = []population{
		{n: 0, nameInLog: "nothing to record"},
		{n: 5_000, nameInLog: "5 000 models"},
		{n: 5_000, animated: true, nameInLog: "5 000 animated"},
		{n: 5_000, params: true, nameInLog: "5 000 with Params"},
		{n: 5_000, material: true, nameInLog: "5 000 with Material"},
		{n: 5_000, sight: true, nameInLog: "5 000 under a default shader"},
	}

	// benchAnimation blends two clips, which is the common walk-into-idle
	// case. They are the animated model's two, so the drawn arm samples both.
	benchAnimation = Animation{Plays: [model.MaxClipPlays]model.ClipPlay{
		{Clip: "Walk", Weight: 1, Loop: true}, {Clip: "Idle", Weight: 0.25, Loop: true},
	}}
	// sightShader is the sight arm's default scene shader: a source of its own
	// and one param every draw carries under the Entity's.
	sightShader = model.SceneShaderDescr{
		Source: gfx.ShaderWithText("sight"),
		Params: []gfx.ParameterDescr{gfx.FloatParam("fade", 0.5)},
	}
	// benchParams is one tint, which is the per-Entity variation case.
	benchParams = Params{Values: m.NewList(gfx.ColorParam("baseColorFactor", m.Color{R: 1, A: 1}))}
	// benchMaterial is two pass tags with one parameter each, so the per-tag
	// scratch rule is exercised on every draw.
	benchMaterial = Material{Tags: m.NewList(
		MaterialTag{Shader: gfx.ShaderWithText("forward"), State: gfx.StateOpaque3D(),
			Params: m.NewList(gfx.FloatParam("fade", 1))},
		MaterialTag{Tag: "shadow", Shader: gfx.ShaderWithText("shadow"), State: gfx.StateOpaque3D(),
			Params: m.NewList(gfx.FloatParam("bias", 0.01))},
	)}
)

// newRecordingHarness is the binding under measurement, warmed past every
// arena the first frames grow: the scratches keep their backing across
// frames.
func newRecordingHarness(tb testing.TB, arm population) *harness {
	tb.Helper()
	h := newHarnessOver(tb, fstest.MapFS{}, uint32(arm.n)+8)
	if arm.sight {
		h.kernel.ExecuteCommand[defaultShaderCmd](sightShader)
	}
	if arm.n > 0 {
		request := spawnRequest{
			Count: arm.n, Step: 0.5,
			Model: &Model{Ref: model.ModelRef{Path: crateModel}},
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
// harness subscribes no second recorder, because one waiting on gfx's queue
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
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
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
// floor above is the engine's own charge for this composition and means nothing
// without the number of subscribers it was measured over.
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

// The bench frame is a camera at Z=80 over a grid of 100 columns and 50 rows
// half a unit apart, which it sees whole: the grid the recording renderer
// removed in #573 benched as BenchmarkFrame.
const (
	benchColumns = 100
	benchStep    = 0.5
)

var benchEye = m.LookAt(m.Vec3{Z: 80}, m.Vec3{}, m.Vec3{Y: 1})

// benchPlace is where the arm's row-th row of Entities starts.
func benchPlace(row, rows int) m.Transform {
	return m.At(-benchColumns*benchStep/2, float32(row)*benchStep-float32(rows)*benchStep/2, 0)
}

// newFrameHarness is the whole frame an arm benches: the load System, the
// recording System with a camera to decide for - bucket, cull, sort, pack - and
// gfx's recording of the passes into a backend that is Ready and discards what
// it is handed. The model is resident and every
// Entity is packed before it returns, and the arenas the first frames grow are
// grown.
//
// The empty arm has a model made resident too, by an Entity that draws it and
// is despawned, so all five arms differ only in the population.
func newFrameHarness(b *testing.B, arm population) *harness {
	b.Helper()
	path := crateModel
	if arm.animated {
		path = animatedModel
	}
	files := fstest.MapFS{
		crateModel:    &fstest.MapFile{Data: crateGLB(b)},
		animatedModel: &fstest.MapFile{Data: animatedGLB(b)},
	}
	backend := &discardBackend{}
	h := newHarnessWith(b, files, uint32(arm.n)+8, backend)
	h.kernel.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{
		Width: 800, Height: 600, FramebufferWidth: 1600, FramebufferHeight: 1200,
	})
	h.spawn(b, spawnRequest{Place: benchEye, Camera: &Camera{FovY: 1.0472, Near: 0.1, Far: 200}})
	if arm.sight {
		h.kernel.ExecuteCommand[defaultShaderCmd](sightShader)
	}
	crate := &Model{Ref: model.ModelRef{Path: path}}
	want := int64(arm.n)
	if arm.n == 0 {
		resident := h.spawn(b, spawnRequest{Model: crate})
		h.frameUntil(b, "the model to become resident", func() bool { return backend.drew.Load() == 1 })
		h.despawn(b, resident)
	}
	rows := (arm.n + benchColumns - 1) / benchColumns
	for row := range rows {
		request := spawnRequest{
			Count: min(benchColumns, arm.n-row*benchColumns), Step: benchStep,
			Place: benchPlace(row, rows), Model: crate,
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
		h.spawn(b, request)
	}
	h.frameUntil(b, "every Entity to be drawn", func() bool { return backend.drew.Load() == want })
	for range 100 {
		h.frame(b)
	}
	return h
}

// benchmarkFrame is the classic b.N form deliberately: testing.B.Loop keeps its
// loop-assigned values alive, which is exactly what an allocation figure must
// not have helping it.
//
// It asserts nothing about the frame. It reports time and allocations, and
// TestRecordingAllocatesNothingPerEntity holds the allocation claim.
func benchmarkFrame(b *testing.B, arm population) {
	h := newFrameHarness(b, arm)
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
func BenchmarkFrameSight5000(b *testing.B)    { benchmarkFrame(b, arms[5]) }

// BenchmarkFrameDistinct5000 is 5 000 crates tinted 5 000 ways: 5 000 Batches,
// so 5 000 draws reach gfx, and gfx's per-draw work - the draw's parameter
// plan and its uniform block - is paid 5 000 times a frame rather than once.
// Every other arm collapses into one Batch and cannot see that cost.
func BenchmarkFrameDistinct5000(b *testing.B) {
	h := newFrameHarness(b, arms[0])
	const rows = 50
	for row := range rows {
		h.spawn(b, spawnRequest{
			Count: benchColumns, Step: benchStep, Place: benchPlace(row, rows),
			Model: &Model{Ref: model.ModelRef{Path: crateModel}},
			ParamsEach: func(i int) Params {
				tint := float32(row*benchColumns+i) / (rows * benchColumns)
				return Params{Values: m.NewList(gfx.ColorParam("baseColorFactor", m.Color{R: tint, A: 1}))}
			},
		})
	}
	for range 200 {
		h.frame(b)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.frame(b)
	}
}
