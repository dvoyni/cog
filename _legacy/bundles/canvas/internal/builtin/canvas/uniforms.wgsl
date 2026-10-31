// The canvas uniform block, and nothing else.
//
// DECLARES: struct CanvasUniforms; @group(0) @binding(0) var<uniform> u.
// Every canvas material includes it: canvas sets u whole, once a batch, and the
// published vertex stages and clip test read it.
//
// The block is canvas's and is never extended. A custom material declares its
// own per-batch values as uniforms of their own in group 0, at any binding but
// 0, each set whole by the parameter that names it:
//
//     struct Lens { centre: vec2<f32>, radius: f32, strength: f32 };
//     @group(0) @binding(1) var<uniform> lens: Lens;
//     @group(0) @binding(2) var<uniform> fade: f32;
//
// set from Go with gfx.RawParameter("lens", value) - a Go struct mirroring Lens
// field for field - and gfx.FloatParam("fade", amount). The number is only the
// module's own: a parameter binds by the global's name, so two materials may
// put one value at different numbers.
//
// No other published source declares u, only reads it: WGSL is
// order-independent at module scope, so spritevertex.wgsl and clip.wgsl work
// wherever the material includes this.
struct CanvasUniforms {
    canvasViewport: vec4<f32>, // width, height, clipEnabled, unused
    canvasLayer: mat4x4<f32>,
    canvasClip: vec4<f32>,     // minX, minY, maxX, maxY
};
@group(0) @binding(0) var<uniform> u: CanvasUniforms;
