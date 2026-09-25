package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/libs/m"
)

func TestMaterialStateIsReadable(t *testing.T) {
	material := descriptors.MaterialWithState(shader.ShaderWithText("a"), StateTransparent3D())
	if material.State() != StateTransparent3D() {
		t.Fatalf("State() = %+v, want %+v", material.State(), StateTransparent3D())
	}
}

// Two descriptors with the same content fingerprint the same regardless of
// which backing slice their parameters live in, which is what lets a recorder
// that builds its material inline every draw still batch those draws.
func TestFingerprintIsByContentNotByBacking(t *testing.T) {
	build := func() descriptors.MaterialDescr {
		return descriptors.MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateOpaque3D(),
			descriptors.FloatParam("roughness", 0.5),
			descriptors.ColorParam("tint", m.NewColorSrgb(1, 0.5, 0.25, 1)),
			descriptors.SamplerParam("sampler", types.SamplerDesc{AddressU: types.AddressRepeat}),
		)
	}
	if build().Fingerprint() != build().Fingerprint() {
		t.Fatal("two identical materials built separately fingerprint differently")
	}
}

func TestFingerprintDistinguishesEveryPart(t *testing.T) {
	base := func() descriptors.MaterialDescr {
		return descriptors.MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateOpaque3D(),
			descriptors.FloatParam("roughness", 0.5),
			descriptors.VecParam("offset", m.Vec4{X: 1}),
		)
	}
	variants := map[string]descriptors.MaterialDescr{
		"shader path": descriptors.MaterialWithState(shader.ShaderWithResource("other.wgsl"), StateOpaque3D(),
			descriptors.FloatParam("roughness", 0.5), descriptors.VecParam("offset", m.Vec4{X: 1})),
		"shader source kind": descriptors.MaterialWithState(shader.ShaderWithText("shader.wgsl"), StateOpaque3D(),
			descriptors.FloatParam("roughness", 0.5), descriptors.VecParam("offset", m.Vec4{X: 1})),
		"state": descriptors.MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateTransparent3D(),
			descriptors.FloatParam("roughness", 0.5), descriptors.VecParam("offset", m.Vec4{X: 1})),
		"parameter name": descriptors.MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateOpaque3D(),
			descriptors.FloatParam("metallic", 0.5), descriptors.VecParam("offset", m.Vec4{X: 1})),
		"parameter value": descriptors.MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateOpaque3D(),
			descriptors.FloatParam("roughness", 0.75), descriptors.VecParam("offset", m.Vec4{X: 1})),
		"parameter kind": descriptors.MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateOpaque3D(),
			descriptors.FloatParam("roughness", 0.5), descriptors.FloatParam("offset", 1)),
		"parameter order": descriptors.MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateOpaque3D(),
			descriptors.VecParam("offset", m.Vec4{X: 1}), descriptors.FloatParam("roughness", 0.5)),
		"parameter count": descriptors.MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateOpaque3D(),
			descriptors.FloatParam("roughness", 0.5)),
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
		a, b descriptors.MaterialDescr
	}{
		{"texture path", descriptors.Material(shaderDescr, descriptors.TextureParam("t", descriptors.TextureWithResource("a.png"))),
			descriptors.Material(shaderDescr, descriptors.TextureParam("t", descriptors.TextureWithResource("b.png")))},
		{"texture id", descriptors.Material(shaderDescr, descriptors.TextureParam("t", descriptors.BakedTexture(1, 0, 0))),
			descriptors.Material(shaderDescr, descriptors.TextureParam("t", descriptors.BakedTexture(2, 0, 0)))},
		{"buffer id", descriptors.Material(shaderDescr, descriptors.BufferParam("b", descriptors.BakedBuffer(1, 0))),
			descriptors.Material(shaderDescr, descriptors.BufferParam("b", descriptors.BakedBuffer(2, 0)))},
		{"buffer range", descriptors.Material(shaderDescr, descriptors.BufferRangeParam("b", descriptors.BakedBuffer(1, 0), 0, 256)),
			descriptors.Material(shaderDescr, descriptors.BufferRangeParam("b", descriptors.BakedBuffer(1, 0), 256, 256))},
		{"matrix", descriptors.Material(shaderDescr, descriptors.MatParam("m", m.NewMat4())),
			descriptors.Material(shaderDescr, descriptors.MatParam("m", m.Mat4{}))},
		{"sampler", descriptors.Material(shaderDescr, descriptors.SamplerParam("s", types.SamplerDesc{})),
			descriptors.Material(shaderDescr, descriptors.SamplerParam("s", types.SamplerDesc{Anisotropy: 16}))},
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
	material := descriptors.MaterialWithState(shader.ShaderWithResource("shader.wgsl"), StateOpaque3D(),
		descriptors.FloatParam("roughness", 0.5),
		descriptors.TextureParam("t", descriptors.TextureWithResource("a.png")),
		descriptors.SamplerParam("s", types.SamplerDesc{}),
	)
	if allocations := testing.AllocsPerRun(100, func() { material.Fingerprint() }); allocations != 0 {
		t.Fatalf("Fingerprint allocated %v times per call", allocations)
	}
}

// Two materials differing only in their defines must not merge into one batch,
// because one of them would then draw the other's module.
func TestFingerprintDistinguishesTheSupply(t *testing.T) {
	base := descriptors.Material(shader.ShaderWithResource("s.wgsl"), descriptors.FloatParam("roughness", 0.5))
	variants := map[string]descriptors.MaterialDescr{
		"a define":      descriptors.Material(shader.ShaderWithResource("s.wgsl", shader.ShaderDefine("SKIN")), descriptors.FloatParam("roughness", 0.5)),
		"a const":       descriptors.Material(shader.ShaderWithResource("s.wgsl", shader.ShaderConst("N", "16")), descriptors.FloatParam("roughness", 0.5)),
		"a const value": descriptors.Material(shader.ShaderWithResource("s.wgsl", shader.ShaderConst("N", "4")), descriptors.FloatParam("roughness", 0.5)),
		"a second define": descriptors.Material(shader.ShaderWithResource("s.wgsl", shader.ShaderDefine("SKIN"), shader.ShaderDefine("MORPH")),
			descriptors.FloatParam("roughness", 0.5)),
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
