package gfximpl

import (
	"testing"

	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// transitionFrame records one frame and reports the texture transitions the
// backend was asked to place, interleaved with the passes, so a test can assert
// both that a transition happened and that it happened before the pass that
// needs it.
func transitionFrame(t *testing.T, record func(*gfx.OpQueue)) *fakeBackend {
	t.Helper()
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	record(recordRaw(t, k))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	return backend
}

func TestSamplingWhatAnEarlierPassRenderedGetsABarrier(t *testing.T) {
	// gogpu tracks resources for lifetime and submit-time validation but derives
	// no barriers from that tracking, so nothing orders this sample against the
	// writes that produced it - not within one encoder, not across a submit. On
	// Vulkan the sample reads the image mid-write. The translator knows the
	// write-then-read pairs, so it is the one that places the transition.
	var sampled gfx.TextureID
	backend := transitionFrame(t, func(q *gfx.OpQueue) {
		target, texture := q.TemporaryTarget(64, 64, gfx.FormatRGBA8Srgb)
		sampled = texture.ID()
		q.Pass(gfx.PassDescr{Target: target, Depth: gfx.DepthNone(), Load: gfx.LoadClear, Order: 0, Label: "offscreen"})
		drawInto(q)
		q.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthNone(), Load: gfx.LoadClear, Order: 1, Label: "composite"})
		q.Draw(triangle(), testMaterial(gfx.TextureParam("MainTexture", texture)), gfx.MatParam("mvp", m.NewMat4()))
	})

	want := gfx.TextureTransition{Texture: sampled, From: gfx.TextureUsageRenderAttachment, To: gfx.TextureUsageTextureBinding}
	at, ok := backend.transitionBefore(want)
	if !ok {
		t.Fatalf("no %v transition placed; got %v", want, backend.transitions)
	}
	if at != 1 {
		t.Errorf("transition placed before pass %d, want before pass 1 (the sampling one)", at)
	}
}

func TestAPassThatSamplesNothingItRenderedGetsNoBarrier(t *testing.T) {
	// The barrier is not a rule about render targets, it is a rule about
	// write-then-read pairs. A frame that renders into a texture and never reads
	// it back has nothing to order, and paying for an image transition there
	// would be a cost with no hazard behind it.
	backend := transitionFrame(t, func(q *gfx.OpQueue) {
		target, _ := q.TemporaryTarget(64, 64, gfx.FormatRGBA8Srgb)
		q.Pass(gfx.PassDescr{Target: target, Depth: gfx.DepthNone(), Load: gfx.LoadClear, Order: 0, Label: "offscreen"})
		drawInto(q)
		q.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthNone(), Load: gfx.LoadClear, Order: 1, Label: "screen"})
		drawInto(q)
	})

	if len(backend.transitions) != 0 {
		t.Errorf("transitions = %v, want none", backend.transitions)
	}
}

func TestRenderingIntoATextureAnEarlierPassSampledGetsTheReverseBarrier(t *testing.T) {
	// The ping-pong a post-processing chain is made of. The hazard is symmetric:
	// a pass that writes what an earlier pass read has to wait for those reads,
	// and the declared old usage has to be the one the texture is actually in or
	// the layout transition is a lie.
	var pong gfx.TextureID
	backend := transitionFrame(t, func(q *gfx.OpQueue) {
		targetA, textureA := q.TemporaryTarget(64, 64, gfx.FormatRGBA8Srgb)
		targetB, textureB := q.TemporaryTarget(32, 32, gfx.FormatRGBA8Srgb)
		pong = textureB.ID()

		// B is written, then read, then written again.
		q.Pass(gfx.PassDescr{Target: targetB, Depth: gfx.DepthNone(), Load: gfx.LoadClear, Order: 0, Label: "b-first"})
		drawInto(q)
		q.Pass(gfx.PassDescr{Target: targetA, Depth: gfx.DepthNone(), Load: gfx.LoadClear, Order: 1, Label: "a-reads-b"})
		q.Draw(triangle(), testMaterial(gfx.TextureParam("MainTexture", textureB)), gfx.MatParam("mvp", m.NewMat4()))
		q.Pass(gfx.PassDescr{Target: targetB, Depth: gfx.DepthNone(), Load: gfx.LoadClear, Order: 2, Label: "b-again"})
		q.Draw(triangle(), testMaterial(gfx.TextureParam("MainTexture", textureA)), gfx.MatParam("mvp", m.NewMat4()))
	})

	forward := gfx.TextureTransition{Texture: pong, From: gfx.TextureUsageRenderAttachment, To: gfx.TextureUsageTextureBinding}
	if at, ok := backend.transitionBefore(forward); !ok || at != 1 {
		t.Errorf("forward transition at pass %d (ok %v), want before pass 1; got %v", at, ok, backend.transitions)
	}
	reverse := gfx.TextureTransition{Texture: pong, From: gfx.TextureUsageTextureBinding, To: gfx.TextureUsageRenderAttachment}
	if at, ok := backend.transitionBefore(reverse); !ok || at != 2 {
		t.Errorf("reverse transition at pass %d (ok %v), want before pass 2; got %v", at, ok, backend.transitions)
	}
}

