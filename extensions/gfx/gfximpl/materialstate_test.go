package gfximpl

import (
	"testing"

	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

func TestMaterialStateZeroValueIsTheWebGPUDefault(t *testing.T) {
	var state gpu.MaterialState
	if state.Blend != gpu.BlendAlpha {
		t.Errorf("zero Blend = %v, want BlendAlpha", state.Blend)
	}
	if state.DepthCompare != gpu.CompareAlways {
		t.Errorf("zero DepthCompare = %v, want CompareAlways", state.DepthCompare)
	}
	if state.DepthWrite {
		t.Error("zero DepthWrite = true, want false")
	}
	if state.Cull != gpu.CullNone {
		t.Errorf("zero Cull = %v, want CullNone", state.Cull)
	}
	if state.FrontFace != gpu.FrontCCW {
		t.Errorf("zero FrontFace = %v, want FrontCCW", state.FrontFace)
	}
	// The 2D overlay state is what canvas spells out by hand, which is the zero
	// value: alpha over, no depth interaction, no culling.
	if gpu.StateOverlay2D != state {
		t.Errorf("StateOverlay2D = %+v, want the zero value %+v", gpu.StateOverlay2D, state)
	}
}

func TestNamed3DStatesSpellOutTheirPasses(t *testing.T) {
	if want := (gpu.MaterialState{Blend: gpu.BlendOpaque, DepthCompare: gpu.CompareLess, DepthWrite: true, Cull: gpu.CullBack}); gpu.StateOpaque3D != want {
		t.Errorf("StateOpaque3D = %+v, want %+v", gpu.StateOpaque3D, want)
	}
	// Transparent draws test against the opaque depth but must not write, or
	// they occlude each other in draw order.
	if want := (gpu.MaterialState{Blend: gpu.BlendAlpha, DepthCompare: gpu.CompareLess}); gpu.StateTransparent3D != want {
		t.Errorf("StateTransparent3D = %+v, want %+v", gpu.StateTransparent3D, want)
	}
}

func TestPipelineDescCarriesStateAndTargetFormats(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	w := recordList(t, k)
	w.Draw(triangle(), gfx.MaterialWithState(gfx.ShaderWithText("//test"), gpu.StateOpaque3D), gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.lastPipelines) != 1 {
		t.Fatalf("pipelines created = %d, want 1", len(backend.lastPipelines))
	}
	desc := backend.lastPipelines[0]
	if desc.State != gpu.StateOpaque3D {
		t.Errorf("pipeline state = %+v, want StateOpaque3D", desc.State)
	}
	if desc.ColorFormat != gpu.FormatScreen {
		t.Errorf("pipeline colour format = %v, want FormatScreen", desc.ColorFormat)
	}
	if desc.DepthFormat != gpu.FormatDepth32F {
		t.Errorf("pipeline depth format = %v, want FormatDepth32F", desc.DepthFormat)
	}
}

func TestPipelineCacheDistinguishesDepthState(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	shader := gfx.ShaderWithText("//test")
	writing := gfx.MaterialWithState(shader, gpu.MaterialState{DepthCompare: gpu.CompareLess, DepthWrite: true})
	// Same compare, no write: the transparent pass, and a different pipeline.
	reading := gfx.MaterialWithState(shader, gpu.MaterialState{DepthCompare: gpu.CompareLess})

	w := recordList(t, k)
	w.Draw(triangle(), writing, gfx.MatParam("mvp", m.NewMat4()))
	w.Draw(triangle(), reading, gfx.MatParam("mvp", m.NewMat4()))
	w.Draw(triangle(), writing, gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.pipes != 2 {
		t.Errorf("pipelines created = %d, want 2: depth write is part of the key", backend.pipes)
	}
}
