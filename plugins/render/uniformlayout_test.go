package render

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/plugins/gfx"
)

func TestAUniformTypeFitsABlockOfItsSizeAndMemberOffsetsInOrderPaddingLeftOut(t *testing.T) {
	block := gfx.ShaderBinding{
		Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: 32},
		Type:   gfx.ShaderType{Size: 32, Members: []gfx.ShaderStructMember{{Name: "color", Offset: 0, Type: gfx.ShaderType{Size: 16}}, {Name: "strength", Offset: 16, Type: gfx.ShaderType{Size: 4}}}},
	}
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
		goType  reflect.Type
		binding gfx.ShaderBinding
		wantErr bool
	}{
		"a struct of the block's layout":     {goType: reflect.TypeFor[fitting](), binding: block},
		"a struct of its members swapped":    {goType: reflect.TypeFor[swapped](), binding: block, wantErr: true},
		"a struct of another size":           {goType: reflect.TypeFor[unpadded](), binding: block, wantErr: true},
		"a struct of a field too many":       {goType: reflect.TypeFor[extra](), binding: block, wantErr: true},
		"a struct of a field too few":        {goType: reflect.TypeFor[short](), binding: block, wantErr: true},
		"no struct, for a block of members":  {goType: reflect.TypeFor[[8]float32](), binding: block, wantErr: true},
		"no struct, for a block of none":     {goType: reflect.TypeFor[[4]float32](), binding: gfx.ShaderBinding{Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: 16}}},
		"no struct, of another size of none": {goType: reflect.TypeFor[float32](), binding: gfx.ShaderBinding{Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: 16}}, wantErr: true},
	} {
		if err := checkUniformLayout(test.goType, test.binding); (err != nil) != test.wantErr {
			t.Errorf("%s: answered %v, want an error: %t", name, err, test.wantErr)
		}
	}
}
