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
// It declares the prefix of canvas's uniform block it actually reads. gfx
// resolves a recorder's parameters by name against the reflected layout and
// drops the ones a shader never declared, so leaving keyColor out costs
// nothing.
struct CanvasUniforms {
    canvasViewport: vec4<f32>,
    canvasLayer: mat4x4<f32>,
    canvasClip: vec4<f32>,
};
@group(0) @binding(0) var<uniform> u: CanvasUniforms;
@group(1) @binding(0) var canvasSampler: sampler;
@group(1) @binding(1) var canvasTexture: texture_2d<f32>;

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
    return textureSample(canvasTexture, canvasSampler, in.uv) * in.color;
}
