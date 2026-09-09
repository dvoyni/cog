// The canvas uniform block, and nothing else.
//
// DECLARES: struct CanvasUniforms; @group(0) @binding(0) var<uniform> u.
// Include this only if you are NOT extending the block.
//
// A custom material declares its own per-batch parameters by APPENDING members
// to this struct, and WGSL has no way to add a member to a struct declared
// elsewhere. Include-once by resolved path means an extending material can
// never include this file, so it lives alone and an extending shader skips it
// and hand-writes these six lines with its own members after canvasClip:
//
//     struct CanvasUniforms {
//         canvasViewport: vec4<f32>,
//         canvasLayer: mat4x4<f32>,
//         canvasClip: vec4<f32>,
//         fade: f32,
//     };
//     @group(0) @binding(0) var<uniform> u: CanvasUniforms;
//
// That is why no other published source declares u, only reads it: WGSL is
// order-independent at module scope, so spritevertex.wgsl and clip.wgsl work
// against a block the app declared.
struct CanvasUniforms {
    canvasViewport: vec4<f32>, // width, height, clipEnabled, unused
    canvasLayer: mat4x4<f32>,
    canvasClip: vec4<f32>,     // minX, minY, maxX, maxY
};
@group(0) @binding(0) var<uniform> u: CanvasUniforms;
