// A test 2D material's shader, a fragment stage alone, so canvas's vertex
// stage draws sprites and shapes with it: an entry's image times its tint
// times material_tint, a material parameter, on the fragments it covers.
#include ../assets/canvas.wgsl

@group(1) @binding(0) var<uniform> material_tint: vec4<f32>;

@fragment
fn tint_fragment(in: CanvasVaryings) -> @location(0) vec4<f32> {
	let color = canvas_sample(in) * in.tint * material_tint;
	if !canvas_covers(in) {
		discard;
	}
	return canvas_output(color);
}
