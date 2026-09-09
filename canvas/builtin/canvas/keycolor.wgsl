// The key-colour detector and ramp, and the sRGB transfer functions it is
// written in.
//
// DECLARES: fn keyColorRamp; fn srgbEncode, srgbDecode, srgbEncode3,
// srgbDecode3; const keyChannelTolerance, keyGreenCutoff, keyRampMidpoint. No
// binding and no struct, so it is includable by extending and non-extending
// materials alike.
//
// Included by every built-in canvas shader that draws artwork - sprite.wgsl and
// triangles.wgsl - so the ramp is one declaration and a change to it cannot land
// in some copies and not others. texture.wgsl deliberately includes nothing from
// here: a render target is not artwork, and its header says why.
//
// It is mounted at builtin/canvas/keycolor.wgsl, so a shader in a consuming
// app includes it by that absolute storage name and gets a ramp identical to
// the built-ins' rather than a re-typed approximation.


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
