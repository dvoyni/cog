// A test 2D material's shader with a vertex stage of its own, for a mesh of
// the grass test's layout: each vertex's position moved along x by its
// sway times grass_wind's x, a material parameter, then placed through
// canvas's include as canvas_mesh_vertex places one, sampling the middle
// of the entry's image; and a fragment stage drawing that times the tint.
#include ../assets/canvas.wgsl

@group(1) @binding(0) var<uniform> grass_wind: vec4<f32>;

// GrassVertex is the grass test's vertex: its position in the mesh's own
// space and how far it sways, 0 at the root and 1 at the tip.
struct GrassVertex {
	@location(0) position: vec2<f32>,
	@location(1) sway: f32,
}

@vertex
fn grass_vertex(vertex: GrassVertex, @builtin(instance_index) instance: u32) -> CanvasVaryings {
	let swayed = vertex.position + vec2<f32>(vertex.sway * grass_wind.x, 0.0);
	return canvas_make_quad_varyings(instance, canvas_instances[instance], swayed, vec2<f32>(0.5));
}

@fragment
fn grass_fragment(in: CanvasVaryings) -> @location(0) vec4<f32> {
	return canvas_output(canvas_sample(in) * in.tint);
}
