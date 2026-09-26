package internal

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

func TestFingerprintParamsSeesTextureBufferMatrixAndSamplerParameters(t *testing.T) {
	pairs := []struct {
		name string
		a, b descriptors.ParameterDescr
	}{
		{"texture path", descriptors.TextureParam("t", descriptors.TextureWithResource("a.png")),
			descriptors.TextureParam("t", descriptors.TextureWithResource("b.png"))},
		{"texture id", descriptors.TextureParam("t", descriptors.BakedTexture(1, 0, 0)),
			descriptors.TextureParam("t", descriptors.BakedTexture(2, 0, 0))},
		{"buffer id", descriptors.BufferParam("b", descriptors.BakedBuffer(1, 0)),
			descriptors.BufferParam("b", descriptors.BakedBuffer(2, 0))},
		{"buffer range", descriptors.BufferRangeParam("b", descriptors.BakedBuffer(1, 0), 0, 256),
			descriptors.BufferRangeParam("b", descriptors.BakedBuffer(1, 0), 256, 256)},
		{"matrix", descriptors.MatParam("m", m.NewMat4()), descriptors.MatParam("m", m.Mat4{})},
		{"sampler", descriptors.SamplerParam("s", types.SamplerDesc{}),
			descriptors.SamplerParam("s", types.SamplerDesc{Anisotropy: 16})},
	}
	for _, pair := range pairs {
		a := descriptors.FingerprintParams([]descriptors.ParameterDescr{pair.a})
		b := descriptors.FingerprintParams([]descriptors.ParameterDescr{pair.b})
		if a == b {
			t.Errorf("two slices differing in a %s parameter fingerprint the same", pair.name)
		}
	}
}

func TestFingerprintParamsAllocatesNothing(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	params := []descriptors.ParameterDescr{
		descriptors.FloatParam("roughness", 0.5),
		descriptors.TextureParam("t", descriptors.TextureWithResource("a.png")),
		descriptors.SamplerParam("s", types.SamplerDesc{}),
	}
	if allocations := testing.AllocsPerRun(100, func() { descriptors.FingerprintParams(params) }); allocations != 0 {
		t.Fatalf("FingerprintParams allocated %v times per call", allocations)
	}
}
