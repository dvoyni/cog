// The pixel tests' unbound defaults: one triangle covering a target of two
// texels, each holding what the bindings a test leaves unbound draw with.
// Red is the float texture's texel, green the depth texture's depth, and
// blue a quarter plus the integer texture's texel and the uniform block's
// four values. Alpha is ramp sampled through ramp_sampler: at its middle in
// the first texel, which a linear filter averages, and half a texture left
// of it in the second, which clamping to the edge reads as the first texel.

@vertex
fn vertex_main(@builtin(vertex_index) index: u32) -> @builtin(position) vec4<f32> {
    let corner = vec2<f32>(f32((index << 1u) & 2u), f32(index & 2u));
    return vec4<f32>(corner.x * 2.0 - 1.0, 1.0 - corner.y * 2.0, 0.0, 1.0);
}

struct Block {
    value: vec4<f32>,
}

@group(1) @binding(0) var white: texture_2d<f32>;
@group(1) @binding(1) var depth: texture_depth_2d;
@group(1) @binding(2) var integers: texture_2d<u32>;
@group(1) @binding(3) var<uniform> block: Block;
@group(1) @binding(4) var ramp: texture_2d<f32>;
@group(1) @binding(5) var ramp_sampler: sampler;

@fragment
fn fragment_main(@builtin(position) position: vec4<f32>) -> @location(0) vec4<f32> {
    let u = select(-0.5, 0.5, position.x < 1.0);
    let sampled = textureSample(ramp, ramp_sampler, vec2<f32>(u, 0.5)).r;
    let zeros = f32(textureLoad(integers, vec2<i32>(0), 0).r) + block.value.x + block.value.y + block.value.z + block.value.w;
    return vec4<f32>(textureLoad(white, vec2<i32>(0), 0).r, textureLoad(depth, vec2<i32>(0), 0), 0.25 + zeros, sampled);
}
