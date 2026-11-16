// The pixel tests' last pass: one triangle covering the target, each texel
// the texel of picture at the same place, wrapped to picture's size, encoded
// as sRGB in the shader, as the last pass into a BGRA8Unorm framebuffer
// encodes it.

@vertex
fn vertex_main(@builtin(vertex_index) index: u32) -> @builtin(position) vec4<f32> {
    let corner = vec2<f32>(f32((index << 1u) & 2u), f32(index & 2u));
    return vec4<f32>(corner.x * 2.0 - 1.0, 1.0 - corner.y * 2.0, 0.0, 1.0);
}

@group(1) @binding(0) var picture: texture_2d<f32>;

fn encode_srgb(linear: vec3<f32>) -> vec3<f32> {
    let low = linear * 12.92;
    let high = 1.055 * pow(linear, vec3<f32>(1.0 / 2.4)) - 0.055;
    return select(high, low, linear <= vec3<f32>(0.0031308));
}

@fragment
fn fragment_main(@builtin(position) position: vec4<f32>) -> @location(0) vec4<f32> {
    let texel = textureLoad(picture, vec2<u32>(position.xy) % textureDimensions(picture), 0);
    return vec4<f32>(encode_srgb(texel.rgb), texel.a);
}
