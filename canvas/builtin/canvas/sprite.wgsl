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

// The key-colour detector and ramp. See keyColorRamp below for why the
// thresholds are written the way they are.
const keyChannelTolerance: f32 = 0.2;   // sRGB: how far R and B may differ
const keyGreenCutoff: f32 = 0.0331048;  // 0.2 in sRGB
const keyRampMidpoint: f32 = 0.5;       // sRGB: the intensity keyColor lands on

// srgbEncode applies the sRGB OETF to one channel: linear light in, the
// gamma-encoded scalar an artist authored out.
fn srgbEncode(value: f32) -> f32 {
    if value <= 0.0031308 {
        return value * 12.92;
    }
    return 1.055 * pow(value, 1.0 / 2.4) - 0.055;
}

// srgbDecode is the inverse, the EOTF, on one channel.
fn srgbDecode(value: f32) -> f32 {
    if value <= 0.04045 {
        return value / 12.92;
    }
    return pow((value + 0.055) / 1.055, 2.4);
}

fn srgbEncode3(value: vec3<f32>) -> vec3<f32> {
    return vec3<f32>(srgbEncode(value.r), srgbEncode(value.g), srgbEncode(value.b));
}

fn srgbDecode3(value: vec3<f32>) -> vec3<f32> {
    return vec3<f32>(srgbDecode(value.r), srgbDecode(value.g), srgbDecode(value.b));
}

// keyColorRamp recolours the magenta key ramp an artist paints into a sprite,
// replacing it with the player's colour.
//
// The atlas is sRGB, so a sampled texel arrives as linear light, but both the
// detector and the ramp position were authored against the values the artist
// saw in an image editor. So the classification happens in sRGB: two of the
// three channels are encoded back (one pow each, cheaper and exact where a
// vec3 round trip would be neither), and green - which is only ever compared
// against a constant - takes the linear image of its sRGB cutoff instead. That
// keeps which texels are keyed, and where each one sits on the ramp,
// bit-for-bit what it was under a unorm atlas.
//
// The ramp itself also runs in sRGB, and that is not a compromise: the ramp is
// a palette lookup, not a blend of two lights. Most of a unit sprite is keyed -
// 67% to 87% of the opaque texels in feuds-26's art - so the ramp is not a tint
// over a drawing, it *is* the drawing's shading, authored by eye against these
// exact numbers. Running it in linear lightens the shadow end two to three fold
// (7,13,34 becomes 11,27,80 for azure at intensity 0.1) and the figure collapses
// into a flat block of player colour. So keyColor is encoded on the way in, the
// ramp reproduces the artist's values exactly, and the result is decoded once at
// the end - the only conversion the rest of the pipeline needs.
fn keyColorRamp(sampled: vec4<f32>, keyColor: vec3<f32>) -> vec4<f32> {
    let intensity = srgbEncode(sampled.r);
    if abs(intensity - srgbEncode(sampled.b)) >= keyChannelTolerance || sampled.g >= keyGreenCutoff {
        return sampled;
    }
    let key = srgbEncode3(keyColor);
    var ramped: vec3<f32>;
    if intensity <= keyRampMidpoint {
        ramped = key * intensity / keyRampMidpoint;
    } else {
        ramped = mix(key, vec3<f32>(1.0), (intensity - keyRampMidpoint) / (1.0 - keyRampMidpoint));
    }
    return vec4<f32>(srgbDecode3(ramped), sampled.a);
}

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