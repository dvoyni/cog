package internal

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// passFrame records one frame through the plugin and reports the passes the
// backend was asked to encode.
func passFrame(t *testing.T, record func(*OpQueue, types.DrawStateID)) (*fakeBackend, kernel.Executioner) {
	t.Helper()
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	set := testSet(t, k)
	record(recordRaw(t, k), set)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	return backend, k
}

func drawInto(q *OpQueue, ref types.PassID, set types.DrawStateID) {
	q.Draw(ref, triangle(), set, 1, 0)
}

func TestAPassCarriesItsTargetAndClearToTheBackend(t *testing.T) {
	backend, _ := passFrame(t, func(q *OpQueue, set types.DrawStateID) {
		ref := q.NewPass(types.PassDescr{
			Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(),
			Load: types.LoadClear, Clear: m.Color{R: 1, A: 1}, Label: "screen",
		})
		drawInto(q, ref, set)
	})
	if len(backend.lastPasses) != 1 {
		t.Fatalf("passes = %d, want 1", len(backend.lastPasses))
	}
	pass := backend.lastPasses[0]
	if pass.Target.Kind != types.TargetScreen || pass.Depth.Kind != types.DepthKindAuto {
		t.Errorf("pass = %+v, want the screen target with automatic depth", pass)
	}
	if pass.Load != types.LoadClear || pass.Clear != (m.Color{R: 1, A: 1}) {
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

	// No pass declared: a reference that names none cannot absorb the draw.
	set := testSet(t, k)
	drawInto(recordRaw(t, k), 0, set)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.lastPasses) != 0 {
		t.Errorf("passes = %d, want none", len(backend.lastPasses))
	}
	var dropped types.ErrDrawWithoutPass
	for _, err := range reported {
		if errors.As(err, &dropped) && dropped.Count == 1 {
			return
		}
	}
	t.Errorf("reported errors = %v, want one ErrDrawWithoutPass for 1 draw", reported)
}

func TestPassesRunInDeclaredOrderNotStreamOrder(t *testing.T) {
	backend, _ := passFrame(t, func(q *OpQueue, set types.DrawStateID) {
		late := q.NewPass(types.PassDescr{Order: 10, Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), Load: types.LoadClear, Label: "late"})
		drawInto(q, late, set)
		early := q.NewPass(types.PassDescr{Order: -10, Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), Load: types.LoadClear, Label: "early"})
		drawInto(q, early, set)
		// A draw names its pass, so it may land in one declared earlier.
		drawInto(q, late, set)
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
	backend, _ := passFrame(t, func(q *OpQueue, set types.DrawStateID) {
		ref := q.NewPass(types.PassDescr{Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), Load: types.LoadClear, Label: "first"})
		drawInto(q, ref, set)
		ref = q.NewPass(types.PassDescr{Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), Load: types.LoadClear, Label: "second"})
		drawInto(q, ref, set)
	})
	if len(backend.lastPasses) != 2 || backend.lastPasses[0].Label != "first" {
		t.Fatalf("passes = %+v, want first then second", backend.lastPasses)
	}
}

func TestAdjacentPassesMergeWhenTheSuccessorOnlyContinues(t *testing.T) {
	backend, _ := passFrame(t, func(q *OpQueue, set types.DrawStateID) {
		ref := q.NewPass(types.PassDescr{Order: 0, Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), Load: types.LoadClear, Label: "layer0"})
		drawInto(q, ref, set)
		// Same attachments, preserving both, after a pass that kept both: by
		// definition indistinguishable from continuing the first.
		ref = q.NewPass(types.PassDescr{Order: 1, Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), Label: "layer1"})
		drawInto(q, ref, set)
	})
	if len(backend.lastPasses) != 1 {
		t.Fatalf("passes = %d, want the two merged into 1", len(backend.lastPasses))
	}
	if backend.lastPasses[0].Load != types.LoadClear {
		t.Errorf("merged load = %v, want the first pass's LoadClear", backend.lastPasses[0].Load)
	}
	if backend.passDraws[0] != 2 {
		t.Errorf("draws in the merged pass = %d, want 2", backend.passDraws[0])
	}
}

