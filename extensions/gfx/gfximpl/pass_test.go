package gfximpl

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/extensions/gfx/internal"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// passFrame records one frame through the plugin and reports the passes the
// backend was asked to encode.
func passFrame(t *testing.T, record func(*gfx.OpQueue)) (*fakeBackend, kernel.Executioner) {
	t.Helper()
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	record(recordRaw(t, k))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	return backend, k
}

func drawInto(q *gfx.OpQueue) {
	q.Draw(triangle(), testMaterial(), gfx.MatParam("mvp", m.NewMat4()))
}

func TestAPassCarriesItsTargetAndClearToTheBackend(t *testing.T) {
	backend, _ := passFrame(t, func(q *gfx.OpQueue) {
		q.Pass(gfx.PassDescr{
			Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(),
			Load: gpu.LoadClear, Clear: m.Color{R: 1, A: 1}, Label: "screen",
		})
		drawInto(q)
	})
	if len(backend.lastPasses) != 1 {
		t.Fatalf("passes = %d, want 1", len(backend.lastPasses))
	}
	pass := backend.lastPasses[0]
	if !pass.Screen || !pass.DepthAuto {
		t.Errorf("pass = %+v, want the screen target with automatic depth", pass)
	}
	if pass.Load != gpu.LoadClear || pass.Clear != (m.Color{R: 1, A: 1}) {
		t.Errorf("pass load = %v clear = %v, want LoadClear with the declared colour", pass.Load, pass.Clear)
	}
	if backend.passDraws[0] != 1 {
		t.Errorf("draws in the pass = %d, want 1", backend.passDraws[0])
	}
}

func TestDrawsOutsideAnyPassAreDroppedAndReported(t *testing.T) {
	p := newPlugin()
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	// No pass declared: there is no implicit one to absorb the draw.
	drawInto(recordRaw(t, k))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.lastPasses) != 0 {
		t.Errorf("passes = %d, want none", len(backend.lastPasses))
	}
	var dropped gfx.ErrDrawWithoutPass
	for _, err := range reported {
		if errors.As(err, &dropped) && dropped.Count == 1 {
			return
		}
	}
	t.Errorf("reported errors = %v, want one ErrDrawWithoutPass for 1 draw", reported)
}

func TestPassesRunInDeclaredOrderNotStreamOrder(t *testing.T) {
	backend, _ := passFrame(t, func(q *gfx.OpQueue) {
		late := q.Pass(gfx.PassDescr{Order: 10, Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), Load: gpu.LoadClear, Label: "late"})
		drawInto(q)
		early := q.Pass(gfx.PassDescr{Order: -10, Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), Load: gpu.LoadClear, Label: "early"})
		drawInto(q)
		// Re-selecting appends to a pass declared earlier in the frame.
		q.SetPass(late)
		drawInto(q)
		_ = early
	})
	if len(backend.lastPasses) != 2 {
		t.Fatalf("passes = %d, want 2", len(backend.lastPasses))
	}
	if backend.lastPasses[0].Label != "early" || backend.lastPasses[1].Label != "late" {
		t.Fatalf("pass order = %q then %q, want early then late", backend.lastPasses[0].Label, backend.lastPasses[1].Label)
	}
	if backend.passDraws[0] != 1 || backend.passDraws[1] != 2 {
		t.Errorf("draws per pass = %v, want 1 in early and 2 in late", backend.passDraws)
	}
}

func TestEqualOrderKeepsDeclarationSequence(t *testing.T) {
	backend, _ := passFrame(t, func(q *gfx.OpQueue) {
		q.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), Load: gpu.LoadClear, Label: "first"})
		drawInto(q)
		q.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), Load: gpu.LoadClear, Label: "second"})
		drawInto(q)
	})
	if len(backend.lastPasses) != 2 || backend.lastPasses[0].Label != "first" {
		t.Fatalf("passes = %+v, want first then second", backend.lastPasses)
	}
}

