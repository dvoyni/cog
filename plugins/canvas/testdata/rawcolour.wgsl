// A test 2D material's shader, a fragment stage alone, writing raw_colour,
// a material parameter, premultiplied, as it is: unsampled and unencoded,
// so what its blending makes of it is read from the target's bytes.
#include ../assets/canvas.wgsl

@group(1) @binding(0) var<uniform> raw_colour: vec4<f32>;

@fragment
fn raw_colour_fragment(in: CanvasVaryings) -> @location(0) vec4<f32> {
	return raw_colour;
}
