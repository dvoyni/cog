package internal

import (
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// A pipeline declares the format of the attachment it renders into, and gfx
// named the frame buffer's format there whatever the pass targeted. That was
// accidentally right while every renderable texture in the tree was allocated
// FormatRGBA8Srgb, and wrong the moment one was not: a linear target got an
// sRGB pipeline, and two targets of different formats sharing a shader,
// topology, state and layout collided on one cache entry. These tests pin the
// format to the pass.
//
// The format is asked of the backend, which is where a texture's descriptor
// lives, so a target is answerable only once the bake that allocates it has
// been replayed - which is why the tests here render the frame that allocates
// before the frame that draws.

// allocatedTarget allocates a render target and renders the frame that bakes
// it, so the backend can answer for it while the next frame is translated.
func allocatedTarget(t *testing.T, k kernel.Executioner, format TextureFormat) TextureDescr {
	t.Helper()
	var target TextureDescr
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		target = resources.AllocateRenderTarget(64, 64, 1, format)
	})
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	return target
}

// renderInto renders one frame of one pass into target, with one draw in it.
func renderInto(t *testing.T, k kernel.Executioner, target TargetDescr, label string) {
	t.Helper()
	q := recordRaw(t, k)
	q.Pass(PassDescr{Target: target, Depth: DepthNone(), Load: LoadClear, Label: label})
	drawInto(q)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
}

func TestAPassIntoALinearTargetBuildsALinearPipeline(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	target := allocatedTarget(t, k, FormatRGBA8)
	renderInto(t, k, TextureTarget(target, 0, 0), "linear")

	if len(backend.lastPipelines) != 1 {
		t.Fatalf("pipelines = %d, want the one the linear pass needed", len(backend.lastPipelines))
	}
	if got := backend.lastPipelines[0].ColorFormat; got != FormatRGBA8 {
		t.Errorf("colour format = %v, want %v: the pass renders into a linear target",
			got.Name(), FormatRGBA8.Name())
	}
}

func TestTwoTargetFormatsSharingAShaderBuildTwoPipelines(t *testing.T) {
	// colorFormat is a pipelineKey component that never varied, so two passes
	// alike in everything but their target's format were one cache entry, and
	// the second pass was handed the first pass's pipeline.
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	srgb := allocatedTarget(t, k, FormatRGBA8Srgb)
	linear := allocatedTarget(t, k, FormatRGBA8)

	q := recordRaw(t, k)
	q.Pass(PassDescr{
		Target: TextureTarget(srgb, 0, 0), Depth: DepthNone(),
		Load: LoadClear, Order: 0, Label: "srgb",
	})
	drawInto(q)
	q.Pass(PassDescr{
		Target: TextureTarget(linear, 0, 0), Depth: DepthNone(),
		Load: LoadClear, Order: 1, Label: "linear",
	})
	drawInto(q)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.lastPipelines) != 2 {
		t.Fatalf("pipelines = %d, want one per target format", len(backend.lastPipelines))
	}
	first, second := backend.lastPipelines[0].ColorFormat, backend.lastPipelines[1].ColorFormat
	if first != FormatRGBA8Srgb || second != FormatRGBA8 {
		t.Errorf("colour formats = (%v, %v), want (%v, %v)",
			first.Name(), second.Name(), FormatRGBA8Srgb.Name(), FormatRGBA8.Name())
	}
}

func TestAScreenPassAndATargetInTheFrameBufferFormatShareOnePipeline(t *testing.T) {
	// The screen sentinel is resolved where the key is built rather than
	// carried into it, so the two passes do not build byte-identical pipelines.
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	target := allocatedTarget(t, k, FrameBufferFormat)

	q := recordRaw(t, k)
	q.Pass(PassDescr{
		Target: TextureTarget(target, 0, 0), Depth: DepthNone(),
		Load: LoadClear, Order: 0, Label: "offscreen",
	})
	drawInto(q)
	q.Pass(PassDescr{
		Target: ScreenTarget(), Depth: DepthNone(),
		Load: LoadClear, Order: 1, Label: "screen",
	})
	drawInto(q)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.lastPipelines) != 1 {
		t.Fatalf("pipelines = %d, want one shared: both passes render into the frame buffer's format",
			len(backend.lastPipelines))
	}
}

func TestATargetsFirstFrameKeysTheFrameBufferAndItsNextFrameKeysItsOwn(t *testing.T) {
	// A texture is unknown to the backend until the bake that allocates it has
	// been replayed, so the frame that allocates a target cannot key its real
	// format. It falls back to what every pipeline was keyed to before, and
	// loses nothing by it: the same condition leaves TextureView with no view
	// to return, so that pass is skipped and the pipeline keyed here never
	// renders.
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	var target TextureDescr
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		target = resources.AllocateRenderTarget(64, 64, 1, FormatRGBA8)
	})
	renderInto(t, k, TextureTarget(target, 0, 0), "first")

	if len(backend.lastPipelines) != 1 {
		t.Fatalf("pipelines after the allocating frame = %d, want one", len(backend.lastPipelines))
	}
	if got := backend.lastPipelines[0].ColorFormat; got != FrameBufferFormat {
		t.Errorf("colour format = %v, want the frame buffer's: the target is not baked yet", got.Name())
	}

	renderInto(t, k, TextureTarget(target, 0, 0), "second")

	if len(backend.lastPipelines) != 2 {
		t.Fatalf("pipelines after the drawing frame = %d, want a second for the real format",
			len(backend.lastPipelines))
	}
	if got := backend.lastPipelines[1].ColorFormat; got != FormatRGBA8 {
		t.Errorf("colour format = %v, want %v once the target is baked", got.Name(), FormatRGBA8.Name())
	}
}

func TestAColourlessPassTakesNoFormatFromItsTarget(t *testing.T) {
	// A depth-only pass has no colour attachment to take a format from and is
	// keyed by noColor instead; depthpass_test.go pins what that flag does.
	var shadow TextureDescr
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		shadow = resources.AllocateTexture(64, 64, 1, FormatDepth32F)
	})
	q := recordRaw(t, k)
	q.Pass(PassDescr{
		Target: NoTarget(), Depth: DepthTarget(shadow),
		DepthLoad: LoadClear, Label: "shadow",
	})
	q.Draw(triangle(), testMaterial(), MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.lastPipelines) != 1 {
		t.Fatalf("pipelines = %d, want the one the depth pass needed", len(backend.lastPipelines))
	}
	if !backend.lastPipelines[0].NoColorTarget {
		t.Error("a colourless pass built a pipeline with a colour target")
	}
	if backend.lastPipelines[0].DepthFormat != FormatDepth32F {
		t.Errorf("depth format = %v, want the engine's one depth format",
			backend.lastPipelines[0].DepthFormat.Name())
	}
}
