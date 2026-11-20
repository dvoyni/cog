// A test 2D material's shader taking an argument, a fragment stage alone,
// which compiles only with CANVAS_ARGUMENT, as canvas preprocesses it: an
// entry's image times its tint times its argument's colour, dissolved from
// its left edge by its argument's amount.
#include ../assets/canvas.wgsl

struct Argument {
	// amount is how much of the entry's width is dissolved, from its left
	// edge: 0 none of it, 1 all of it.
	amount: f32,
	// colour is premultiplied, at offset 16, past 12 bytes of padding.
	colour: vec4<f32>,
}

@fragment
fn dissolve_fragment(in: CanvasVaryings) -> @location(0) vec4<f32> {
	let color = canvas_sample(in) * in.tint;
	let argument = canvas_argument(in);
	if !canvas_covers(in) || in.uv_local.x < argument.amount {
		discard;
	}
	return canvas_output(color * argument.colour);
}
