package gfx

import (
	"testing"

	"github.com/dvoyni/cog/m"
)

func TestMaterialStateIsReadable(t *testing.T) {
	material := MaterialWithState(ShaderWithText("a"), StateTransparent3D)
	if material.State() != StateTransparent3D {
		t.Fatalf("State() = %+v, want %+v", material.State(), StateTransparent3D)
	}
}

// Two descriptors with the same content fingerprint the same regardless of
// which backing slice their parameters live in, which is what lets a recorder
// that builds its material inline every draw still batch those draws.
func TestFingerprintIsByContentNotByBacking(t *testing.T) {
	build := func() MaterialDescr {
		return MaterialWithState(ShaderWithResource("shader.wgsl"), StateOpaque3D,
			FloatParam("roughness", 0.5),
			ColorParam("tint", m.NewColorSrgb(1, 0.5, 0.25, 1)),
			SamplerParam("sampler", SamplerDesc{AddressU: AddressRepeat}),
		)
	}
	if build().Fingerprint() != build().Fingerprint() {
		t.Fatal("two identical materials built separately fingerprint differently")
	}
}

func TestFingerprintDistinguishesEveryPart(t *testing.T) {
	base := func() MaterialDescr {
		return MaterialWithState(ShaderWithResource("shader.wgsl"), StateOpaque3D,
			FloatParam("roughness", 0.5),
			VecParam("offset", m.Vec4{X: 1}),
		)
	}
	variants := map[string]MaterialDescr{
		"shader path": MaterialWithState(ShaderWithResource("other.wgsl"), StateOpaque3D,
			FloatParam("roughness", 0.5), VecParam("offset", m.Vec4{X: 1})),
		"shader source kind": MaterialWithState(ShaderWithText("shader.wgsl"), StateOpaque3D,
			FloatParam("roughness", 0.5), VecParam("offset", m.Vec4{X: 1})),
		"state": MaterialWithState(ShaderWithResource("shader.wgsl"), StateTransparent3D,
			FloatParam("roughness", 0.5), VecParam("offset", m.Vec4{X: 1})),
		"parameter name": MaterialWithState(ShaderWithResource("shader.wgsl"), StateOpaque3D,
			FloatParam("metallic", 0.5), VecParam("offset", m.Vec4{X: 1})),
		"parameter value": MaterialWithState(ShaderWithResource("shader.wgsl"), StateOpaque3D,
			FloatParam("roughness", 0.75), VecParam("offset", m.Vec4{X: 1})),
		"parameter kind": MaterialWithState(ShaderWithResource("shader.wgsl"), StateOpaque3D,
			FloatParam("roughness", 0.5), FloatParam("offset", 1)),
		"parameter order": MaterialWithState(ShaderWithResource("shader.wgsl"), StateOpaque3D,
			VecParam("offset", m.Vec4{X: 1}), FloatParam("roughness", 0.5)),
		"parameter count": MaterialWithState(ShaderWithResource("shader.wgsl"), StateOpaque3D,
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
	shader := ShaderWithResource("shader.wgsl")
	pairs := []struct {
		name string
		a, b MaterialDescr
	}{
		{"texture path", Material(shader, TextureParam("t", TextureWithResource("a.png"))),
			Material(shader, TextureParam("t", TextureWithResource("b.png")))},
		{"texture id", Material(shader, TextureParam("t", TextureDescr{id: 1})),
			Material(shader, TextureParam("t", TextureDescr{id: 2}))},
		{"buffer id", Material(shader, BufferParam("b", BufferDescr{source: BufferSourceBaked, id: 1})),
			Material(shader, BufferParam("b", BufferDescr{source: BufferSourceBaked, id: 2}))},
		{"buffer range", Material(shader, BufferRangeParam("b", BufferDescr{source: BufferSourceBaked, id: 1}, 0, 256)),
			Material(shader, BufferRangeParam("b", BufferDescr{source: BufferSourceBaked, id: 1}, 256, 256))},
		{"matrix", Material(shader, MatParam("m", m.NewMat4())),
			Material(shader, MatParam("m", m.Mat4{}))},
		{"sampler", Material(shader, SamplerParam("s", SamplerDesc{})),
			Material(shader, SamplerParam("s", SamplerDesc{Anisotropy: 16}))},
	}
	for _, pair := range pairs {
		if pair.a.Fingerprint() == pair.b.Fingerprint() {
			t.Errorf("two materials differing in a %s parameter fingerprint the same", pair.name)
		}
	}
}

func TestFingerprintAllocatesNothing(t *testing.T) {
	material := MaterialWithState(ShaderWithResource("shader.wgsl"), StateOpaque3D,
		FloatParam("roughness", 0.5),
		TextureParam("t", TextureWithResource("a.png")),
		SamplerParam("s", SamplerDesc{}),
	)
	if allocations := testing.AllocsPerRun(100, func() { material.Fingerprint() }); allocations != 0 {
		t.Fatalf("Fingerprint allocated %v times per call", allocations)
	}
}
