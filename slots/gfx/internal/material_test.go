package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/libs/m"
)

func TestMaterialStateIsReadable(t *testing.T) {
	material := MaterialWithState(shader.ShaderWithText("a"), StateTransparent3D())
	if material.State() != StateTransparent3D() {
		t.Fatalf("State() = %+v, want %+v", material.State(), StateTransparent3D())
	}
}

// Two descriptors with the same content fingerprint the same regardless of
// which backing slice their parameters live in, which is what lets a recorder
// that builds its material inline every draw still batch those draws.
func TestFingerprintIsByContentNotByBacking(t *testing.T) {
	build := func() MaterialDescr {
		return MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateOpaque3D(),
			FloatParam("roughness", 0.5),
			ColorParam("tint", m.NewColorSrgb(1, 0.5, 0.25, 1)),
			SamplerParam("sampler", types.SamplerDesc{AddressU: types.AddressRepeat}),
		)
	}
	if build().Fingerprint() != build().Fingerprint() {
		t.Fatal("two identical materials built separately fingerprint differently")
	}
}

func TestFingerprintDistinguishesEveryPart(t *testing.T) {
	base := func() MaterialDescr {
		return MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateOpaque3D(),
			FloatParam("roughness", 0.5),
			VecParam("offset", m.Vec4{X: 1}),
		)
	}
	variants := map[string]MaterialDescr{
		"shader path": MaterialWithState(shader.ShaderWithResource("other.wgsl"), StateOpaque3D(),
			FloatParam("roughness", 0.5), VecParam("offset", m.Vec4{X: 1})),
		"shader source kind": MaterialWithState(shader.ShaderWithText("shader.wgsl"), StateOpaque3D(),
			FloatParam("roughness", 0.5), VecParam("offset", m.Vec4{X: 1})),
		"state": MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateTransparent3D(),
			FloatParam("roughness", 0.5), VecParam("offset", m.Vec4{X: 1})),
		"parameter name": MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateOpaque3D(),
			FloatParam("metallic", 0.5), VecParam("offset", m.Vec4{X: 1})),
		"parameter value": MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateOpaque3D(),
			FloatParam("roughness", 0.75), VecParam("offset", m.Vec4{X: 1})),
		"parameter kind": MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateOpaque3D(),
			FloatParam("roughness", 0.5), FloatParam("offset", 1)),
		"parameter order": MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateOpaque3D(),
			VecParam("offset", m.Vec4{X: 1}), FloatParam("roughness", 0.5)),
		"parameter count": MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateOpaque3D(),
			FloatParam("roughness", 0.5)),
	}
	want := base().Fingerprint()
	for name, variant := range variants {
		if variant.Fingerprint() == want {
			t.Errorf("a material differing only in its %s fingerprints the same as the base", name)
		}
	}
}

func TestFingerprintSeesTextureBufferAndMatrixParameters(t *testing.T) {
	shaderDescr := shader.ShaderWithResource("shader.wgsl")
	pairs := []struct {
		name string
		a, b MaterialDescr
	}{
		{"texture path", Material(shaderDescr, TextureParam("t", TextureWithResource("a.png"))),
			Material(shaderDescr, TextureParam("t", TextureWithResource("b.png")))},
		{"texture id", Material(shaderDescr, TextureParam("t", BakedTexture(1, 0, 0))),
			Material(shaderDescr, TextureParam("t", BakedTexture(2, 0, 0)))},
		{"buffer id", Material(shaderDescr, BufferParam("b", BufferDescr{source: BufferSourceBaked, id: 1})),
			Material(shaderDescr, BufferParam("b", BufferDescr{source: BufferSourceBaked, id: 2}))},
		{"buffer range", Material(shaderDescr, BufferRangeParam("b", BufferDescr{source: BufferSourceBaked, id: 1}, 0, 256)),
			Material(shaderDescr, BufferRangeParam("b", BufferDescr{source: BufferSourceBaked, id: 1}, 256, 256))},
		{"matrix", Material(shaderDescr, MatParam("m", m.NewMat4())),
			Material(shaderDescr, MatParam("m", m.Mat4{}))},
		{"sampler", Material(shaderDescr, SamplerParam("s", types.SamplerDesc{})),
			Material(shaderDescr, SamplerParam("s", types.SamplerDesc{Anisotropy: 16}))},
	}
	for _, pair := range pairs {
		if pair.a.Fingerprint() == pair.b.Fingerprint() {
			t.Errorf("two materials differing in a %s parameter fingerprint the same", pair.name)
		}
	}
}

func TestFingerprintAllocatesNothing(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	material := MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateOpaque3D(),
		FloatParam("roughness", 0.5),
		TextureParam("t", TextureWithResource("a.png")),
		SamplerParam("s", types.SamplerDesc{}),
	)
	if allocations := testing.AllocsPerRun(100, func() { material.Fingerprint() }); allocations != 0 {
		t.Fatalf("Fingerprint allocated %v times per call", allocations)
	}
}

// Two materials differing only in their defines must not merge into one batch,
// because one of them would then draw the other's module.
func TestFingerprintDistinguishesTheSupply(t *testing.T) {
	base := Material(shader.ShaderWithResource("s.wgsl"), FloatParam("roughness", 0.5))
	variants := map[string]MaterialDescr{
		"a define":      Material(shader.ShaderWithResource("s.wgsl", shader.ShaderDefine("SKIN")), FloatParam("roughness", 0.5)),
		"a const":       Material(shader.ShaderWithResource("s.wgsl", shader.ShaderConst("N", "16")), FloatParam("roughness", 0.5)),
		"a const value": Material(shader.ShaderWithResource("s.wgsl", shader.ShaderConst("N", "4")), FloatParam("roughness", 0.5)),
		"a second define": Material(shader.ShaderWithResource("s.wgsl", shader.ShaderDefine("SKIN"), shader.ShaderDefine("MORPH")),
			FloatParam("roughness", 0.5)),
	}
	seen := map[uint64]string{base.Fingerprint(): "no supply"}
	for name, variant := range variants {
		fingerprint := variant.Fingerprint()
		if other, ok := seen[fingerprint]; ok {
			t.Errorf("%s fingerprints the same as %s", name, other)
			continue
		}
		seen[fingerprint] = name
	}
}
