package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx"
	"github.com/gogpu/gputypes"
)

func TestADepthlessPipelineDeclaresNoDepthState(t *testing.T) {
	// BeginPass writes no DepthStencilAttachment for a DepthNone pass, so the
	// pipeline set into it must declare no depth state either.
	if state := depthStencilState(gfx.PipelineDesc{NoDepthTarget: true, DepthFormat: gfx.FormatDepth32F}); state != nil {
		t.Errorf("depth state = %+v, want none for a depthless pipeline", state)
	}
}

func TestADepthPipelineTakesItsFormatFromTheDescriptor(t *testing.T) {
	state := depthStencilState(gfx.PipelineDesc{
		DepthFormat: gfx.FormatDepth32F,
		State:       gfx.MaterialState{DepthCompare: gfx.CompareLessEqual, DepthWrite: true},
	})
	if state == nil {
		t.Fatal("a depth pipeline got no depth state")
	}
	if state.Format != gputypes.TextureFormatDepth32Float {
		t.Errorf("depth format = %v, want Depth32Float", state.Format)
	}
	if state.DepthCompare != compareFunc(gfx.CompareLessEqual) || !state.DepthWriteEnabled {
		t.Errorf("depth state = %+v, want the material's compare and write", state)
	}
}

func TestADepthPipelineDoesNotAssumeTheEngineDepthFormat(t *testing.T) {
	// FormatDepth32F is the engine's only depth format, so the test above
	// cannot tell a descriptor read from a constant. Any other format can.
	state := depthStencilState(gfx.PipelineDesc{DepthFormat: gfx.FormatRGBA8})
	if state == nil || state.Format != textureFormat(gfx.FormatRGBA8) {
		t.Errorf("depth state = %+v, want the descriptor's format", state)
	}
}