func TestAdjacentPassesMergeWhenTheSuccessorOnlyContinues(t *testing.T) {
	backend, _ := passFrame(t, func(q *gfx.OpQueue) {
		q.Pass(gfx.PassDescr{Order: 0, Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), Load: gpu.LoadClear, Label: "layer0"})
		drawInto(q)
		// Same attachments, preserving both, after a pass that kept both: by
		// definition indistinguishable from continuing the first.
		q.Pass(gfx.PassDescr{Order: 1, Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), Label: "layer1"})
		drawInto(q)
	})
	if len(backend.lastPasses) != 1 {
		t.Fatalf("passes = %d, want the two merged into 1", len(backend.lastPasses))
	}
	if backend.lastPasses[0].Load != gpu.LoadClear {
		t.Errorf("merged load = %v, want the first pass's LoadClear", backend.lastPasses[0].Load)
	}
	if backend.passDraws[0] != 2 {
		t.Errorf("draws in the merged pass = %d, want 2", backend.passDraws[0])
	}
}

func TestAPassThatClearsAgainDoesNotMerge(t *testing.T) {
	backend, _ := passFrame(t, func(q *gfx.OpQueue) {
		q.Pass(gfx.PassDescr{Order: 0, Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), Load: gpu.LoadClear, Label: "first"})
		drawInto(q)
		q.Pass(gfx.PassDescr{Order: 1, Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), DepthLoad: gpu.LoadClear, Label: "second"})
		drawInto(q)
	})
	if len(backend.lastPasses) != 2 {
		t.Fatalf("passes = %d, want 2: clearing depth is an effect the merge would lose", len(backend.lastPasses))
	}
}

func TestPassWithoutDrawsRunsOnlyWhenAnAttachmentLoads(t *testing.T) {
	backend, _ := passFrame(t, func(q *gfx.OpQueue) {
		// A camera that culled everything, but still clears its target.
		q.Pass(gfx.PassDescr{Order: 0, Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), Load: gpu.LoadClear, Label: "clearing"})
		// Nothing to draw and nothing to load: unobservable, so it is not encoded.
		q.Pass(gfx.PassDescr{Order: 5, Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), Label: "empty"})
	})
	if len(backend.lastPasses) != 1 || backend.lastPasses[0].Label != "clearing" {
		t.Fatalf("passes = %+v, want only the clearing one", backend.lastPasses)
	}
}

func TestScreenPassesAreFollowedByOnePresentPass(t *testing.T) {
	backend, _ := passFrame(t, func(q *gfx.OpQueue) {
		q.Pass(gfx.PassDescr{Order: 0, Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), Load: gpu.LoadClear, Label: "first"})
		drawInto(q)
		q.Pass(gfx.PassDescr{Order: 1, Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), DepthLoad: gpu.LoadClear, Label: "second"})
		drawInto(q)
	})
	if backend.presents != 1 {
		t.Fatalf("present passes = %d, want exactly 1 however many screen passes there were", backend.presents)
	}
	// The present pass reads what the frame wrote, so it can only run last.
	if backend.presentAfter != len(backend.lastPasses) {
		t.Errorf("present ran after %d of %d passes, want last", backend.presentAfter, len(backend.lastPasses))
	}
}

func TestAFrameThatNeverTouchesTheScreenDoesNotPresent(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	var target gfx.TextureDescr
	withResourceQueue(t, k, func(resources *gfx.ResourceQueue) {
		target = resources.AllocateTexture(64, 64, 1, gpu.FormatRGBA8Srgb)
	})
	w := recordRaw(t, k)
	w.Pass(gfx.PassDescr{Target: gfx.TextureTarget(target, 0, 0), Depth: gfx.DepthNone(), Load: gpu.LoadClear, Label: "offscreen"})
	w.Draw(triangle(), testMaterial(), gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.lastPasses) != 1 {
		t.Fatalf("passes = %d, want the one texture pass", len(backend.lastPasses))
	}
	// Nothing rendered into the frame buffer, so there is nothing to show and
	// blitting it over the screen would only wipe the last frame out.
	if backend.presents != 0 {
		t.Errorf("present passes = %d, want none: no pass named the screen", backend.presents)
	}
}

