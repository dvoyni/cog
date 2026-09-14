// The one sprite material: the instanced atlas draw every sprite, glyph, inline
// icon and fill reaches the screen through. A lone sprite is its one-instance
// case; there is no second, single-sprite path.
//
// It is an ENTRY POINT, not an includable source: a material names it as its
// root, and an app that wants these pieces includes the published sources below
// instead. Its whole body is its includes and fs_main, which is the shape every
// custom sprite material has.
//#include builtin/canvas/uniforms.wgsl
//#include builtin/canvas/spritevertex.wgsl
//#include builtin/canvas/clip.wgsl
//#include builtin/canvas/keycolor.wgsl

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    if canvasClipped(in.canvasPosition) { discard; }
    let sampled = keyColorRamp(textureSample(canvasTexture, canvasSampler, in.uv, in.atlasLayer), in.keyColor.rgb);
    return sampled * in.tint;
}
