// PROTOTYPE: the grass material's shader, for a mesh of the game's own
// layout, GrassVertex. Its vertex stage moves vertices, so the material
// declares a CullMargin.
#include "cog.canvas/canvas.wgsl"

struct Argument {
    strength: f32,
    phase: f32,
}

// Set with SetMaterialUniform by whichever task owns the game's clock.
@group(1) @binding(0) var<uniform> time: f32;

struct GrassVertex {
    @location(0) position: vec2<f32>,
    @location(1) sway: f32,
}

@vertex
fn grass_vertex(vertex: GrassVertex, @builtin(instance_index) instance: u32) -> CanvasVaryings {
    let row = canvas_row(instance);
    let bend = sin(time * 2.0 + row.argument.phase) * row.argument.strength * vertex.sway;
    return canvas_mesh_varyings(row, instance, vertex.position + vec2(bend, 0.0), vec2(0.5, 1.0 - vertex.sway), vec4(1.0));
}

@fragment
fn grass_fragment(in: CanvasVaryings) -> @location(0) vec4<f32> {
    return canvas_output(canvas_sample(in) * in.tint);
}
