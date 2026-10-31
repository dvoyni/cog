// The material hand-recorded geometry gets: sample canvasTexture, times the
// vertex colour.
//
// The vertex colour is a plain multiply, not the tint ramp: it is the colour the
// geometry is, and a vertex coloured white over the backend's white texture
// draws white. A sprite that canvas routes through this family - a tiled one -
// has its tint carried onto the ramp before it becomes a vertex colour, so it
// draws as it would have on the sprite path.
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
