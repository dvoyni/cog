// The sprite vertex stage, over the bindings it reads.
//
// DECLARES: fn vs_main, plus everything spritebindings.wgsl declares, which it
// includes. Include this to replace only fs_main; include spritebindings.wgsl
// alone to write a vs_main of your own.
//
// The vertex input @location(0) quad: vec2<f32> and the @builtin(instance_index)
// read that selects the record are part of the contract: canvas draws a unit
// quad instanced, and a lone sprite is the one-instance case.
//
// It reads u.canvasLayer and u.canvasViewport but declares neither, so an
// extending material that hand-writes its own CanvasUniforms works unchanged.
//#include builtin/canvas/spritebindings.wgsl

@vertex
fn vs_main(@location(0) quad: vec2<f32>, @builtin(instance_index) instance: u32) -> VertexOut {
    let s = instances.data[instance];
    let origin = s.transform1.xy;
    let sine = s.transform1.z;
    let cosine = s.transform1.w;
    let scaled = (quad - origin) * s.transform0.zw;
    let rotated = vec2<f32>(
        scaled.x * cosine - scaled.y * sine,
        scaled.x * sine + scaled.y * cosine,
    );
    let local = s.transform0.xy + rotated;
    let world = u.canvasLayer * vec4<f32>(local, 0.0, 1.0);
    let viewport = u.canvasViewport.xy;
    var out: VertexOut;
    out.position = vec4<f32>(world.x * 2.0 / viewport.x - 1.0, 1.0 - world.y * 2.0 / viewport.y, 0.0, 1.0);
    out.canvasPosition = local;
    out.uv = mix(s.frame.xy, s.frame.zw, quad);
    out.atlasLayer = i32(s.misc.x);
    out.tint = s.tint;
    out.keyColor = s.keyColor;
    return out;
}
