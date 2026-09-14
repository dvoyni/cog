package types

import (
	"testing"

	"github.com/dvoyni/cog/extensions/gfx/gpu"
)

// glTF's alphaMode and doubleSided are the only things that change the bundled
// PBR's fixed-function state, and MASK is fixed-function-identical to OPAQUE:
// it writes depth and batches with the opaque geometry, because the cutoff is
// entirely a fragment-shader concern.
func TestAlphaModeAndDoubleSidedMapOntoPipelineState(t *testing.T) {
	opaque := PbrState(AlphaOpaque, false)
	if opaque != (gpu.MaterialState{
		Blend: gpu.BlendOpaque, DepthCompare: gpu.CompareLess, DepthWrite: true, Cull: gpu.CullBack,
	}) {
		t.Fatalf("an opaque single-sided material is %+v, want the opaque 3D state culling back faces", opaque)
	}
	if mask := PbrState(AlphaMask, false); mask != opaque {
		t.Fatalf("a MASK material is %+v, want the opaque state exactly: %+v", mask, opaque)
	}
	blend := PbrState(AlphaBlend, false)
	if blend.Blend != gpu.BlendAlpha || blend.DepthWrite {
		t.Fatalf("a BLEND material is %+v, want alpha blending without depth writes", blend)
	}
	if double := PbrState(AlphaOpaque, true); double.Cull != gpu.CullNone {
		t.Fatalf("a double-sided material culls %v, want CullNone", double.Cull)
	}
}
