package rendertest_test

import (
	"slices"
	"sync"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

// flushWatcher is the test's plugin, listed after render: on each
// gfx.DrawEvent it schedules a task after what render's handler returned,
// which gfx's flush waits for, and keeps how many clear-after-draw reports
// the harness has by then.
type flushWatcher struct {
	harness *rendertest.Harness
	// mutex guards reportCounts, one for each frame.
	mutex        sync.Mutex
	reportCounts []int
}

func (*flushWatcher) Name() kernel.PluginName { return "flush watcher" }

func (*flushWatcher) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

func (w *flushWatcher) Init(configurator *kernel.Configurator, _ any) error {
	count := configurator.RegisterTask0(func(k *kernel.Kernel, _ kernel.NoArg) kernel.NoResult {
		reportCount := countReports(w.harness, render.ReportKindClearAfterDraw)
		w.mutex.Lock()
		w.reportCounts = append(w.reportCounts, reportCount)
		w.mutex.Unlock()
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.DrawEvent) (gfx.DrawEvent, kernel.JobHandle) {
		return event, ex.Schedule(count, kernel.NoArg{}, in).Handle()
	})
	return nil
}

func (*flushWatcher) Start(*kernel.Kernel) error { return nil }

func (*flushWatcher) Stop(*kernel.Kernel) error { return nil }

func (*flushWatcher) Destroy() error { return nil }

// clearScene is what the clear-after-draw tests draw with, made in their
// first frame: the draw scene, a plain material to draw its meshes with,
// and a full-screen material to draw with no mesh.
type clearScene struct {
	drawScene
	plain, fullscreen render.Material
}

// spawnClearHarness spawns h with the draw tests' shaders mounted and a
// window open, beside plugins, running duringFrame in each frame's recording
// task with the window and the clear scene, and steps frameCount frames.
func spawnClearHarness(t *testing.T, h *rendertest.Harness, frameCount int, plugins []kernel.Plugin, duringFrame func(frame render.Frame, window platform.Window, scene clearScene)) {
	t.Helper()
	var window platform.Window
	var scene clearScene
	loader := &loaderPlugin{files: makeDrawFiles(t), duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = clearScene{
				drawScene:  makeDrawScene(inventory),
				plain:      inventory.MakeMaterial(render.MaterialDescriptor{Label: "plain", Shader: inventory.AcquireShader("plain.wgsl")}),
				fullscreen: inventory.MakeMaterial(render.MaterialDescriptor{Label: "fullscreen", Shader: inventory.AcquireShader("fullscreen.wgsl")}),
			}
		}
		duringFrame(frame, window, scene)
	}}
	h.Spawn(nil, append([]kernel.Plugin{loader}, plugins...)...)
	window = h.OpenWindow(platform.Size{Width: 64, Height: 48})
	for range frameCount {
		h.Step()
	}
}

// recordPasses records into list a pass of each descriptor, drawing scene's
// indexed mesh with its plain material in those whose label is in drawn.
func recordPasses(list render.CommandList, scene clearScene, drawn []string, descriptors ...render.PassDescriptor) {
	for _, descriptor := range descriptors {
		pass := list.BeginPass(descriptor)
		if slices.Contains(drawn, descriptor.Label) {
			pass.SetMaterial(scene.plain)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		}
		pass.End()
	}
}

