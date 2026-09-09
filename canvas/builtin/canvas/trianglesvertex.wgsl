// The triangles vertex stage, over the bindings it reads.
//
// DECLARES: fn vs_main, plus everything trianglesbindings.wgsl declares, which
// it includes. Include this to replace only fs_main; include
// trianglesbindings.wgsl alone to write a vs_main of your own.
//
// The vertex input is canvas.Vertex: position, colour and uv at locations 0, 1
// and 2. It reads u.canvasLayer and u.canvasViewport but declares neither, so an
// extending material that hand-writes its own CanvasUniforms works unchanged.
//#include builtin/canvas/trianglesbindings.wgsl

@vertex
fn vs_main(
    @location(0) position: vec2<f32>,
    @location(1) color: vec4<f32>,
    @location(2) uv: vec2<f32>,
) -> VertexOut {
    let world = u.canvasLayer * vec4<f32>(position, 0.0, 1.0);
    let viewport = u.canvasViewport.xy;
    var out: VertexOut;
    out.position = vec4<f32>(world.x * 2.0 / viewport.x - 1.0, 1.0 - world.y * 2.0 / viewport.y, 0.0, 1.0);
    out.canvasPosition = position;
    out.color = color;
    out.uv = uv;
    return out;
}
