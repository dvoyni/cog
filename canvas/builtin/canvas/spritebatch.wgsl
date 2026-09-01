struct BatchUniforms {
    canvasViewport: vec4<f32>, // width, height, clipEnabled, unused
    canvasLayer: mat4x4<f32>,
    canvasClip: vec4<f32>,     // minX, minY, maxX, maxY
};

struct SpriteInstance {
    transform0: vec4<f32>, // position.xy, size.xy
    transform1: vec4<f32>, // origin.xy, sine, cosine
    frame: vec4<f32>,      // uv rect (x0, y0, x1, y1)
    tint: vec4<f32>,
    misc: vec4<f32>,       // atlasLayer, unused, unused, unused
    keyColor: vec4<f32>,
};

struct Instances {
    data: array<SpriteInstance>,
};

@group(0) @binding(0) var<uniform> u: BatchUniforms;
@group(1) @binding(0) var<storage, read> instances: Instances;
@group(2) @binding(0) var canvasSampler: sampler;
@group(2) @binding(1) var canvasTexture: texture_2d_array<f32>;

struct VertexOut {
    @builtin(position) position: vec4<f32>,
    @location(0) canvasPosition: vec2<f32>,
    @location(1) uv: vec2<f32>,
    @location(2) @interpolate(flat) atlasLayer: i32,
    @location(3) tint: vec4<f32>,
    @location(4) keyColor: vec4<f32>,
};

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

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    if u.canvasViewport.z > 0.5 && (
        in.canvasPosition.x < u.canvasClip.x || in.canvasPosition.y < u.canvasClip.y ||
        in.canvasPosition.x > u.canvasClip.z || in.canvasPosition.y > u.canvasClip.w
    ) {
        discard;
    }
    var sampled = textureSample(canvasTexture, canvasSampler, in.uv, in.atlasLayer);
    if abs(sampled.r - sampled.b) < 0.2 && sampled.g < 0.2 {
        let intensity = sampled.r;
        if intensity <= 0.5 {
            sampled = vec4<f32>(in.keyColor.rgb * intensity * 2.0, sampled.a);
        } else {
            sampled = vec4<f32>(mix(in.keyColor.rgb, vec3<f32>(1.0), (intensity - 0.5) * 2.0), sampled.a);
        }
    }
    return sampled * in.tint;
}
