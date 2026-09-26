// The material hand-recorded geometry gets: sample canvasTexture through the
// key-colour ramp, times the vertex colour.
//
// It declares its own per-batch value, keyColor, as a uniform of its own beside
// the canvas block - which is the mechanism a custom material uses for its own
// per-batch parameters, with a built-in demonstration rather than only a
// documented one. A draw's gfx.ColorParam(canvas.KeyColorSlot, colour) sets it
// whole, and the material's own default is the mid grey that leaves artwork as
// painted. A custom triangles material that keys declares the same line.
//
// It is an entry point, not an includable source.
//#include builtin/canvas/uniforms.wgsl
@group(0) @binding(1) var<uniform> keyColor: vec4<f32>;

//#include builtin/canvas/trianglesvertex.wgsl
//#include builtin/canvas/clip.wgsl
//#include builtin/canvas/keycolor.wgsl

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    if canvasClipped(in.canvasPosition) { discard; }
    let sampled = keyColorRamp(textureSample(canvasTexture, canvasSampler, in.uv), keyColor.rgb);
    return sampled * in.color;
}