func TestAScreenPassThatIsDroppedDoesNotPresent(t *testing.T) {
	backend, _ := passFrame(t, func(q *gfx.OpQueue) {
		// Nothing to draw and nothing to load: the pass is not encoded, so the
		// frame buffer is never allocated and there is nothing to present.
		q.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), Label: "empty"})
	})
	if len(backend.lastPasses) != 0 || backend.presents != 0 {
		t.Errorf("passes = %d, presents = %d, want neither", len(backend.lastPasses), backend.presents)
	}
}

func TestDrawSamplingItsOwnAttachmentIsRejected(t *testing.T) {
	p := newPlugin()
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	var target gfx.TextureDescr
	withResourceQueue(t, k, func(resources *gfx.ResourceQueue) {
		target = resources.AllocateTexture(64, 64, 1, gpu.FormatRGBA8Srgb)
	})
	w := recordRaw(t, k)
	w.Pass(gfx.PassDescr{Target: gfx.TextureTarget(target, 0, 0), Depth: gfx.DepthNone(), Load: gpu.LoadClear, Label: "feedback"})
	w.Draw(triangle(), testMaterial(gfx.TextureParam("MainTexture", target)), gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.passDraws[0] != 0 {
		t.Errorf("draws = %d, want the feedback draw dropped", backend.passDraws[0])
	}
	if len(reported) == 0 {
		t.Error("no error reported for a draw sampling its own attachment")
	}
}

func TestATextureTargetCarriesItsSize(t *testing.T) {
	// A recorder that resolves a projection needs the target's aspect, and the
	// only place the size is known is the descriptor it was built from.
	texture := internal.BakedTexture(7, 1024, 256)
	width, height, ok := gfx.TextureTarget(texture, 0, 0).Size()
	if !ok || width != 1024 || height != 256 {
		t.Errorf("size = %d x %d (ok %v), want 1024 x 256", width, height, ok)
	}
	if _, _, ok := gfx.ScreenTarget().Size(); ok {
		t.Error("the screen sentinel reported a size; only the render thread knows it")
	}
	if _, _, ok := gfx.NoTarget().Size(); ok {
		t.Error("a colourless target reported a size")
	}
}

func TestADepthTargetCarriesItsSize(t *testing.T) {
	// A depth-only pass has no colour target, so its aspect comes from here.
	texture := internal.BakedTexture(9, 2048, 2048)
	width, height, ok := gfx.DepthTarget(texture).Size()
	if !ok || width != 2048 || height != 2048 {
		t.Errorf("size = %d x %d (ok %v), want 2048 x 2048", width, height, ok)
	}
	if _, _, ok := gfx.DepthAuto().Size(); ok {
		t.Error("DepthAuto reported a size; it takes the colour target's")
	}
}

func TestATemporaryTargetCarriesItsSize(t *testing.T) {
	q := internal.NewOpQueue(idsOf(&fakeBackend{}))
	target, _ := q.TemporaryTarget(640, 480, gpu.FormatRGBA8Srgb)
	width, height, ok := target.Size()
	if !ok || width != 640 || height != 480 {
		t.Errorf("size = %d x %d (ok %v), want 640 x 480", width, height, ok)
	}
}

func TestATemporaryTargetHandsBackTheTextureItRendersInto(t *testing.T) {
	// The whole reason the allocation exists is that a later pass samples it,
	// and a target is write-only: it names an attachment and answers nothing
	// about the texture behind it. So the texture has to come back from the
	// same call, already carrying the size and format the caller asked for.
	q := internal.NewOpQueue(idsOf(&fakeBackend{}))
	target, texture := q.TemporaryTarget(320, 200, gpu.FormatRGBA8Srgb)

	if texture.ID() == 0 {
		t.Fatal("the texture came back with no id, so nothing can sample it")
	}
	if texture.ID() != internal.TargetTextureOf(&target) {
		t.Errorf("texture id = %d, but the target renders into %d", texture.ID(), internal.TargetTextureOf(&target))
	}
	if width, height := texture.Size(); width != 320 || height != 200 {
		t.Errorf("texture size = %d x %d, want 320 x 200", width, height)
	}
	if texture.Format() != gpu.FormatRGBA8Srgb {
		t.Errorf("texture format = %v, want FormatRGBA8Srgb", texture.Format())
	}
	if internal.TextureSource(&texture) != gfx.TextureSourceBaked {
		t.Errorf("texture source = %v, want TextureSourceBaked", internal.TextureSource(&texture))
	}
}

func TestALaterPassSamplesWhatAnEarlierPassRenderedIntoATemporaryTarget(t *testing.T) {
	// The frame-local render-then-sample round trip, which is how split-screen,
	// minimap and post-processing are all spelled. The same-pass guard must not
	// fire here: the sampling draw is in a different pass.
	backend := &fakeBackend{}
	var sampled gpu.TextureID
	frame := func(q *gfx.OpQueue) {
		target, texture := q.TemporaryTarget(64, 64, gpu.FormatRGBA8Srgb)
		sampled = texture.ID()
		q.Pass(gfx.PassDescr{Target: target, Depth: gfx.DepthNone(), Load: gpu.LoadClear, Order: 0, Label: "offscreen"})
		drawInto(q)
		q.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthNone(), Load: gpu.LoadClear, Order: 1, Label: "composite"})
		q.Draw(triangle(), testMaterial(gfx.TextureParam("MainTexture", texture)), gfx.MatParam("mvp", m.NewMat4()))
	}

	p := newPlugin()
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	frame(recordRaw(t, k))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(reported) != 0 {
		t.Fatalf("errors reported for a cross-pass sample: %v", reported)
	}
	if len(backend.passDraws) != 2 {
		t.Fatalf("passes = %d, want 2", len(backend.passDraws))
	}
	if backend.passDraws[1] != 1 {
		t.Errorf("composite pass drew %d times, want the sampling draw kept", backend.passDraws[1])
	}
	if !backend.boundTexture(sampled) {
		t.Errorf("texture %d never reached the backend as a binding", sampled)
	}
}