func TestAPassClearingATargetAnEarlierPassDrewIntoIsReportedOnceNamingBothBeforeGfxsFlush(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindClearAfterDraw)
	watcher := &flushWatcher{harness: h}
	spawnClearHarness(t, h, 3, []kernel.Plugin{watcher}, func(frame render.Frame, window platform.Window, scene clearScene) {
		multisampled := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "multisampled", Width: 8, Height: 8, Format: render.TextureFormatRGBA8Unorm, SampleCount: 4})
		resolved := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "resolved", Width: 8, Height: 8, Format: render.TextureFormatRGBA8Unorm})
		shadow := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "shadow", Width: 8, Height: 8, Format: render.TextureFormatDepth32Float})
		// The present is recorded before the interface, but submitted after.
		present := frame.BeginCommandList("present", 20)
		recordPasses(present, scene, []string{"present"}, render.PassDescriptor{Label: "present", ColorTargets: []render.ColorTarget{{Window: window, Clear: true}}})
		present.End()
		userInterface := frame.BeginCommandList("interface", 5)
		pass := userInterface.BeginPass(render.PassDescriptor{Label: "interface", ColorTargets: []render.ColorTarget{{Window: window}}})
		pass.SetMaterial(scene.fullscreen)
		pass.DrawVertices(render.PrimitiveTopologyTriangleList, 3, render.DrawArgs{})
		pass.End()
		userInterface.End()
		list := frame.BeginCommandList("scene", drawOrder)
		// "again" clears what "overlay" drew into last, "cleaner" what only
		// "clean" cleared since "again" drew, and "wipe" what "multisampled"
		// resolved into, "shadow again" what "shadow" drew into as its
		// depth-stencil target, and "again" and "clean" scene's depth too.
		recordPasses(list, scene, []string{"scene", "overlay", "again", "multisampled", "shadow"},
			scene.colorPass("scene"),
			scene.makeKeptPass("overlay"),
			scene.colorPass("again"),
			scene.colorPass("clean"),
			scene.colorPass("cleaner"),
			render.PassDescriptor{Label: "multisampled", ColorTargets: []render.ColorTarget{{Texture: multisampled, Clear: true, Discard: true, Resolve: render.ResolveTarget{Texture: resolved}}}},
			render.PassDescriptor{Label: "wipe", ColorTargets: []render.ColorTarget{{Texture: resolved, Clear: true}}},
			render.PassDescriptor{Label: "shadow", DepthStencil: render.DepthStencilTarget{Texture: shadow, Clear: true, ClearDepth: 1}},
			render.PassDescriptor{Label: "shadow again", DepthStencil: render.DepthStencilTarget{Texture: shadow, Clear: true, ClearDepth: 1}},
		)
		list.End()
	})

	var messages []string
	for _, report := range h.Reports() {
		if report.Kind == render.ReportKindClearAfterDraw {
			messages = append(messages, report.Error())
		}
	}
	want := []string{
		`render: pass "again" at order 10 clears a target that pass "overlay" at order 10 drew into earlier in the frame`,
		`render: pass "clean" at order 10 clears a target that pass "again" at order 10 drew into earlier in the frame`,
		`render: pass "wipe" at order 10 clears a target that pass "multisampled" at order 10 drew into earlier in the frame`,
		`render: pass "shadow again" at order 10 clears a target that pass "shadow" at order 10 drew into earlier in the frame`,
		`render: pass "present" at order 20 clears a target that pass "interface" at order 5 drew into earlier in the frame`,
	}
	if !slices.Equal(messages, want) {
		t.Errorf("render reported\n%q\nover three frames, want each pass once, in submit order\n%q", messages, want)
	}
	if want := []int{5, 5, 5}; !slices.Equal(watcher.reportCounts, want) {
		t.Errorf("the frames had %v clear-after-draw reports when render's handler of gfx.DrawEvent finished, want %v: the first frame's before its flush", watcher.reportCounts, want)
	}
}

func TestKeepingATargetOrClearingOneNothingDrewIntoReportsNothing(t *testing.T) {
	h := rendertest.NewHarness(t)
	spawnClearHarness(t, h, 2, nil, func(frame render.Frame, window platform.Window, scene clearScene) {
		levels := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "levels", Width: 8, Height: 8, Format: render.TextureFormatRGBA8Unorm, MipLevelCount: 2})
		other := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "other", Width: 8, Height: 8, Format: render.TextureFormatRGBA8Unorm})
		prepass := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "prepass", Width: 8, Height: 8, Format: render.TextureFormatDepth32Float})
		// Recorded first, submitted after the pass that clears other.
		late := frame.BeginCommandList("late", 30)
		recordPasses(late, scene, []string{"late"}, render.PassDescriptor{Label: "late", ColorTargets: []render.ColorTarget{{Texture: other}}})
		late.End()
		early := frame.BeginCommandList("early", 25)
		recordPasses(early, scene, nil, render.PassDescriptor{Label: "early", ColorTargets: []render.ColorTarget{{Texture: other, Clear: true}}})
		early.End()
		list := frame.BeginCommandList("scene", drawOrder)
		// "read-only" tests against the prepass's depth without writing it.
		recordPasses(list, scene, []string{"second", "kept", "read-only", "level 0"},
			scene.colorPass("first"),
			scene.colorPass("second"),
			scene.makeKeptPass("kept"),
			render.PassDescriptor{Label: "prepass", DepthStencil: render.DepthStencilTarget{Texture: prepass, Clear: true, ClearDepth: 1}},
			render.PassDescriptor{Label: "read-only", ColorTargets: []render.ColorTarget{{Texture: scene.color}}, DepthStencil: render.DepthStencilTarget{Texture: prepass, ReadOnly: true}},
			render.PassDescriptor{Label: "prepass again", DepthStencil: render.DepthStencilTarget{Texture: prepass, Clear: true, ClearDepth: 1}},
			render.PassDescriptor{Label: "level 0", ColorTargets: []render.ColorTarget{{Texture: levels, Clear: true}}},
			render.PassDescriptor{Label: "level 1", ColorTargets: []render.ColorTarget{{Texture: levels, MipLevel: 1, Clear: true}}},
		)
		list.End()
		windowList := frame.BeginCommandList("window", 40)
		recordPasses(windowList, scene, nil, render.PassDescriptor{Label: "window", ColorTargets: []render.ColorTarget{{Window: window, Clear: true}}})
		windowList.End()
	})
}