func TestATextureIsTransitionedOncePerPassThatNeedsIt(t *testing.T) {
	// Two draws in one pass sampling the same render target is one hazard, not
	// two, and a duplicate barrier is a real pipeline stall rather than a
	// bookkeeping wart.
	backend := transitionFrame(t, func(q *gfx.OpQueue) {
		target, texture := q.TemporaryTarget(64, 64, gfx.FormatRGBA8Srgb)
		q.Pass(gfx.PassDescr{Target: target, Depth: gfx.DepthNone(), Load: gfx.LoadClear, Order: 0, Label: "offscreen"})
		drawInto(q)
		q.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthNone(), Load: gfx.LoadClear, Order: 1, Label: "composite"})
		q.Draw(triangle(), testMaterial(gfx.TextureParam("MainTexture", texture)), gfx.MatParam("mvp", m.NewMat4()))
		q.Draw(triangle(), testMaterial(gfx.TextureParam("MainTexture", texture)), gfx.MatParam("mvp", m.NewMat4()))
	})

	if len(backend.transitions) != 1 {
		t.Errorf("transitions = %v, want exactly one", backend.transitions)
	}
}

func TestADepthAttachmentSampledLaterGetsABarrier(t *testing.T) {
	// A shadow map is a depth attachment first and a texture second, so the
	// write-then-read pair has to be found on the depth attachment too and not
	// only on the colour one.
	var shadow gfx.TextureDescr
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	withResourceQueue(t, k, func(resources *gfx.ResourceQueue) {
		shadow = resources.AllocateTexture(64, 64, 1, gfx.FormatDepth32F)
	})
	q := recordRaw(t, k)
	q.Pass(gfx.PassDescr{Target: gfx.NoTarget(), Depth: gfx.DepthTarget(shadow), DepthLoad: gfx.LoadClear, Order: 0, Label: "shadow"})
	drawInto(q)
	q.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthNone(), Load: gfx.LoadClear, Order: 1, Label: "lit"})
	q.Draw(triangle(), testMaterial(gfx.TextureParam("MainTexture", shadow)), gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	want := gfx.TextureTransition{Texture: shadow.ID(), From: gfx.TextureUsageRenderAttachment, To: gfx.TextureUsageTextureBinding}
	if _, ok := backend.transitionBefore(want); !ok {
		t.Errorf("no depth transition placed; got %v", backend.transitions)
	}
}

func TestATextureStaysTransitionedAcrossConsecutivePassesThatSampleIt(t *testing.T) {
	// Once a texture is readable it stays readable until something writes it
	// again. A second barrier saying TextureBinding -> TextureBinding orders
	// nothing and costs a real pipeline stall, so the usage the frame left the
	// texture in is tracked rather than re-asserted per pass.
	backend := transitionFrame(t, func(q *gfx.OpQueue) {
		source, texture := q.TemporaryTarget(64, 64, gfx.FormatRGBA8Srgb)
		other, _ := q.TemporaryTarget(32, 32, gfx.FormatRGBA8Srgb)
		q.Pass(gfx.PassDescr{Target: source, Depth: gfx.DepthNone(), Load: gfx.LoadClear, Order: 0, Label: "offscreen"})
		drawInto(q)
		q.Pass(gfx.PassDescr{Target: other, Depth: gfx.DepthNone(), Load: gfx.LoadClear, Order: 1, Label: "reads-once"})
		q.Draw(triangle(), testMaterial(gfx.TextureParam("MainTexture", texture)), gfx.MatParam("mvp", m.NewMat4()))
		q.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthNone(), Load: gfx.LoadClear, Order: 2, Label: "reads-again"})
		q.Draw(triangle(), testMaterial(gfx.TextureParam("MainTexture", texture)), gfx.MatParam("mvp", m.NewMat4()))
	})

	if len(backend.transitions) != 1 {
		t.Errorf("transitions = %v, want exactly one for the two passes that read it", backend.transitions)
	}
}

func TestNoPassIsHandedAnEmptyBarrierList(t *testing.T) {
	// The sink contract says TransitionTextures is never called with nothing to
	// do, so a backend can treat the call itself as the signal.
	backend := transitionFrame(t, func(q *gfx.OpQueue) {
		q.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthNone(), Load: gfx.LoadClear, Label: "screen"})
		drawInto(q)
	})
	if backend.emptyTransitions != 0 {
		t.Errorf("empty TransitionTextures calls = %d, want 0", backend.emptyTransitions)
	}
}

func TestAnOrdinaryTextureIsNeverTransitioned(t *testing.T) {
	// Almost every texture a frame samples was uploaded, not rendered into, and
	// has no writes this frame to order against. Emitting a barrier for one
	// would declare RenderAttachment as its old usage, which it never was - and
	// a layout transition naming the wrong old layout is undefined behaviour,
	// not a wasted instruction.
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	var uploaded gfx.TextureDescr
	withResourceQueue(t, k, func(resources *gfx.ResourceQueue) {
		uploaded = resources.AllocateTexture(8, 8, 1, gfx.FormatRGBA8Srgb)
	})
	q := recordRaw(t, k)
	q.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthNone(), Load: gfx.LoadClear, Label: "screen"})
	q.Draw(triangle(), testMaterial(gfx.TextureParam("MainTexture", uploaded)), gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.transitions) != 0 {
		t.Errorf("transitions = %v, want none for a texture nothing rendered into", backend.transitions)
	}
}