func TestAPassThatClearsAgainDoesNotMerge(t *testing.T) {
	backend, _ := passFrame(t, func(q *OpQueue, set types.DrawStateID) {
		ref := q.NewPass(types.PassDescr{Order: 0, Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), Load: types.LoadClear, Label: "first"})
		drawInto(q, ref, set)
		ref = q.NewPass(types.PassDescr{Order: 1, Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), DepthLoad: types.LoadClear, Label: "second"})
		drawInto(q, ref, set)
	})
	if len(backend.lastPasses) != 2 {
		t.Fatalf("passes = %d, want 2: clearing depth is an effect the merge would lose", len(backend.lastPasses))
	}
}

func TestPassWithoutDrawsRunsOnlyWhenAnAttachmentLoads(t *testing.T) {
	backend, _ := passFrame(t, func(q *OpQueue, set types.DrawStateID) {
		// A camera that culled everything, but still clears its target.
		q.NewPass(types.PassDescr{Order: 0, Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), Load: types.LoadClear, Label: "clearing"})
		// Nothing to draw and nothing to load: unobservable, so it is not encoded.
		q.NewPass(types.PassDescr{Order: 5, Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), Label: "empty"})
	})
	if len(backend.lastPasses) != 1 || backend.lastPasses[0].Label != "clearing" {
		t.Fatalf("passes = %+v, want only the clearing one", backend.lastPasses)
	}
}

func TestScreenPassesAreFollowedByOnePresentPass(t *testing.T) {
	backend, _ := passFrame(t, func(q *OpQueue, set types.DrawStateID) {
		ref := q.NewPass(types.PassDescr{Order: 0, Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), Load: types.LoadClear, Label: "first"})
		drawInto(q, ref, set)
		ref = q.NewPass(types.PassDescr{Order: 1, Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), DepthLoad: types.LoadClear, Label: "second"})
		drawInto(q, ref, set)
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

	var target types.TextureDescr
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		target = resources.NewTexture(64, 64, 1, types.FormatRGBA8Srgb, false)
	})
	w := recordRaw(t, k)
	ref := w.NewPass(types.PassDescr{Target: types.TargetDescrTexture(target, 0, 0), Depth: types.DepthDescrNone(), Load: types.LoadClear, Label: "offscreen"})
	w.Draw(ref, triangle(), testSet(t, k), 1, 0)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
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
	backend, _ := passFrame(t, func(q *OpQueue, set types.DrawStateID) {
		// Nothing to draw and nothing to load: the pass is not encoded, so the
		// frame buffer is never allocated and there is nothing to present.
		q.NewPass(types.PassDescr{Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), Label: "empty"})
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

	var target types.TextureDescr
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		target = resources.NewTexture(64, 64, 1, types.FormatRGBA8Srgb, false)
	})
	w := recordRaw(t, k)
	ref := w.NewPass(types.PassDescr{Target: types.TargetDescrTexture(target, 0, 0), Depth: types.DepthDescrNone(), Load: types.LoadClear, Label: "feedback"})
	w.Draw(ref, triangle(), testSet(t, k, types.ShaderParameterTexture("MainTexture", target)), 1, 0)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
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
	texture := types.BakedTexture(7, 1024, 256)
	target := types.TargetDescrTexture(texture, 0, 0)
	if target.Width != 1024 || target.Height != 256 {
		t.Errorf("size = %d x %d, want 1024 x 256", target.Width, target.Height)
	}
}

func TestADepthTargetCarriesItsSize(t *testing.T) {
	// A depth-only pass has no colour target, so its aspect comes from here.
	texture := types.BakedTexture(9, 2048, 2048)
	depth := types.DepthDescrTarget(texture)
	if depth.Width != 2048 || depth.Height != 2048 {
		t.Errorf("size = %d x %d, want 2048 x 2048", depth.Width, depth.Height)
	}
}

