// Everything a triangles material binds, and what its vertex stage passes on.
//
// DECLARES: @group(1) @binding(0) canvasSampler; @group(1) @binding(1)
// canvasTexture: texture_2d<f32>; struct VertexOut. Do not redeclare any of
// them - a duplicate binding costs the whole frame with nothing reported.
//
// canvasTexture is texture_2d here and texture_2d_array on the sprite path, at
// the same group 1 either way, because the group rule keys on what a binding IS
// and not on the type behind it. That is also why the two families can never be
// one module: including both sources would declare canvasTexture, canvasSampler
// and VertexOut twice.
@group(1) @binding(0) var canvasSampler: sampler;
@group(1) @binding(1) var canvasTexture: texture_2d<f32>;

struct VertexOut {
    @builtin(position) position: vec4<f32>,
    @location(0) canvasPosition: vec2<f32>,
    @location(1) color: vec4<f32>,
    @location(2) uv: vec2<f32>,
};
