package gfx_test

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
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

func TestShaderModuleEntryPointsAnswersEachEntryPointTheBackendReflects(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	reflected := []gfx.ShaderEntryPoint{{Name: "vs", Stage: gfx.ShaderStageFlagVertex}, {Name: "fs", Stage: gfx.ShaderStageFlagFragment}}
	h.instance.AnswerNext(recording.CallKindReflectEntryPoints, recording.Answer{EntryPoints: reflected})

	h.readDevice(func(device gfx.Device) {
		module, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{WGSL: triangleWGSL})
		mustSucceed(t, err)

		entryPoints, err := device.ShaderModuleEntryPoints(module)

		mustSucceed(t, err)
		if !reflect.DeepEqual(entryPoints, reflected) {
			t.Errorf("the entry points are %+v, want %+v", entryPoints, reflected)
		}
		if call, want := lastCall(h), (recording.Call{Kind: recording.CallKindReflectEntryPoints, Arguments: module}); !reflect.DeepEqual(call, want) {
			t.Errorf("the last call is %+v, want %+v", call, want)
		}
	})
}

func TestShaderModuleVertexInputsAnswersTheInputsTheBackendReflectsForTheEntryPoint(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	reflected := []gfx.VertexInput{{Location: 0, Type: gfx.ScalarTypeFloat32, Components: 3}}
	h.instance.AnswerNext(recording.CallKindReflectVertexInputs, recording.Answer{VertexInputs: reflected})

	h.readDevice(func(device gfx.Device) {
		module, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{WGSL: triangleWGSL})
		mustSucceed(t, err)

		inputs, err := device.ShaderModuleVertexInputs(module, "vs")

		mustSucceed(t, err)
		if !reflect.DeepEqual(inputs, reflected) {
			t.Errorf("the inputs are %+v, want %+v", inputs, reflected)
		}
		want := recording.Call{Kind: recording.CallKindReflectVertexInputs, Arguments: recording.ReflectVertexInputsArguments{Module: module, EntryPoint: "vs"}}
		if call := lastCall(h); !reflect.DeepEqual(call, want) {
			t.Errorf("the last call is %+v, want %+v", call, want)
		}
	})
}
