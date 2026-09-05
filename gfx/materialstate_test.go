package gfx

import (
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/m"
)

func TestMaterialStateZeroValueIsTheWebGPUDefault(t *testing.T) {
	var state MaterialState
	if state.Blend != BlendAlpha {
		t.Errorf("zero Blend = %v, want BlendAlpha", state.Blend)
	}
	if state.DepthCompare != CompareAlways {
		t.Errorf("zero DepthCompare = %v, want CompareAlways", state.DepthCompare)
	}
	if state.DepthWrite {
		t.Error("zero DepthWrite = true, want false")
	}
	if state.Cull != CullNone {
		t.Errorf("zero Cull = %v, want CullNone", state.Cull)
	}
	if state.FrontFace != FrontCCW {
		t.Errorf("zero FrontFace = %v, want FrontCCW", state.FrontFace)
	}
	// The 2D overlay state is what canvas spells out by hand, which is the zero
	// value: alpha over, no depth interaction, no culling.
	if StateOverlay2D != state {
		t.Errorf("StateOverlay2D = %+v, want the zero value %+v", StateOverlay2D, state)
	}
}

func TestNamed3DStatesSpellOutTheirPasses(t *testing.T) {
	if want := (MaterialState{Blend: BlendOpaque, DepthCompare: CompareLess, DepthWrite: true, Cull: CullBack}); StateOpaque3D != want {
		t.Errorf("StateOpaque3D = %+v, want %+v", StateOpaque3D, want)
	}
	// Transparent draws test against the opaque depth but must not write, or
	// they occlude each other in draw order.
	if want := (MaterialState{Blend: BlendAlpha, DepthCompare: CompareLess}); StateTransparent3D != want {
		t.Errorf("StateTransparent3D = %+v, want %+v", StateTransparent3D, want)
	}
}

func TestPipelineDescCarriesStateAndTargetFormats(t *testing.T) {
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	w := recordList(t, k)
	w.Draw(triangle(), MaterialWithState(ShaderWithText("//test"), StateOpaque3D), MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.lastPipelines) != 1 {
		t.Fatalf("pipelines created = %d, want 1", len(backend.lastPipelines))
	}
	desc := backend.lastPipelines[0]
	if desc.State != StateOpaque3D {
		t.Errorf("pipeline state = %+v, want StateOpaque3D", desc.State)
	}
	if desc.ColorFormat != FormatScreen {
		t.Errorf("pipeline colour format = %v, want FormatScreen", desc.ColorFormat)
	}
	if desc.DepthFormat != FormatDepth32F {
		t.Errorf("pipeline depth format = %v, want FormatDepth32F", desc.DepthFormat)
	}
}

func TestPipelineCacheDistinguishesDepthState(t *testing.T) {
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	shader := ShaderWithText("//test")
	writing := MaterialWithState(shader, MaterialState{DepthCompare: CompareLess, DepthWrite: true})
	// Same compare, no write: the transparent pass, and a different pipeline.
	reading := MaterialWithState(shader, MaterialState{DepthCompare: CompareLess})

	w := recordList(t, k)
	w.Draw(triangle(), writing, MatParam("mvp", m.NewMat4()))
	w.Draw(triangle(), reading, MatParam("mvp", m.NewMat4()))
	w.Draw(triangle(), writing, MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.pipes != 2 {
		t.Errorf("pipelines created = %d, want 2: depth write is part of the key", backend.pipes)
	}
}
