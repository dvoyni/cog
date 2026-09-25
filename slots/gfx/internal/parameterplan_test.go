package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/libs/m"
)

func TestPreparedParameterPlanReusesShapeAndReadsCurrentValues(t *testing.T) {
	translator := newTranslator()
	layout := shader.ShaderLayout{
		Resources: []shader.ShaderResource{
			{Name: "params", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 16, Members: []shader.StorageMember{{Name: "value", Offset: 0}}},
			{Name: "texture", Group: 1, Binding: 0},
		},
	}
	material := []descriptors.ParameterDescr{descriptors.TextureParam("texture", descriptors.BakedTexture(1, 0, 0))}
	draw := []descriptors.ParameterDescr{descriptors.FloatParam("value", 1)}
	first := translator.prepareParameterPlan(1, "test", layout, material, draw)

	material = []descriptors.ParameterDescr{descriptors.TextureParam("texture", descriptors.BakedTexture(2, 0, 0))}
	draw = []descriptors.ParameterDescr{descriptors.FloatParam("value", 9)}
	second := translator.prepareParameterPlan(1, "test", layout, material, draw)
	if second != first {
		t.Fatal("identical parameter shape did not reuse its prepared plan")
	}
	if got := descriptors.ParameterNum(second.uniforms[0].param.value(material, draw)); got != 9 {
		t.Fatalf("prepared uniform value = %v, want current value 9", got)
	}
	if got := descriptors.ParameterTexture(second.resources[0].param.value(material, draw)).ID(); got != 2 {
		t.Fatalf("prepared texture = %v, want current texture 2", got)
	}
}

func TestPreparedParameterPlanKeysOrderAndPreservesFirstDrawMatch(t *testing.T) {
	translator := newTranslator()
	layout := shader.ShaderLayout{
		Resources: []shader.ShaderResource{{Name: "params", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 4, Members: []shader.StorageMember{{Name: "value", Offset: 0}}}},
	}
	material := []descriptors.ParameterDescr{descriptors.FloatParam("value", 1)}
	draw := []descriptors.ParameterDescr{descriptors.FloatParam("value", 2), descriptors.FloatParam("value", 3), descriptors.FloatParam("other", 4)}
	plan := translator.prepareParameterPlan(1, "test", layout, material, draw)
	if got := descriptors.ParameterNum(plan.uniforms[0].param.value(material, draw)); got != 2 {
		t.Fatalf("prepared duplicate draw value = %v, want first value 2", got)
	}

	reordered := []descriptors.ParameterDescr{draw[2], draw[0], draw[1]}
	if got := translator.prepareParameterPlan(1, "test", layout, material, reordered); got == plan {
		t.Fatal("reordered parameter names reused the wrong plan")
	}
}

// A name is either a uniform member or a storage binding, never both. Matching
// ignores kind, so without this check the buffer descriptor is bound into the
// uniform slot and the draw renders garbage with nothing reported.
func TestPreparedParameterPlanRejectsAKindMismatch(t *testing.T) {
	translator := newTranslator()
	layout := shader.ShaderLayout{
		Resources: []shader.ShaderResource{{Name: "params", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 4, Members: []shader.StorageMember{{Name: "wobble", Offset: 0}}}},
	}
	draw := []descriptors.ParameterDescr{descriptors.BufferParam("wobble", descriptors.BakedBuffer(1, 0))}
	plan := translator.prepareParameterPlan(1, "wobbly.wgsl", layout, nil, draw)
	mismatch, ok := plan.mismatch.(types.ErrParameterKindMismatch)
	if !ok {
		t.Fatalf("plan mismatch = %v, want ErrParameterKindMismatch", plan.mismatch)
	}
	if mismatch.Parameter != "wobble" || mismatch.Supplied != "buffer" || mismatch.Declared != "a uniform member" {
		t.Fatalf("mismatch = %+v, want wobble supplied as a buffer against a uniform member", mismatch)
	}
}

func TestPreparedParameterPlanAcceptsEveryKindThatFillsItsBinding(t *testing.T) {
	translator := newTranslator()
	layout := shader.ShaderLayout{
		Resources: []shader.ShaderResource{
			{Name: "params", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 64, Members: []shader.StorageMember{
				{Name: "scalar", Offset: 0}, {Name: "vector", Offset: 16},
				{Name: "tint", Offset: 32}, {Name: "record", Offset: 48},
			}},
			{Name: "sampler", Kind: shader.ResourceSampler, Group: 1, Binding: 0},
			{Name: "texture", Group: 1, Binding: 1},
			{Name: "instances", Kind: shader.ResourceStorageBuffer, Group: 2, Binding: 0},
		},
	}
	draw := []descriptors.ParameterDescr{
		descriptors.FloatParam("scalar", 1), descriptors.VecParam("vector", m.Vec4{X: 1}),
		descriptors.ColorParam("tint", m.Color{R: 1}), descriptors.RawParameter("record", m.Vec4{Y: 1}),
		descriptors.SamplerParam("sampler", types.SamplerDesc{}), descriptors.TextureParam("texture", descriptors.BakedTexture(1, 0, 0)),
		descriptors.BufferParam("instances", descriptors.BakedBuffer(1, 0)),
	}
	if plan := translator.prepareParameterPlan(1, "fine.wgsl", layout, nil, draw); plan.mismatch != nil {
		t.Fatalf("plan mismatch = %v, want none", plan.mismatch)
	}
}

// A parameter no shader declared is dropped, not rejected: a material carrying
// one for a sibling shader is ordinary, and the built-in canvas materials do it.
func TestPreparedParameterPlanIgnoresAnUnmatchedName(t *testing.T) {
	translator := newTranslator()
	layout := shader.ShaderLayout{
		Resources: []shader.ShaderResource{{Name: "params", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 4, Members: []shader.StorageMember{{Name: "value", Offset: 0}}}},
	}
	draw := []descriptors.ParameterDescr{descriptors.FloatParam("value", 1), descriptors.BufferParam("unknown", descriptors.BakedBuffer(1, 0))}
	if plan := translator.prepareParameterPlan(1, "fine.wgsl", layout, nil, draw); plan.mismatch != nil {
		t.Fatalf("plan mismatch = %v, want none", plan.mismatch)
	}
}
