// The pixel tests' uniform block: one triangle covering a target of three
// texels, which hold the block's members as WGSL lays them out. The first
// is color and scale, the second offset and count over 255 with an opaque
// alpha, and the third tint.

@vertex
fn vertex_main(@builtin(vertex_index) index: u32) -> @builtin(position) vec4<f32> {
    let corner = vec2<f32>(f32((index << 1u) & 2u), f32(index & 2u));
    return vec4<f32>(corner.x * 2.0 - 1.0, 1.0 - corner.y * 2.0, 0.0, 1.0);
}

struct Block {
    color: vec3<f32>,
    scale: f32,
    offset: vec2<f32>,
    count: u32,
    tint: vec4<f32>,
}

@group(1) @binding(0) var<uniform> block: Block;

@fragment
fn fragment_main(@builtin(position) position: vec4<f32>) -> @location(0) vec4<f32> {
    switch u32(position.x) {
        case 0u: {
            return vec4<f32>(block.color, block.scale);
        }
        case 1u: {
            return vec4<f32>(block.offset, f32(block.count) / 255.0, 1.0);
        }
        default: {
            return block.tint;
        }
    }
}
