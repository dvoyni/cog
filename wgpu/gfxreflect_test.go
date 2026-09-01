package wgpu

import (
	"testing"

	cgfx "github.com/dvoyni/cog/gfx"
)

const testUnlitWGSL = `
struct U { mvp: mat4x4<f32>, tint: vec4<f32> };
@group(0) @binding(0) var<uniform> u: U;
@group(1) @binding(0) var samp: sampler;
@group(1) @binding(1) var tex: texture_2d<f32>;

struct VSOut {
	@builtin(position) pos: vec4<f32>,
	@location(0) uv: vec2<f32>,
	@location(1) color: vec4<f32>,
};

@vertex
fn vs_main(@location(0) pos: vec3<f32>, @location(1) uv: vec2<f32>, @location(2) color: vec4<f32>) -> VSOut {
	var out: VSOut;
	out.pos = u.mvp * vec4<f32>(pos, 1.0);
	out.uv = uv;
	out.color = color;
	return out;
}

@fragment
fn fs_main(in: VSOut) -> @location(0) vec4<f32> {
	return textureSample(tex, samp, in.uv) * in.color * u.tint;
}
`

func TestReflectShaderLayout(t *testing.T) {
	layout, err := reflectShaderLayout(testUnlitWGSL)
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}
	if layout.UniformSize != 80 {
		t.Errorf("uniform size = %d, want 80", layout.UniformSize)
	}
	got := map[string]int{}
	for _, m := range layout.Uniforms {
		got[m.Name] = m.Offset
	}
	for name, want := range map[string]int{"mvp": 0, "tint": 64} {
		if got[name] != want {
			t.Errorf("member %q offset = %d, want %d", name, got[name], want)
		}
	}
}

func TestReflectShaderStorageBuffers(t *testing.T) {
	layout, err := reflectShaderLayout(`
struct Data { values: array<vec4<f32>, 1> };
@group(2) @binding(3) var<storage, read> source: Data;
@group(2) @binding(4) var<storage, read_write> target: Data;

@vertex
fn vs_main() -> @builtin(position) vec4<f32> {
	return source.values[0];
}

@fragment
fn fs_main() -> @location(0) vec4<f32> {
	target.values[0] = source.values[0];
	return target.values[0];
}
`)
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}
	resources := map[string]cgfx.ShaderResource{}
	for _, resource := range layout.Resources {
		resources[resource.Name] = resource
	}
	if source := resources["source"]; !source.StorageBuffer || source.WritableBuffer || source.Group != 2 || source.Binding != 3 {
		t.Errorf("source = %+v, want read-only storage buffer at 2:3", source)
	}
	if target := resources["target"]; !target.StorageBuffer || !target.WritableBuffer || target.Group != 2 || target.Binding != 4 {
		t.Errorf("target = %+v, want writable storage buffer at 2:4", target)
	}
}

func TestReflectShaderTextureArray(t *testing.T) {
	layout, err := reflectShaderLayout(`
@group(1) @binding(0) var regular: texture_2d<f32>;
@group(1) @binding(1) var atlas: texture_2d_array<f32>;

@vertex
fn vs_main() -> @builtin(position) vec4<f32> { return vec4<f32>(); }
@fragment
fn fs_main() -> @location(0) vec4<f32> { return vec4<f32>(); }
`)
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}
	resources := map[string]cgfx.ShaderResource{}
	for _, resource := range layout.Resources {
		resources[resource.Name] = resource
	}
	if got := resources["regular"].TextureView; got != cgfx.TextureView2D {
		t.Fatalf("regular texture view = %v, want 2D", got)
	}
	if got := resources["atlas"].TextureView; got != cgfx.TextureView2DArray {
		t.Fatalf("atlas texture view = %v, want 2D array", got)
	}
}
