package bindingdeclaration_test

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/render/internal/bindingdeclaration"
)

func TestAMarshalledDeclarationReadsBackEachVariantsReflection(t *testing.T) {
	base := bindingdeclaration.Reflection{
		Bindings: []gfx.ShaderBinding{
			{
				Name:        "camera",
				Buffer:      gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: 80},
				Members:     []gfx.ShaderStructMember{{Name: "view", Size: 64}, {Name: "time", Offset: 64, Size: 4}},
				EntryPoints: []string{"vertex_main", "fragment_main"},
			},
			{Name: "lights", Binding: 1, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeReadOnlyStorage, MinBindingSize: 16}},
			{Name: "albedo", Group: 1, Texture: gfx.TextureBindingLayout{SampleType: gfx.TextureSampleTypeUnfilterableFloat, ViewDimension: gfx.TextureViewDimension2D, Multisampled: true}, EntryPoints: []string{"fragment_main"}},
			{Name: "shadow", Group: 1, Binding: 1, Sampler: gfx.SamplerBindingLayout{Type: gfx.SamplerBindingTypeComparison}},
			{Name: "written", Group: 2, StorageTexture: gfx.StorageTextureBindingLayout{Access: gfx.StorageTextureAccessReadWrite, Format: gfx.TextureFormatR32Float, ViewDimension: gfx.TextureViewDimension2DArray}},
		},
		EntryPoints: []gfx.ShaderEntryPoint{
			{Name: "vertex_main", Stage: gfx.ShaderStageFlagVertex},
			{Name: "fragment_main", Stage: gfx.ShaderStageFlagFragment},
			{Name: "cull", Stage: gfx.ShaderStageFlagCompute},
		},
		VertexInputs: map[string][]gfx.VertexInput{
			"vertex_main": {{Location: 0, Type: gfx.ScalarTypeFloat32, Components: 3}, {Location: 1, Type: gfx.ScalarTypeUint32, Components: 1}},
		},
	}
	skinned := bindingdeclaration.Variant{
		Defines: []gfx.ShaderDefine{{Name: "SKINNED"}, {Name: "JOINTS", Value: "4"}},
		Reflection: bindingdeclaration.Reflection{
			Bindings:     []gfx.ShaderBinding{{Name: "joints", Group: 2, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeStorage, MinBindingSize: 64}}},
			EntryPoints:  []gfx.ShaderEntryPoint{{Name: "vertex_main", Stage: gfx.ShaderStageFlagVertex}},
			VertexInputs: map[string][]gfx.VertexInput{"vertex_main": {{Location: 0, Type: gfx.ScalarTypeSint32, Components: 4}}},
		},
	}

	declarationJSON, err := bindingdeclaration.Marshal(base, []bindingdeclaration.Variant{skinned})

	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{"lit.wgsl.json": {Data: declarationJSON}}
	for _, variant := range []bindingdeclaration.Variant{{Reflection: base}, skinned} {
		reflection, err := bindingdeclaration.Read(files, "lit.wgsl", variant.Defines)
		if err != nil || !reflect.DeepEqual(reflection, variant.Reflection) {
			t.Errorf("with defines %q the declaration read back\n%+v, %v\nwant\n%+v\nfrom\n%s", variant.Defines, reflection, err, variant.Reflection, declarationJSON)
		}
	}
}

func TestAValueTheFormatCannotNameFailsMarshalNamingIt(t *testing.T) {
	for name, test := range map[string]struct {
		reflection bindingdeclaration.Reflection
		named      string
	}{
		"a binding of no kind": {
			bindingdeclaration.Reflection{Bindings: []gfx.ShaderBinding{{Name: "nothing"}}},
			`binding "nothing"`,
		},
		"a storage format WebGPU refuses a storage texture": {
			bindingdeclaration.Reflection{Bindings: []gfx.ShaderBinding{{Name: "written", StorageTexture: gfx.StorageTextureBindingLayout{Access: gfx.StorageTextureAccessWriteOnly, Format: gfx.TextureFormatDepth32Float, ViewDimension: gfx.TextureViewDimension2D}}}},
			"storage format",
		},
		"an entry point of several stages": {
			bindingdeclaration.Reflection{EntryPoints: []gfx.ShaderEntryPoint{{Name: "both", Stage: gfx.ShaderStageFlagVertex | gfx.ShaderStageFlagFragment}}},
			`entry point "both"`,
		},
		"a vector of five": {
			bindingdeclaration.Reflection{VertexInputs: map[string][]gfx.VertexInput{"vertex_main": {{Location: 2, Type: gfx.ScalarTypeFloat32, Components: 5}}}},
			"location 2",
		},
	} {
		_, err := bindingdeclaration.Marshal(test.reflection, nil)
		if err == nil || !strings.Contains(err.Error(), test.named) {
			t.Errorf("%s: Marshal answered %v, want an error naming %s", name, err, test.named)
		}
	}
}
