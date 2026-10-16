// The tint ramp every built-in canvas shader that draws artwork applies.
//
// DECLARES: fn canvasTint; const canvasTintNeutral. No binding and no struct,
// so it is includable by any material, sprite and triangles alike.
//
// It is mounted at builtin/canvas/tint.wgsl, so a shader in a consuming app
// includes it by that absolute storage name and gets a ramp identical to the
// built-ins' rather than a re-typed approximation.

// canvasTintNeutral is the middle of the ramp in linear light: the grey the
// art paints its player-colour masks in, #6D6D6D, decoded. It is also the tint that
// leaves a texel as it was painted; canvas.DefaultTint is the same value, and it
// is what a draw naming no tint gets.
const canvasTintNeutral: f32 = 0.15292619;

// canvasTint runs a sampled texel through the tint ramp, lerp(0, tint / mid,
// texel) per channel with mid the neutral above: a texel painted #6D6D6D comes
// out in exactly the tint, black stays black, and white comes out at about
// 2.3 times the tint's brightness. So the neutral tint leaves art as painted, and
// canvas.TintFor(c) - c scaled by the neutral - draws white art in exactly c.
//
// It is one multiply in linear, where the texel and the tint already are: the
// midpoint is what carries the artist's #6D into linear, so no transfer
// function runs per fragment.
//
// Alpha is not ramped. It is coverage rather than colour, so tint.a scales it
// plainly, and 1 is still the alpha that leaves a texel as it was.
fn canvasTint(sampled: vec4<f32>, tint: vec4<f32>) -> vec4<f32> {
    return vec4<f32>(sampled.rgb * tint.rgb / canvasTintNeutral, sampled.a * tint.a);
}