func TestATemporaryTargetCarriesItsSize(t *testing.T) {
	q := NewOpQueue(idsOf(&fakeBackend{}))
	target, _ := q.NewTemporaryTarget(640, 480, types.FormatRGBA8Srgb)
	if target.Kind != types.TargetTexture || target.Width != 640 || target.Height != 480 {
		t.Errorf("target = %+v, want a 640 x 480 texture target", target)
	}
}

func TestATemporaryTargetHandsBackTheTextureItRendersInto(t *testing.T) {
	// The whole reason the allocation exists is that a later pass samples it,
	// and a target is write-only: it names an attachment and answers nothing
	// about the texture behind it. So the texture has to come back from the
	// same call, already carrying the size and format the caller asked for.
	q := NewOpQueue(idsOf(&fakeBackend{}))
	target, texture := q.NewTemporaryTarget(320, 200, types.FormatRGBA8Srgb)

	if texture.Params.ID == 0 {
		t.Fatal("the texture came back with no id, so nothing can sample it")
	}
	if texture.Params.ID != target.Texture {
		t.Errorf("texture id = %d, but the target renders into %d", texture.Params.ID, target.Texture)
	}
	if width, height := texture.Params.Width, texture.Params.Height; width != 320 || height != 200 {
		t.Errorf("texture size = %d x %d, want 320 x 200", width, height)
	}
	if texture.Params.Format != types.FormatRGBA8Srgb {
		t.Errorf("texture format = %v, want FormatRGBA8Srgb", texture.Params.Format)
	}
	// A baked texture is the one case that carries an id and no path, which is
	// what the descriptor says instead of a source marker.
	if texture.Name != "" || texture.Blob.Len() != 0 {
		t.Errorf("texture reads as path %q with %d inline bytes, want a baked id alone", texture.Name, texture.Blob.Len())
	}
}

func TestALaterPassSamplesWhatAnEarlierPassRenderedIntoATemporaryTarget(t *testing.T) {
	// The frame-local render-then-sample round trip, which is how split-screen,
	// minimap and post-processing are all spelled. The same-pass guard must not
	// fire here: the sampling draw is in a different pass.
	backend := &fakeBackend{}
	var sampled types.TextureID
	frame := func(q *OpQueue, set, sampler types.DrawStateID) {
		target, texture := q.NewTemporaryTarget(64, 64, types.FormatRGBA8Srgb)
		sampled = texture.Params.ID
		ref := q.NewPass(types.PassDescr{Target: target, Depth: types.DepthDescrNone(), Load: types.LoadClear, Order: 0, Label: "offscreen"})
		drawInto(q, ref, set)
		ref = q.NewPass(types.PassDescr{Target: types.TargetDescrScreen(), Depth: types.DepthDescrNone(), Load: types.LoadClear, Order: 1, Label: "composite"})
		drawSampling(q, ref, sampler, texture)
	}

	p := newPlugin()
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	set, sampler := testSet(t, k), testSet(t, k)
	frame(recordRaw(t, k), set, sampler)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
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

	sampler := testSet(t, k)
	w := recordRaw(t, k)
	target, texture := w.NewTemporaryTarget(64, 64, types.FormatRGBA8Srgb)
	ref := w.NewPass(types.PassDescr{Target: target, Depth: types.DepthDescrNone(), Load: types.LoadClear, Label: "feedback"})
	drawSampling(w, ref, sampler, texture)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
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
	// value has to be the common case. TargetDescrNone and DepthDescrNone stay reachable and
	// distinguishable, which is what a depth-only shadow pass needs.
	if (types.TargetDescr{}) != types.TargetDescrScreen() {
		t.Error("the zero target is not the screen sentinel")
	}
	if (types.DepthDescr{}) != types.DepthDescrAuto() {
		t.Error("the zero depth attachment is not DepthDescrAuto")
	}
	if types.TargetDescrNone() == types.TargetDescrScreen() || types.DepthDescrNone() == types.DepthDescrAuto() {
		t.Error("a colourless or depthless pass is indistinguishable from an unset one")
	}
}
