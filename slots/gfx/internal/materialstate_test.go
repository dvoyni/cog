package internal

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx"
)

func TestMaterialStateZeroValueIsTheWebGPUDefault(t *testing.T) {
	var state gfx.MaterialState
	if state.Blend != gfx.BlendAlpha {
		t.Errorf("zero Blend = %v, want BlendAlpha", state.Blend)
	}
	if state.DepthCompare != gfx.CompareAlways {
		t.Errorf("zero DepthCompare = %v, want CompareAlways", state.DepthCompare)
	}
	if state.DepthWrite {
		t.Error("zero DepthWrite = true, want false")
	}
	if state.Cull != gfx.CullNone {
		t.Errorf("zero Cull = %v, want CullNone", state.Cull)
	}
	if state.FrontFace != gfx.FrontCCW {
		t.Errorf("zero FrontFace = %v, want FrontCCW", state.FrontFace)
	}
	// The 2D overlay state is what canvas spells out by hand, which is the zero
	// value: alpha over, no depth interaction, no culling.
	if gfx.StateOverlay2D() != state {
		t.Errorf("StateOverlay2D = %+v, want the zero value %+v", gfx.StateOverlay2D(), state)
	}
}

func TestNamed3DStatesSpellOutTheirPasses(t *testing.T) {
	if want := (gfx.MaterialState{Blend: gfx.BlendOpaque, DepthCompare: gfx.CompareLess, DepthWrite: true, Cull: gfx.CullBack}); gfx.StateOpaque3D() != want {
		t.Errorf("StateOpaque3D = %+v, want %+v", gfx.StateOpaque3D(), want)
	}
	// Transparent draws test against the opaque depth but must not write, or
	// they occlude each other in draw order.
	if want := (gfx.MaterialState{Blend: gfx.BlendAlpha, DepthCompare: gfx.CompareLess}); gfx.StateTransparent3D() != want {
		t.Errorf("StateTransparent3D = %+v, want %+v", gfx.StateTransparent3D(), want)
	}
}

func TestPipelineDescCarriesStateAndTargetFormats(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	w := recordList(t, k)
	w.Draw(triangle(), gfx.MaterialWithState(gfx.ShaderWithText("//test"), gfx.StateOpaque3D()), gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.lastPipelines) != 1 {
		t.Fatalf("pipelines created = %d, want 1", len(backend.lastPipelines))
	}
	desc := backend.lastPipelines[0]
	if desc.State != gfx.StateOpaque3D() {
		t.Errorf("pipeline state = %+v, want StateOpaque3D", desc.State)
	}
	if desc.ColorFormat != gfx.FormatScreen {
		t.Errorf("pipeline colour format = %v, want FormatScreen", desc.ColorFormat)
	}
	if desc.DepthFormat != gfx.FormatDepth32F {
		t.Errorf("pipeline depth format = %v, want FormatDepth32F", desc.DepthFormat)
	}
}

func TestPipelineCacheDistinguishesDepthState(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	shader := gfx.ShaderWithText("//test")
	writing := gfx.MaterialWithState(shader, gfx.MaterialState{DepthCompare: gfx.CompareLess, DepthWrite: true})
	// Same compare, no write: the transparent pass, and a different pipeline.
	reading := gfx.MaterialWithState(shader, gfx.MaterialState{DepthCompare: gfx.CompareLess})

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
