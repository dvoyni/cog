// canvas's default material, the zero Material: an entry's image times its
// tint, on the fragments it covers. Shapes draw with it over a white texel.
#include ./canvas.wgsl

@fragment
fn canvas_default_fragment(in: CanvasVaryings) -> @location(0) vec4<f32> {
	let color = canvas_sample(in) * in.tint;
	if !canvas_covers(in) {
		discard;
	}
	return canvas_output(color);
}
