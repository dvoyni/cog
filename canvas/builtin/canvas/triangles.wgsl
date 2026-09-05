struct CanvasUniforms {
    canvasViewport: vec4<f32>,
    canvasLayer: mat4x4<f32>,
    canvasClip: vec4<f32>,
    keyColor: vec4<f32>,
};
@group(0) @binding(0) var<uniform> u: CanvasUniforms;
@group(1) @binding(0) var canvasSampler: sampler;
@group(1) @binding(1) var canvasTexture: texture_2d<f32>;

// The key-colour detector and ramp, identical to sprite.wgsl's - there is no
// include mechanism, so all three canvas shaders carry a copy and any change
// here has to be made in all three. sprite.wgsl carries the full rationale for
// why both the classification and the ramp itself are done in sRGB.
const keyChannelTolerance: f32 = 0.2;   // sRGB: how far R and B may differ
const keyGreenCutoff: f32 = 0.0331048;  // 0.2 in sRGB
const keyRampMidpoint: f32 = 0.5;       // sRGB: the intensity keyColor lands on

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
    @location(1) color: vec4<f32>,
    @location(2) uv: vec2<f32>,
};

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

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    if u.canvasViewport.z > 0.5 && (
        in.canvasPosition.x < u.canvasClip.x || in.canvasPosition.y < u.canvasClip.y ||
        in.canvasPosition.x > u.canvasClip.z || in.canvasPosition.y > u.canvasClip.w
    ) {
        discard;
    }
    let sampled = keyColorRamp(textureSample(canvasTexture, canvasSampler, in.uv), u.keyColor.rgb);
    return sampled * in.color;
}