package internal

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

func TestFingerprintParamsSeesTextureBufferMatrixAndSamplerParameters(t *testing.T) {
	pairs := []struct {
		name string
		a, b types.ShaderParameterDescr
	}{
		{"texture path", types.ShaderParameterTexture("t", types.TextureWithResource("a.png")),
			types.ShaderParameterTexture("t", types.TextureWithResource("b.png"))},
		{"texture id", types.ShaderParameterTexture("t", types.BakedTexture(1, 0, 0)),
			types.ShaderParameterTexture("t", types.BakedTexture(2, 0, 0))},
		{"buffer id", types.ShaderParameterBuffer("b", types.BufferDescrWithId(1, 0)),
			types.ShaderParameterBuffer("b", types.BufferDescrWithId(2, 0))},
		{"buffer range", types.ShaderParameterBufferRange("b", types.BufferDescrWithId(1, 0), 0, 256),
			types.ShaderParameterBufferRange("b", types.BufferDescrWithId(1, 0), 256, 256)},
		{"matrix", types.ShaderParameterMat4("m", m.NewMat4()), types.ShaderParameterMat4("m", m.Mat4{})},
		{"sampler", types.ShaderParameterSampler("s", types.SamplerDesc{}),
			types.ShaderParameterSampler("s", types.SamplerDesc{Anisotropy: 16})},
	}
	for _, pair := range pairs {
		a := FingerprintParams([]types.ShaderParameterDescr{pair.a})
		b := FingerprintParams([]types.ShaderParameterDescr{pair.b})
		if a == b {
			t.Errorf("two slices differing in a %s parameter fingerprint the same", pair.name)
		}
	}
}

func TestFingerprintParamsAllocatesNothing(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	params := []types.ShaderParameterDescr{
		types.ShaderParameterFloat("roughness", 0.5),
		types.ShaderParameterTexture("t", types.TextureWithResource("a.png")),
		types.ShaderParameterSampler("s", types.SamplerDesc{}),
	}
	if allocations := testing.AllocsPerRun(100, func() { FingerprintParams(params) }); allocations != 0 {
		t.Fatalf("FingerprintParams allocated %v times per call", allocations)
	}
}
