// The material hand-recorded geometry gets: sample canvasTexture through the
// key-colour ramp, times the vertex colour.
//
// It EXTENDS the canvas uniform block with its own keyColor member, so it
// hand-writes the block rather than including uniforms.wgsl - which is the
// mechanism a custom material uses for its own per-batch parameters, with a
// built-in demonstration rather than only a documented one.
//
// It is an entry point, not an includable source.
struct CanvasUniforms {
    canvasViewport: vec4<f32>,
    canvasLayer: mat4x4<f32>,
    canvasClip: vec4<f32>,
    keyColor: vec4<f32>,
};
@group(0) @binding(0) var<uniform> u: CanvasUniforms;

//#include builtin/canvas/trianglesvertex.wgsl
//#include builtin/canvas/clip.wgsl
//#include builtin/canvas/keycolor.wgsl

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    if canvasClipped(in.canvasPosition) { discard; }
    let sampled = keyColorRamp(textureSample(canvasTexture, canvasSampler, in.uv), u.keyColor.rgb);
    return sampled * in.color;
}
