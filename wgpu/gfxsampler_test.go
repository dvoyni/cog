package wgpu

import (
	"testing"

	cgfx "github.com/dvoyni/cog/gfx"
	"github.com/gogpu/gputypes"
)

func TestSamplerDescriptorMapsEveryAxisAndFilter(t *testing.T) {
	got := samplerDescriptor(cgfx.SamplerDesc{
		AddressU: cgfx.AddressRepeat, AddressV: cgfx.AddressMirror,
		Mag: cgfx.FilterNearest, Min: cgfx.FilterLinear, Mip: cgfx.FilterNearest,
		Label: "test",
	})
	if got.AddressModeU != gputypes.AddressModeRepeat {
		t.Errorf("AddressModeU = %v, want Repeat", got.AddressModeU)
	}
	if got.AddressModeV != gputypes.AddressModeMirrorRepeat {
		t.Errorf("AddressModeV = %v, want MirrorRepeat", got.AddressModeV)
	}
	if got.MagFilter != gputypes.FilterModeNearest || got.MinFilter != gputypes.FilterModeLinear {
		t.Errorf("mag/min = %v/%v, want Nearest/Linear", got.MagFilter, got.MinFilter)
	}
	if got.MipmapFilter != gputypes.FilterModeNearest {
		t.Errorf("mipmap filter = %v, want Nearest", got.MipmapFilter)
	}
	if got.Compare != gputypes.CompareFunctionUndefined {
		t.Errorf("compare = %v, want Undefined on a non-comparison sampler", got.Compare)
	}
}

func TestComparisonSamplersCarryTheirCompare(t *testing.T) {
	got := samplerDescriptor(cgfx.SamplerDesc{Comparison: true, Compare: cgfx.CompareLessEqual})
	if got.Compare != gputypes.CompareFunctionLessEqual {
		t.Errorf("compare = %v, want LessEqual", got.Compare)
	}
	// Compare is ignored unless the sampler says it compares.
	plain := samplerDescriptor(cgfx.SamplerDesc{Compare: cgfx.CompareLessEqual})
	if plain.Compare != gputypes.CompareFunctionUndefined {
		t.Errorf("compare = %v, want Undefined when Comparison is false", plain.Compare)
	}
}

func TestAnisotropyIsClampedAndOffAtZeroOrOne(t *testing.T) {
	for _, off := range []uint8{0, 1} {
		if got := samplerDescriptor(cgfx.SamplerDesc{Anisotropy: off}).Anisotropy; got != 1 {
			t.Errorf("Anisotropy for Anisotropy=%d is %d, want 1 (off)", off, got)
		}
	}
	if got := samplerDescriptor(cgfx.SamplerDesc{Anisotropy: 64}).Anisotropy; got != 16 {
		t.Errorf("Anisotropy for Anisotropy=64 is %d, want the clamp of 16", got)
	}
}

func TestAnisotropyWithoutLinearFilteringIsRejected(t *testing.T) {
	// WebGPU requires mag, min and mip all linear when maxAnisotropy > 1. A
	// silent clamp would hide the mistake.
	if err := validateSampler(cgfx.SamplerDesc{Anisotropy: 4, Mip: cgfx.FilterNearest}); err == nil {
		t.Error("nearest mip with anisotropy was accepted, want an error")
	}
	if err := validateSampler(cgfx.SamplerDesc{Anisotropy: 4}); err != nil {
		t.Errorf("linear sampler with anisotropy rejected: %v", err)
	}
	if err := validateSampler(cgfx.SamplerDesc{Mag: cgfx.FilterNearest}); err != nil {
		t.Errorf("nearest sampler without anisotropy rejected: %v", err)
	}
}

const testShadowWGSL = `
struct U { mvp: mat4x4<f32> };
@group(0) @binding(0) var<uniform> u: U;
@group(1) @binding(0) var shadowSampler: sampler_comparison;
@group(1) @binding(1) var shadowMap: texture_depth_2d;

@vertex
fn vs_main(@location(0) pos: vec3<f32>) -> @builtin(position) vec4<f32> {
	return u.mvp * vec4<f32>(pos, 1.0);
}

@fragment
fn fs_main() -> @location(0) vec4<f32> {
	let lit = textureSampleCompare(shadowMap, shadowSampler, vec2<f32>(0.5, 0.5), 0.5);
	return vec4<f32>(lit, lit, lit, 1.0);
}
`

func TestReflectionTypesDepthTexturesAndComparisonSamplers(t *testing.T) {
	layout, err := reflectShaderLayout(testShadowWGSL)
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}
	byName := map[string]cgfx.ShaderResource{}
	for _, r := range layout.Resources {
		byName[r.Name] = r
	}
	if sampler := byName["shadowSampler"]; !sampler.Sampler || !sampler.Comparison {
		t.Errorf("shadowSampler = %+v, want a comparison sampler", sampler)
	}
	if texture := byName["shadowMap"]; texture.Sampler || !texture.Depth {
		t.Errorf("shadowMap = %+v, want a depth texture", texture)
	}
	// A colour texture and its filtering sampler keep their types.
	colour, err := reflectShaderLayout(testUnlitWGSL)
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}
	for _, r := range colour.Resources {
		if r.Depth || r.Comparison {
			t.Errorf("resource %+v, want neither depth nor comparison", r)
		}
	}
}
