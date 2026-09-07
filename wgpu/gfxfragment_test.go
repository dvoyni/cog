package wgpu

import (
	"testing"

	cgfx "github.com/dvoyni/cog/gfx"
	"github.com/gogpu/gputypes"
)

func TestADepthOnlyPipelineDeclaresNoFragmentStage(t *testing.T) {
	// BeginPass already encodes a NoTarget() pass with an empty
	// ColorAttachments, so the pipeline set into it must declare no target
	// either. Returning nil here rather than an empty Targets slice is what
	// also lets a depth-only shader carry no fs_main at all: there is no
	// entry point to name.
	if state := fragmentState(nil, cgfx.PipelineDesc{NoColorTarget: true}); state != nil {
		t.Errorf("fragment state = %+v, want none for a depth-only pipeline", state)
	}
}

func TestAColourPipelineDeclaresOneTargetInTheFrameBufferFormat(t *testing.T) {
	state := fragmentState(nil, cgfx.PipelineDesc{ColorFormat: cgfx.FormatScreen})
	if state == nil {
		t.Fatal("a colour pipeline got no fragment state")
	}
	if state.EntryPoint != "fs_main" {
		t.Errorf("entry point = %q, want fs_main", state.EntryPoint)
	}
	if len(state.Targets) != 1 {
		t.Fatalf("targets = %d, want one", len(state.Targets))
	}
	if state.Targets[0].Format != textureFormat(cgfx.FrameBufferFormat) {
		t.Errorf("target format = %v, want the frame buffer's", state.Targets[0].Format)
	}
	if state.Targets[0].WriteMask != gputypes.ColorWriteMaskAll {
		t.Errorf("write mask = %v, want every channel", state.Targets[0].WriteMask)
	}
}

func TestABlendedPipelineKeepsItsBlendStateOnTheTarget(t *testing.T) {
	// The blend mode rides on the colour target, so dropping the fragment
	// state for a depth pass must not be the same code path that carries it.
	state := fragmentState(nil, cgfx.PipelineDesc{
		ColorFormat: cgfx.FormatScreen,
		State:       cgfx.MaterialState{Blend: cgfx.BlendAlpha},
	})
	if state == nil || len(state.Targets) != 1 || state.Targets[0].Blend == nil {
		t.Fatalf("an alpha-blended pipeline lost its blend state: %+v", state)
	}
}
