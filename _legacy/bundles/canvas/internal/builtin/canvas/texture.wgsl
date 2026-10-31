// The material a draw sourcing an arbitrary gfx texture gets: it samples and
// returns, exactly as triangles.wgsl does. It is a family of its own for its
// scope slot and its sampler rather than for its shader: a scope can name a
// material over texture-sourced draws without naming one over hand-recorded
// geometry, and the default sampler here is clamped and linear.
//
// It is an entry point, not an includable source, and declares no value of its
// own: the canvas block is all it reads.
//#include builtin/canvas/uniforms.wgsl
//#include builtin/canvas/trianglesvertex.wgsl
//#include builtin/canvas/clip.wgsl

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    if canvasClipped(in.canvasPosition) { discard; }
    return textureSample(canvasTexture, canvasSampler, in.uv) * in.color;
}
