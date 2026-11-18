// canvas's default material, the zero Material: an entry's image times its
// tint. Shapes draw with it over a white texel.
#include ./canvas.wgsl

@fragment
fn canvas_default_fragment(in: CanvasVaryings) -> @location(0) vec4<f32> {
	return canvas_output(canvas_sample(in) * in.tint);
}
