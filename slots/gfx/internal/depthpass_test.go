package internal

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx"
)

// A depth-only pass is the one place in the engine where a pipeline's colour
// targets and a pass's attachments can disagree. wgpu's BeginPass already
// encodes a NoTarget() pass with no colour attachment at all, and every
// pipeline gfx built declared one anyway, so setting one into the other is a
// validation failure - and gfx drops those, so the whole frame's command buffer
// vanishes with nothing reported. These tests pin the flag that keeps the two
// in step.

func TestADrawInADepthOnlyPassBuildsAPipelineWithNoColourTarget(t *testing.T) {
	var shadow gfx.TextureDescr
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	withResourceQueue(t, k, func(resources *gfx.ResourceQueue) {
		shadow = resources.AllocateTexture(64, 64, 1, gfx.FormatDepth32F)
	})
	q := recordRaw(t, k)
	q.Pass(gfx.PassDescr{Target: gfx.NoTarget(), Depth: gfx.DepthTarget(shadow), DepthLoad: gfx.LoadClear, Label: "shadow"})
	drawInto(q)
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.lastPipelines) != 1 {
		t.Fatalf("pipelines = %d, want the one the depth pass needed", len(backend.lastPipelines))
	}
	if !backend.lastPipelines[0].NoColorTarget {
		t.Error("the depth pass's pipeline still declares a colour target")
	}
}

func TestOneShaderInAColourPassAndADepthPassBuildsTwoPipelines(t *testing.T) {
	// The flag has to be part of the pipeline cache key, not only of the
	// descriptor. A shader drawn in both kinds of pass needs two pipelines, and
	// a key that ignored the difference would hand the second pass whichever
	// one the first pass happened to build.
	var shadow gfx.TextureDescr
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	withResourceQueue(t, k, func(resources *gfx.ResourceQueue) {
		shadow = resources.AllocateTexture(64, 64, 1, gfx.FormatDepth32F)
	})
	q := recordRaw(t, k)
	q.Pass(gfx.PassDescr{Target: gfx.NoTarget(), Depth: gfx.DepthTarget(shadow), DepthLoad: gfx.LoadClear, Order: 0, Label: "shadow"})
	drawInto(q)
	q.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), Load: gfx.LoadClear, Order: 1, Label: "lit"})
	drawInto(q)
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.lastPipelines) != 2 {
		t.Fatalf("pipelines = %d, want one per pass kind", len(backend.lastPipelines))
	}
	colourless, coloured := 0, 0
	for _, desc := range backend.lastPipelines {
		if desc.NoColorTarget {
			colourless++
		} else {
			coloured++
		}
	}
	if colourless != 1 || coloured != 1 {
		t.Errorf("pipelines = %d colourless and %d coloured, want one of each", colourless, coloured)
	}
}

func TestAScreenDrawStillDeclaresTheFrameBufferAsItsColourTarget(t *testing.T) {
	// The flag is additive: an ordinary pass has to be untouched by it, and its
	// pipeline has to keep naming the frame buffer's format.
	backend, _ := passFrame(t, func(q *gfx.OpQueue) {
		q.Pass(gfx.PassDescr{Target: gfx.ScreenTarget(), Depth: gfx.DepthAuto(), Load: gfx.LoadClear, Label: "screen"})
		q.Draw(triangle(), testMaterial(), gfx.MatParam("mvp", m.NewMat4()))
	})
	if len(backend.lastPipelines) != 1 {
		t.Fatalf("pipelines = %d, want one", len(backend.lastPipelines))
	}
	desc := backend.lastPipelines[0]
	if desc.NoColorTarget {
		t.Error("a screen pass built a pipeline with no colour target")
	}
	if desc.ColorFormat != gfx.FormatScreen {
		t.Errorf("colour format = %v, want FormatScreen", desc.ColorFormat)
	}
}
