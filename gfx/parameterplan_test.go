package gfx

import (
	"testing"

	"github.com/dvoyni/cog/m"
)

func TestPreparedParameterPlanReusesShapeAndReadsCurrentValues(t *testing.T) {
	translator := newTranslator()
	layout := ShaderLayout{
		UniformSize: 16,
		Uniforms:    []UniformMember{{Name: "value", Offset: 0}},
		Resources:   []ShaderResource{{Name: "texture", Group: 1, Binding: 0}},
	}
	material := []ParameterDescr{TextureParam("texture", TextureDescr{id: 1})}
	draw := []ParameterDescr{FloatParam("value", 1)}
	first := translator.prepareParameterPlan(1, "test", layout, material, draw)

	material = []ParameterDescr{TextureParam("texture", TextureDescr{id: 2})}
	draw = []ParameterDescr{FloatParam("value", 9)}
	second := translator.prepareParameterPlan(1, "test", layout, material, draw)
	if second != first {
		t.Fatal("identical parameter shape did not reuse its prepared plan")
	}
	if got := second.uniforms[0].param.value(material, draw).num; got != 9 {
		t.Fatalf("prepared uniform value = %v, want current value 9", got)
	}
	if got := second.resources[0].param.value(material, draw).texture.id; got != 2 {
		t.Fatalf("prepared texture = %v, want current texture 2", got)
	}
}

func TestPreparedParameterPlanKeysOrderAndPreservesFirstDrawMatch(t *testing.T) {
	translator := newTranslator()
	layout := ShaderLayout{UniformSize: 4, Uniforms: []UniformMember{{Name: "value", Offset: 0}}}
	material := []ParameterDescr{FloatParam("value", 1)}
	draw := []ParameterDescr{FloatParam("value", 2), FloatParam("value", 3), FloatParam("other", 4)}
	plan := translator.prepareParameterPlan(1, "test", layout, material, draw)
	if got := plan.uniforms[0].param.value(material, draw).num; got != 2 {
		t.Fatalf("prepared duplicate draw value = %v, want first value 2", got)
	}

	reordered := []ParameterDescr{draw[2], draw[0], draw[1]}
	if got := translator.prepareParameterPlan(1, "test", layout, material, reordered); got == plan {
		t.Fatal("reordered parameter names reused the wrong plan")
	}
}

// A name is either a uniform member or a storage binding, never both. Matching
// ignores kind, so without this check the buffer descriptor is bound into the
// uniform slot and the draw renders garbage with nothing reported.
func TestPreparedParameterPlanRejectsAKindMismatch(t *testing.T) {
	translator := newTranslator()
	layout := ShaderLayout{
		UniformSize: 4,
		Uniforms:    []UniformMember{{Name: "wobble", Offset: 0}},
	}
	draw := []ParameterDescr{BufferParam("wobble", BufferDescr{id: 1})}
	plan := translator.prepareParameterPlan(1, "wobbly.wgsl", layout, nil, draw)
	mismatch, ok := plan.mismatch.(ErrParameterKindMismatch)
	if !ok {
		t.Fatalf("plan mismatch = %v, want ErrParameterKindMismatch", plan.mismatch)
	}
	if mismatch.Parameter != "wobble" || mismatch.Supplied != "buffer" || mismatch.Declared != "a uniform member" {
		t.Fatalf("mismatch = %+v, want wobble supplied as a buffer against a uniform member", mismatch)
	}
}

func TestPreparedParameterPlanAcceptsEveryKindThatFillsItsBinding(t *testing.T) {
	translator := newTranslator()
	layout := ShaderLayout{
		UniformSize: 64,
		Uniforms: []UniformMember{
			{Name: "scalar", Offset: 0}, {Name: "vector", Offset: 16},
			{Name: "tint", Offset: 32}, {Name: "record", Offset: 48},
		},
		Resources: []ShaderResource{
			{Name: "sampler", Sampler: true, Group: 1, Binding: 0},
			{Name: "texture", Group: 1, Binding: 1},
			{Name: "instances", StorageBuffer: true, Group: 2, Binding: 0},
		},
	}
	draw := []ParameterDescr{
		FloatParam("scalar", 1), VecParam("vector", m.Vec4{X: 1}),
		ColorParam("tint", m.Color{R: 1}), RawParameter("record", m.Vec4{Y: 1}),
		SamplerParam("sampler", SamplerDesc{}), TextureParam("texture", TextureDescr{id: 1}),
		BufferParam("instances", BufferDescr{id: 1}),
	}
	if plan := translator.prepareParameterPlan(1, "fine.wgsl", layout, nil, draw); plan.mismatch != nil {
		t.Fatalf("plan mismatch = %v, want none", plan.mismatch)
	}
}

// A parameter no shader declared is dropped, not rejected: a material carrying
// one for a sibling shader is ordinary, and the built-in canvas materials do it.
func TestPreparedParameterPlanIgnoresAnUnmatchedName(t *testing.T) {
	translator := newTranslator()
	layout := ShaderLayout{UniformSize: 4, Uniforms: []UniformMember{{Name: "value", Offset: 0}}}
	draw := []ParameterDescr{FloatParam("value", 1), BufferParam("unknown", BufferDescr{id: 1})}
	if plan := translator.prepareParameterPlan(1, "fine.wgsl", layout, nil, draw); plan.mismatch != nil {
		t.Fatalf("plan mismatch = %v, want none", plan.mismatch)
	}
}
