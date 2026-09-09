// The material a draw sourcing an arbitrary gfx texture gets: it samples and
// returns, and that is the whole difference from triangles.wgsl.
//
// triangles.wgsl runs every texel through the key-colour ramp, which rewrites
// any texel whose red and blue agree within 0.2 in sRGB and whose green is
// below 0.2 to grey at its own red intensity. That is what makes a sprite sheet
// wear a player colour, and it is silent damage to a rendered image: a dark
// warm shadow at linear (0.10, 0.02, 0.02) is keyed and leaves the shader
// neutral grey. No key colour switches it off, because the ramp's output is a
// function of red alone. Artwork wants the ramp; a render target is not
// artwork, so it gets this shader instead.
//
// It is an entry point, not an includable source. It declares no member of its
// own, so it includes the published uniform block rather than hand-writing one.
//#include builtin/canvas/uniforms.wgsl
//#include builtin/canvas/trianglesvertex.wgsl
//#include builtin/canvas/clip.wgsl

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
    if canvasClipped(in.canvasPosition) { discard; }
    return textureSample(canvasTexture, canvasSampler, in.uv) * in.color;
}
