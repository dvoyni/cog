// canvas's SDF material, SDFMaterial: the distance from an entry's edge drawn
// through a profile, its argument, as a colour. A sprite's distance is read
// from its image, a distance field, and a shape's from its rectangle, its
// quad grown by the profile's reach. It draws the band alone, not the entry,
// so an outline, halo, glow or drop shadow is one entry drawn with it, then
// the entry itself drawn over it.
#include ./canvas.wgsl

// Argument is the profile, canvas's Profile: a band of the distance from the
// edge, positive outside it, measured in a sprite's field's texels or a
// shape's own space.
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
	// spread is the spread of a sprite's distance field, zero meaning 8,
	// which a shape ignores.
	spread: f32,
}

// sdf_default_spread is a profile's spread when it gives none, as a
// DistanceField's Spread of zero is.
const sdf_default_spread = 8.0;

// canvas_sdf_vertex is canvas_sprite_vertex for the SDF material: a sprite's
// quad as it is, its field baked larger, and a shape's grown by its
// profile's outer reach, the offset's length and a target pixel, its
// coordinates moved back by the offset, so the band falls offset in the
// world.
@vertex
fn canvas_sdf_vertex(@builtin(vertex_index) vertex: u32, @builtin(instance_index) instance: u32) -> CanvasVaryings {
	let corner = canvas_quad_corner(vertex);
	let instance_data = canvas_instances[instance];
	let profile = instance_data.argument;
	let offset = canvas_find_unit_offset(mat2x2<f32>(instance_data.transform[0], instance_data.transform[1]), profile.offset);
	var placed_corner: vec2<f32>;
	if canvas_is_shape(instance_data) {
		let shape_offset = abs(offset * instance_data.size);
		let reach = max(profile.outer, 0.0) + max(shape_offset.x, shape_offset.y);
		let grown = (vec2<f32>(reach) + canvas_pixel_width(instance_data)) / max(abs(instance_data.size), vec2<f32>(1e-30));
		placed_corner = canvas_place_grown_corner(instance_data, corner, grown);
	} else {
		placed_corner = canvas_place_corner(instance_data, corner);
	}
	return canvas_make_quad_varyings(instance, instance_data, placed_corner, placed_corner - offset);
}

// canvas_sdf_fragment draws the profile's band as its colour times the
// entry's tint: a sprite's distance its image's alpha, a distance field of
// the profile's spread, ½ on the edge, and nothing past its region, and a
// shape's its rectangle's, as canvas_shape_distance answers.
@fragment
fn canvas_sdf_fragment(in: CanvasVaryings) -> @location(0) vec4<f32> {
	let texel = canvas_sample(in);
	let instance_data = canvas_instances[in.instance];
	let profile = canvas_argument(in);
	let spread = select(profile.spread, sdf_default_spread, profile.spread <= 0.0);
	let within = all(in.uv_local >= vec2<f32>(0.0)) && all(in.uv_local <= vec2<f32>(1.0));
	let field_distance = (0.5 - select(0.0, texel.a, within)) * 2.0 * spread;
	let distance = select(field_distance, canvas_shape_distance(in), canvas_is_shape(instance_data));
	let pixel = max(fwidth(distance), 1e-6);
	return canvas_output(profile.color * in.tint * canvas_band_coverage(profile.inner, profile.outer, profile.plateau, profile.falloff, distance, pixel));
}
