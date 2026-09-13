package wgpu

import (
	"testing"

	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/gogpu/gputypes"
)

func TestGfxBlendStates(t *testing.T) {
	if got := gfxBlendState(gpu.BlendOpaque); got != nil {
		t.Fatalf("opaque blend = %+v, want nil", got)
	}
	additive := gfxBlendState(gpu.BlendAdditive)
	if additive == nil || additive.Color.SrcFactor != gputypes.BlendFactorSrcAlpha || additive.Color.DstFactor != gputypes.BlendFactorOne {
		t.Fatalf("additive blend = %+v", additive)
	}
	multiply := gfxBlendState(gpu.BlendMultiply)
	if multiply == nil || multiply.Color.SrcFactor != gputypes.BlendFactorDst || multiply.Color.DstFactor != gputypes.BlendFactorZero {
		t.Fatalf("multiply blend = %+v", multiply)
	}
}