func TestADrawStillCannotSampleTheTemporaryTargetItsOwnPassRendersInto(t *testing.T) {
	// Handing the texture back is only safe because this guard survives it.
	backend := &fakeBackend{}
	p := newPlugin()
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	w := recordRaw(t, k)
	target, texture := w.TemporaryTarget(64, 64, gpu.FormatRGBA8Srgb)
	w.Pass(gfx.PassDescr{Target: target, Depth: gfx.DepthNone(), Load: gpu.LoadClear, Label: "feedback"})
	w.Draw(triangle(), testMaterial(gfx.TextureParam("MainTexture", texture)), gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.passDraws[0] != 0 {
		t.Errorf("draws = %d, want the feedback draw dropped", backend.passDraws[0])
	}
	if len(reported) == 0 {
		t.Error("no error reported for a draw sampling the temporary target its own pass writes")
	}
}

func TestTheZeroAttachmentsAreTheScreenAndAutomaticDepth(t *testing.T) {
	// A recorder that defaults a pass leaves these fields alone, so the zero
	// value has to be the common case. NoTarget and DepthNone stay reachable and
	// distinguishable, which is what a depth-only shadow pass needs.
	if (gfx.TargetDescr{}) != gfx.ScreenTarget() {
		t.Error("the zero target is not the screen sentinel")
	}
	if (gfx.DepthDescr{}) != gfx.DepthAuto() {
		t.Error("the zero depth attachment is not DepthAuto")
	}
	if gfx.NoTarget() == gfx.ScreenTarget() || gfx.DepthNone() == gfx.DepthAuto() {
		t.Error("a colourless or depthless pass is indistinguishable from an unset one")
	}
}
