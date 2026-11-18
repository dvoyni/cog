package render

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/plugins/gfx"
)

func TestAGoTypeFitsAShaderTypeOfItsSizeAndMemberOffsetsInOrderPaddingLeftOut(t *testing.T) {
	block := gfx.ShaderType{Size: 32, Members: []gfx.ShaderStructMember{{Name: "color", Offset: 0, Type: gfx.ShaderType{Size: 16}}, {Name: "strength", Offset: 16, Type: gfx.ShaderType{Size: 4}}}}
	type fitting struct {
		Colour [4]float32
		Weight float32
		_      [12]byte
	}
	type swapped struct {
		Weight float32
		Colour [4]float32
		_      [12]byte
	}
	type unpadded struct {
		Colour [4]float32
		Weight float32
	}
	type extra struct {
		Colour [4]float32
		Weight float32
		More   float32
		_      [8]byte
	}
	type short struct {
		Colour [4]float32
		_      [16]byte
	}
	for name, test := range map[string]struct {
		goType     reflect.Type
		shaderType gfx.ShaderType
		wantErr    bool
	}{
		"a struct of the block's layout":     {goType: reflect.TypeFor[fitting](), shaderType: block},
		"a struct of its members swapped":    {goType: reflect.TypeFor[swapped](), shaderType: block, wantErr: true},
		"a struct of another size":           {goType: reflect.TypeFor[unpadded](), shaderType: block, wantErr: true},
		"a struct of a field too many":       {goType: reflect.TypeFor[extra](), shaderType: block, wantErr: true},
		"a struct of a field too few":        {goType: reflect.TypeFor[short](), shaderType: block, wantErr: true},
		"no struct, for a block of members":  {goType: reflect.TypeFor[[8]float32](), shaderType: block, wantErr: true},
		"no struct, for a block of none":     {goType: reflect.TypeFor[[4]float32](), shaderType: gfx.ShaderType{Size: 16}},
		"no struct, of another size of none": {goType: reflect.TypeFor[float32](), shaderType: gfx.ShaderType{Size: 16}, wantErr: true},
	} {
		if err := checkTypeLayout(test.goType, test.shaderType, "its uniform block"); (err != nil) != test.wantErr {
			t.Errorf("%s: answered %v, want an error: %t", name, err, test.wantErr)
		}
	}
}
