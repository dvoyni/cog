struct CanvasUniforms {
    canvasTransform0: vec4<f32>,
    canvasTransform1: vec4<f32>,
    canvasFrame: vec4<f32>,
    canvasViewport: vec2<f32>,
    atlasLayer: f32,
    clipEnabled: f32,
    canvasLayer: mat4x4<f32>,
    canvasClip: vec4<f32>,
    tint: vec4<f32>,
    keyColor: vec4<f32>,
};
@group(0) @binding(0) var<uniform> u: CanvasUniforms;
@group(1) @binding(0) var canvasSampler: sampler;
@group(1) @binding(1) var canvasTexture: texture_2d_array<f32>;

//#include ./keycolor.wgsl

struct VertexOut {
    @builtin(position) position: vec4<f32>,
    @location(0) canvasPosition: vec2<f32>,
    @location(1) uv: vec2<f32>,
    @location(2) @interpolate(flat) atlasLayer: i32,
};

@vertex
fn vs_main(@location(0) quad: vec2<f32>) -> VertexOut {
    let origin = u.canvasTransform1.xy;
    let sine = u.canvasTransform1.z;
    let cosine = u.canvasTransform1.w;
    let scaled = (quad - origin) * u.canvasTransform0.zw;
    let rotated = vec2<f32>(
        scaled.x * cosine - scaled.y * sine,
        scaled.x * sine + scaled.y * cosine,
    );
    let local = u.canvasTransform0.xy + rotated;
    let world = u.canvasLayer * vec4<f32>(local, 0.0, 1.0);
    let viewport = u.canvasViewport;
    var out: VertexOut;
    out.position = vec4<f32>(world.x * 2.0 / viewport.x - 1.0, 1.0 - world.y * 2.0 / viewport.y, 0.0, 1.0);
    out.canvasPosition = local;
    out.uv = mix(u.canvasFrame.xy, u.canvasFrame.zw, quad);
    out.atlasLayer = i32(u.atlasLayer);
    return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    if u.clipEnabled > 0.5 && (
        in.canvasPosition.x < u.canvasClip.x || in.canvasPosition.y < u.canvasClip.y ||
        in.canvasPosition.x > u.canvasClip.z || in.canvasPosition.y > u.canvasClip.w
    ) {
        discard;
    }
    let sampled = keyColorRamp(textureSample(canvasTexture, canvasSampler, in.uv, in.atlasLayer), u.keyColor.rgb);
    return sampled * u.tint;
}