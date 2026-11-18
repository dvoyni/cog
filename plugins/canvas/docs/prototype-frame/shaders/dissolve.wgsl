// PROTOTYPE: the dissolve material's whole shader, a fragment stage alone, so
// canvas's vertex stages draw sprites and canvas.Vertex meshes with it. It
// declares only @group(1) and Argument; canvas's include declares the rest.
#include "cog.canvas/canvas.wgsl"

struct Argument {
    amount: f32,
    edge: vec4<f32>,   // at offset 16, after Dissolve's _ [3]float32
}

@group(1) @binding(0) var noise: texture_2d<f32>;
@group(1) @binding(1) var noise_sampler: sampler;

@fragment
fn dissolve(in: CanvasVaryings) -> @location(0) vec4<f32> {
    let argument = canvas_argument(in);
    let threshold = textureSample(noise, noise_sampler, in.uv_local).r;
    if threshold < argument.amount {
        discard;
    }
    let edge = select(vec4(0.0), argument.edge, threshold < argument.amount + 0.05);
    return canvas_output(canvas_sample(in) * in.tint + edge);
}
