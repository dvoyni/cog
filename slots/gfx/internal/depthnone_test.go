package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// The depth half of depthpass_test.go. A DepthNone() pass has no depth
// attachment - gogpu's BeginPass writes none - and a pipeline that declares a
// depth state inside it is rejected at setPipeline by browser WebGPU, which
// takes the frame's whole command buffer with it. Native Dawn does not enforce
// the match, which is why nothing had reported it. These tests pin the flag
// that keeps the two in step.

func TestADrawInADepthNonePassBuildsAPipelineWithNoDepthTarget(t *testing.T) {
	backend, _ := passFrame(t, func(q *OpQueue) {
		q.Pass(descriptors.PassDescr{Target: descriptors.ScreenTarget(), Depth: descriptors.DepthNone(), Load: types.LoadClear, Label: "flat"})
		drawInto(q)
	})
	if len(backend.lastPipelines) != 1 {
		t.Fatalf("pipelines = %d, want the one the flat pass needed", len(backend.lastPipelines))
	}
	if !backend.lastPipelines[0].NoDepthTarget {
		t.Error("the flat pass's pipeline still declares a depth target")
	}
}

func TestOneShaderInADepthPassAndADepthNonePassBuildsTwoPipelines(t *testing.T) {
	// As with noColor, the flag has to be in the cache key: a key that ignored
	// it would hand the second pass whichever pipeline the first one built.
	backend, _ := passFrame(t, func(q *OpQueue) {
		q.Pass(descriptors.PassDescr{Target: descriptors.ScreenTarget(), Depth: descriptors.DepthAuto(), Load: types.LoadClear, Order: 0, Label: "lit"})
		drawInto(q)
		q.Pass(descriptors.PassDescr{Target: descriptors.ScreenTarget(), Depth: descriptors.DepthNone(), Order: 1, Label: "flat"})
		drawInto(q)
	})
	if len(backend.lastPipelines) != 2 {
		t.Fatalf("pipelines = %d, want one per depth kind", len(backend.lastPipelines))
	}
	depthless, depthed := 0, 0
	for _, desc := range backend.lastPipelines {
		if desc.NoDepthTarget {
			depthless++
		} else {
			depthed++
		}
	}
	if depthless != 1 || depthed != 1 {
		t.Errorf("pipelines = %d depthless and %d with depth, want one of each", depthless, depthed)
	}
}

func TestADrawInADepthAutoPassKeepsItsDepthTarget(t *testing.T) {
	backend, _ := passFrame(t, func(q *OpQueue) {
		q.Pass(descriptors.PassDescr{Target: descriptors.ScreenTarget(), Depth: descriptors.DepthAuto(), Load: types.LoadClear, Label: "lit"})
		q.Draw(triangle(), testMaterial(), descriptors.MatParam("mvp", m.NewMat4()))
	})
	if len(backend.lastPipelines) != 1 {
		t.Fatalf("pipelines = %d, want one", len(backend.lastPipelines))
	}
	desc := backend.lastPipelines[0]
	if desc.NoDepthTarget {
		t.Error("a DepthAuto pass built a pipeline with no depth target")
	}
	if desc.DepthFormat != descriptors.FormatDepth32F {
		t.Errorf("depth format = %v, want the engine's one depth format", desc.DepthFormat.String())
	}
}

func TestADrawInADepthTargetPassKeepsItsDepthTarget(t *testing.T) {
	var shadow descriptors.TextureDescr
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		shadow = resources.NewTexture(64, 64, 1, descriptors.FormatDepth32F, false)
	})
	q := recordRaw(t, k)
	q.Pass(descriptors.PassDescr{Target: descriptors.NoTarget(), Depth: descriptors.DepthTarget(shadow), DepthLoad: types.LoadClear, Label: "shadow"})
	drawInto(q)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.lastPipelines) != 1 {
		t.Fatalf("pipelines = %d, want the one the depth pass needed", len(backend.lastPipelines))
	}
	desc := backend.lastPipelines[0]
	if desc.NoDepthTarget {
		t.Error("a DepthTarget pass built a pipeline with no depth target")
	}
	if desc.DepthFormat != descriptors.FormatDepth32F {
		t.Errorf("depth format = %v, want the engine's one depth format", desc.DepthFormat.String())
	}
}
