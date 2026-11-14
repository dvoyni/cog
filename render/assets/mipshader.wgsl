// render's mip shader: one triangle covering the target mip level, each texel
// sampled from the level above it through a linear filter, so an even level
// averages the four texels it halves.

struct VertexOutput {
    @builtin(position) position: vec4<f32>,
    @location(0) coordinates: vec2<f32>,
}

@vertex
fn vertex_main(@builtin(vertex_index) index: u32) -> VertexOutput {
    // The vertices are (0, 0), (2, 0) and (0, 2) in texture coordinates,
    // whose triangle holds the whole target.
    let coordinates = vec2<f32>(f32((index << 1u) & 2u), f32(index & 2u));
    var output: VertexOutput;
    output.position = vec4<f32>(coordinates.x * 2.0 - 1.0, 1.0 - coordinates.y * 2.0, 0.0, 1.0);
    output.coordinates = coordinates;
    return output;
}

@group(0) @binding(0) var source: texture_2d<f32>;
@group(0) @binding(1) var source_sampler: sampler;

@fragment
fn fragment_main(input: VertexOutput) -> @location(0) vec4<f32> {
    return textureSample(source, source_sampler, input.coordinates);
}
