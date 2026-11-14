package gfx_test

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
)

func TestShaderModuleBindingsAnswersEveryBindingTheBackendReflectsTheUnusedIncluded(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	h.instance.AnswerNext(recording.CallKindReflectBindings, recording.Answer{Bindings: reflectedBindings()})

	h.readDevice(func(device gfx.Device) {
		module, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{WGSL: triangleWGSL})
		mustSucceed(t, err)

		bindings, err := device.ShaderModuleBindings(module)

		mustSucceed(t, err)
		if !reflect.DeepEqual(bindings, reflectedBindings()) {
			t.Errorf("the bindings are %+v, want %+v", bindings, reflectedBindings())
		}
		if call, want := lastCall(h), (recording.Call{Kind: recording.CallKindReflectBindings, Arguments: module}); !reflect.DeepEqual(call, want) {
			t.Errorf("the last call is %+v, want %+v", call, want)
		}
	})
}

// reflectedBindings answers the bindings the named reflection tests queue: a
// uniform block of two members both entry points use, and a texture none
// uses.
func reflectedBindings() []gfx.ShaderBinding {
	return []gfx.ShaderBinding{
		{
			Name:   "camera",
			Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: 80},
			Members: []gfx.ShaderStructMember{
				{Name: "viewProjection", Offset: 0, Size: 64},
				{Name: "position", Offset: 64, Size: 12},
			},
			EntryPoints: []string{"vs", "fs"},
		},
		{
			Name:    "unusedTexture",
			Group:   1,
			Binding: 2,
			Texture: gfx.TextureBindingLayout{SampleType: gfx.TextureSampleTypeFloat, ViewDimension: gfx.TextureViewDimensionCube},
		},
	}
}
