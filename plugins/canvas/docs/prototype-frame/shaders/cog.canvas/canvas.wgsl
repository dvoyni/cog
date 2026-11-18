// PROTOTYPE sketch of canvas's one include, "cog.canvas/canvas.wgsl": its
// public surface only, not compiled. It declares every binding of @group(0)
// and @group(2) and the functions over them; a 2D material's shader includes
// it, declares @group(1) and its Argument, and calls these.

// @group(0): the camera's pass parameters, set once per camera pass.
struct CanvasCamera {
    world_to_clip: mat3x2<f32>,   // the fit, zoom, rotation and pivot in it
    clip_to_world: mat3x2<f32>,
    pixel_size: f32,              // one target pixel in world units
    snap: f32,                    // the grid entries snap to, 0 unless pixel-perfect
    viewport_origin: vec2<f32>,   // in pixels
    viewport_size: vec2<f32>,
    encode: u32,                  // whether canvas_output encodes sRGB
}
@group(0) @binding(0) var<uniform> canvas_camera: CanvasCamera;

// @group(2): canvas's rows, one per entry, read at instance_index, and the
// atlases a batch samples.
struct CanvasRow {
    transform: mat3x2<f32>,
    tint: vec4<f32>,
    region: vec4<f32>,            // the image's region in its atlas page
    page: u32,
    // … the draw mode, slice insets, crop and the rest, the design's
#if CANVAS_ARGUMENT
    argument: Argument,           // the material's own struct, declared by its shader
#endif
}
@group(2) @binding(0) var<storage, read> canvas_rows: array<CanvasRow>;
@group(2) @binding(1) var canvas_atlas: texture_2d_array<f32>;
@group(2) @binding(2) var canvas_sampler: sampler;

// What every vertex stage writes, sprites' and meshes' alike, so a
// fragment-only material serves both.
struct CanvasVaryings {
    @builtin(position) position: vec4<f32>,
    @location(0) uv: vec2<f32>,                 // in the atlas
    @location(1) uv_local: vec2<f32>,           // 0 to 1 over the image's region
    @location(2) tint: vec4<f32>,               // premultiplied
    @location(3) @interpolate(flat) row: u32,    // the entry's row
}

fn canvas_row(instance: u32) -> CanvasRow { /* … */ }
fn canvas_argument(in: CanvasVaryings) -> Argument { /* canvas_rows[in.row].argument */ }

// world to clip, then snapped to the world-unit grid under a pixel-perfect
// camera: a custom vertex stage that skips it draws unsnapped.
fn canvas_world_to_clip(world: vec2<f32>) -> vec4<f32> { /* … */ }
fn canvas_snap(translation: vec2<f32>) -> vec2<f32> { /* … */ }

// A mesh's varyings from a local position, its UV over the image's region and
// its colour: the transform, snapping and the region remap.
fn canvas_mesh_varyings(row: CanvasRow, instance: u32, local: vec2<f32>, uv: vec2<f32>, color: vec4<f32>) -> CanvasVaryings { /* … */ }

// The one documented way to sample an entry's image: slicing, tiling, wrapping
// within its region, and the page.
fn canvas_sample(in: CanvasVaryings) -> vec4<f32> { /* … */ }

// A fragment's colour out: premultiplied, encoded to sRGB where the target's
// format does not.
fn canvas_output(color: vec4<f32>) -> vec4<f32> { /* … */ }
