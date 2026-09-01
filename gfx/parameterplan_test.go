package gfx

import (
	"testing"
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
	first := translator.prepareParameterPlan(1, layout, material, draw)

	material = []ParameterDescr{TextureParam("texture", TextureDescr{id: 2})}
	draw = []ParameterDescr{FloatParam("value", 9)}
	second := translator.prepareParameterPlan(1, layout, material, draw)
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
	plan := translator.prepareParameterPlan(1, layout, material, draw)
	if got := plan.uniforms[0].param.value(material, draw).num; got != 2 {
		t.Fatalf("prepared duplicate draw value = %v, want first value 2", got)
	}

	reordered := []ParameterDescr{draw[2], draw[0], draw[1]}
	if got := translator.prepareParameterPlan(1, layout, material, reordered); got == plan {
		t.Fatal("reordered parameter names reused the wrong plan")
	}
}
