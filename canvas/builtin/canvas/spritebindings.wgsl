// Everything a sprite material binds, and the shape of one sprite's data.
//
// DECLARES: @group(1) @binding(0) canvasSampler; @group(1) @binding(1)
// canvasTexture: texture_2d_array<f32>; @group(2) @binding(0) instances;
// struct SpriteInstance; struct Instances; struct VertexOut.
//
// Do not redeclare any of those. A duplicate top-level declaration is a WGSL
// compile error; a duplicated *binding* is worse, because CreateBindGroup
// rejects the short list, the draw encodes with no bind group, and the frame is
// simply wrong with nothing reported.
//
// Groups are numbered by what a binding IS, not by how often it changes: 0 the
// uniform block, 1 the texture a draw samples, 2 per-sprite storage. So
// canvasTexture and canvasSampler - public API names - have one address in every
// canvas shader, sprite and triangles alike.
//
// An app declaring its own per-instance parameter arrays puts them at
// @group(2) @binding(1) and up, read with the same instance index the record
// uses. Nothing else in group 2 is declared here, so @binding(1) redeclares
// nothing. There are seven such bindings available: the WebGPU floor is eight
// storage buffers per stage and instances takes one.
//
//     struct Wobble { data: array<f32> };
//     @group(2) @binding(1) var<storage, read> wobble: Wobble;
//
// SpriteInstance is FROZEN: six vec4 at 16-byte offsets, 96 bytes, no padding.
// It is hand-mirrored by canvas.SpriteInstance in Go and uploaded by direct
// reinterpretation, so changing it here is a silent misread rather than a
// compile error. Extending per-sprite data is not something a material may do.
@group(1) @binding(0) var canvasSampler: sampler;
@group(1) @binding(1) var canvasTexture: texture_2d_array<f32>;

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

@group(2) @binding(0) var<storage, read> instances: Instances;

struct VertexOut {
    @builtin(position) position: vec4<f32>,
    @location(0) canvasPosition: vec2<f32>,
    @location(1) uv: vec2<f32>,
    @location(2) @interpolate(flat) atlasLayer: i32,
    @location(3) tint: vec4<f32>,
    @location(4) keyColor: vec4<f32>,
};
