// canvas's text material, TextMaterial: a run of text's glyphs, each a quad
// of its distance field in the glyph atlas, filled with the entry's tint,
// over a profile, its argument, drawn under the fill in the same fragment:
// an outline, a halo, a glow or a drop shadow, its distances in the font's
// field texels. Its zero profile draws the fill alone. A run's icons are
// quads of the sprite atlas, multiplied by their tint, with no profile.
#include ./canvas.wgsl

// Argument is the profile, canvas's Profile: a band of the distance from a
// glyph's edge, positive outside it, measured in the field's texels.
struct Argument {
	// inner is how far inside the edge the band reaches, and outer how far
	// outside it: solid out to plateau, then falling off to outer along
	// (1 - t) to the power falloff, zero falloff a hard edge at outer.
	inner: f32,
	outer: f32,
	plateau: f32,
	falloff: f32,
	// color is premultiplied: of zero alpha, it adds to what the target
	// holds, as a glow does.
	color: vec4<f32>,
	// offset moves the band in the world.
	offset: vec2<f32>,
	// spread is a sprite's field's, which a glyph ignores: its own is its
	// instance data's width.
	spread: f32,
}

// text_minified_texels is how many of a field's texels a pixel spans past
// which a glyph is small text: its pen placed on a whole pixel and its fill
// read at four points of each pixel, since one point a pixel can miss a
// stroke thinner than the texels between points.
const text_minified_texels = 2.0;

// canvas_text_vertex is canvas_sprite_vertex for the text material: a glyph's
// quad, and for small text, unturned, that quad moved so its pen lands on a
// whole pixel, as a bitmap glyph's is, so each glyph of a kind falls on the
// pixel grid alike, a whole number of pixels from its neighbours. An icon's
// quad is never moved, its region not one of the glyph atlas.
@vertex
fn canvas_text_vertex(@builtin(vertex_index) vertex: u32, @builtin(instance_index) instance: u32) -> CanvasVaryings {
	let instance_data = canvas_instances[instance];
	let corner = canvas_quad_corner(vertex);
	var out = canvas_make_quad_varyings(instance, instance_data, corner, corner);
	let transform = instance_data.transform;
	let linear = mat2x2<f32>(transform[0], transform[1]);
	let unturned = abs(transform[0].y) + abs(transform[1].x) <= 1e-6 * (abs(transform[0].x) + abs(transform[1].y));
	let field_texels = instance_data.region.z * f32(textureDimensions(canvas_atlas).x);
	let pixels = length(transform[0]) / canvas_camera.pixel_size;
	if unturned && field_texels > text_minified_texels * pixels && !canvas_glyph_is_icon(instance_data) {
		let pen = canvas_world_to_clip(linear * canvas_glyph_pen(instance_data) + canvas_snap(transform[2])).xy;
		let pen_pixel = vec2<f32>(pen.x * 0.5 + 0.5, 0.5 - pen.y * 0.5) * canvas_camera.viewport_size;
		let shift = floor(pen_pixel + 0.5) - pen_pixel;
		out.position = vec4<f32>(out.position.xy + shift / canvas_camera.viewport_size * vec2<f32>(2.0, -2.0), out.position.zw);
	}
	return out;
}

// text_find_fill_coverage answers how much of in's fragment the glyph's fill
// covers, its field's value turned into texels by spread, an edge
// anti-aliased over pixel texels, the field's texels a pixel spans: read at
// the fragment, or, for small text, the average of four points on a rotated
// grid across the pixel, each a quarter of it, its edge anti-aliased over
// half as many, uv_gradient_x and uv_gradient_y the field coordinate's
// change across a pixel.
fn text_find_fill_coverage(in: CanvasVaryings, uv_gradient_x: vec2<f32>, uv_gradient_y: vec2<f32>, spread: f32, pixel: f32) -> f32 {
	let instance_data = canvas_instances[in.instance];
	if pixel <= text_minified_texels {
		let value = textureSampleLevel(canvas_atlas, canvas_sampler, in.uv, instance_data.page, 0.0).r;
		return canvas_ramp(-(0.5 - value) * 2.0 * spread, pixel);
	}
	var points = array<vec2<f32>, 4>(vec2<f32>(-0.125, -0.375), vec2<f32>(0.375, -0.125), vec2<f32>(0.125, 0.375), vec2<f32>(-0.375, 0.125));
	var coverage = 0.0;
	for (var index = 0; index < 4; index++) {
		let point = in.uv + points[index].x * uv_gradient_x + points[index].y * uv_gradient_y;
		let value = textureSampleLevel(canvas_atlas, canvas_sampler, point, instance_data.page, 0.0).r;
		coverage += canvas_ramp(-(0.5 - value) * 2.0 * spread, 0.5 * pixel);
	}
	return 0.25 * coverage;
}

// canvas_text_fragment draws a glyph's fill, the entry's tint where its field
// lies inside the edge, over its profile's band, the profile's colour faded
// by the tint's alpha alone: each edge anti-aliased over the field's texels
// a pixel spans, found from the field coordinate's change across the pixel,
// which, unlike the distance's own, never cancels across a thin stroke, the
// field's value turned into texels by its spread, and the band read from
// the field moved back by the profile's offset, sampled again only when it
// has one, nothing past the glyph's quad. The glyph atlas has one mip
// level, so it is sampled at it. An icon is its region of the sprite atlas,
// sampled across its mips, multiplied by its tint.
@fragment
fn canvas_text_fragment(in: CanvasVaryings) -> @location(0) vec4<f32> {
	let instance_data = canvas_instances[in.instance];
	let profile = canvas_argument(in);
	let spread = canvas_glyph_spread(instance_data);
	let uv_gradient_x = dpdx(in.uv);
	let uv_gradient_y = dpdy(in.uv);
	if canvas_glyph_is_icon(instance_data) {
		return canvas_output(in.tint * textureSampleGrad(canvas_sprite_atlas, canvas_sampler, in.uv, instance_data.page, uv_gradient_x, uv_gradient_y));
	}
	let dimensions = vec2<f32>(textureDimensions(canvas_atlas).xy);
	let pixel = max(0.5 * (length(uv_gradient_x * dimensions) + length(uv_gradient_y * dimensions)), 1e-6);
	let fill_coverage = text_find_fill_coverage(in, uv_gradient_x, uv_gradient_y, spread, pixel);
	let fill_value = textureSampleLevel(canvas_atlas, canvas_sampler, in.uv, instance_data.page, 0.0).r;
	var band_distance = (0.5 - fill_value) * 2.0 * spread;
	if any(profile.offset != vec2<f32>(0.0)) {
		let offset = canvas_find_unit_offset(mat2x2<f32>(instance_data.transform[0], instance_data.transform[1]), profile.offset);
		let band_point = in.uv_local - offset;
		let band_value = textureSampleLevel(canvas_atlas, canvas_sampler, in.uv - offset * instance_data.region.zw, instance_data.page, 0.0).r;
		let within = all(band_point >= vec2<f32>(0.0)) && all(band_point <= vec2<f32>(1.0));
		band_distance = (0.5 - select(0.0, band_value, within)) * 2.0 * spread;
	}
	let fill = in.tint * fill_coverage;
	let band = profile.color * in.tint.a * canvas_band_coverage(profile.inner, profile.outer, profile.plateau, profile.falloff, band_distance, pixel);
	return canvas_output(fill + band * (1.0 - fill.a));
}
