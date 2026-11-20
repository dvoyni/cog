// canvas's include, "cog.canvas/canvas.wgsl": every binding of @group(0) and
// @group(2), and the functions over them. A 2D material's shader includes
// it, declares its material parameters at @group(1), and, when it takes a
// material argument, its struct Argument, which canvas preprocesses it with
// the define CANVAS_ARGUMENT for. A material with only a fragment entry
// point draws through the include's vertex stage, canvas_sprite_vertex,
// which a custom vertex stage replaces by naming its own.

// CanvasCamera is the pass parameters, set once per camera pass.
struct CanvasCamera {
	// world_to_clip maps the world to the viewport's clip space: the fit,
	// zoom, rotation and pivot in it. clip_to_world is its inverse, for an
	// effect that needs a fragment's world position.
	world_to_clip: mat3x2<f32>,
	clip_to_world: mat3x2<f32>,
	// pixel_size is one target pixel in world units.
	pixel_size: f32,
	// snap is the grid entries snap to, in world units: zero unless the
	// camera is pixel-perfect.
	snap: f32,
	// viewport_origin and viewport_size are in pixels, mapping a fragment's
	// @builtin(position) into its viewport.
	viewport_origin: vec2<f32>,
	viewport_size: vec2<f32>,
	// encode is whether canvas_output encodes sRGB: 1 where the target's
	// format does not, 0 where it does.
	encode: u32,
}

@group(0) @binding(0) var<uniform> canvas_camera: CanvasCamera;

// CanvasInstanceData is the data of one instance canvas draws, read at its
// instance_index: canvas's fixed part, then its material argument. The
// quad that fills an atlas's cell is no entry's, and canvas_pack_fragment
// reads its region, crop and part as it says.
struct CanvasInstanceData {
	// tint is premultiplied.
	tint: vec4<f32>,
	// region is the entry's rectangle in its atlas page, its origin in xy
	// and its size in zw, in texture coordinates.
	region: vec4<f32>,
	// crop is the part of the unit square an instance drawing all of it
	// draws, its top left in xy and its bottom right in zw: all of it,
	// (0, 0, 1, 1), but for a cropped sprite, which keeps its place.
	crop: vec4<f32>,
	// transform maps the unit square, the entry's quad, into the world.
	transform: mat3x2<f32>,
	page: u32,
	// part is which part of the unit square the instance draws, one of the
	// CANVAS_PART constants: the whole of it for every entry but a line and
	// a stroke's sides.
	part: u32,
	// size is the unit square's size in the entry's own space, which
	// transform maps into the world, and width and radius are in that
	// space: a stroke's or a line's width, zero a hairline one target pixel
	// wide, and the radius of a fill's or a stroke's corners, zero leaving
	// them square.
	size: vec2<f32>,
	width: f32,
	radius: f32,
#if CANVAS_ARGUMENT
	argument: Argument,
#endif
}

// The parts of the unit square an instance draws. CANVAS_PART_WHOLE draws
// all of it. CANVAS_PART_LINE draws a band across its middle, the line's
// width tall. A stroke's sides do not overlap: the top and the bottom span
// the square, the left and the right the height between them, each as
// thick as the stroke's width, or its radius where that is larger, so a
// rounded corner's curve lies within them.
const CANVAS_PART_WHOLE = 0u;
const CANVAS_PART_LINE = 1u;
const CANVAS_PART_STROKE_TOP = 2u;
const CANVAS_PART_STROKE_BOTTOM = 3u;
const CANVAS_PART_STROKE_LEFT = 4u;
const CANVAS_PART_STROKE_RIGHT = 5u;

@group(2) @binding(0) var<storage, read> canvas_instances: array<CanvasInstanceData>;
@group(2) @binding(1) var canvas_atlas: texture_2d_array<f32>;
@group(2) @binding(2) var canvas_sampler: sampler;

// CanvasVaryings is what every vertex stage writes, so a fragment-only
// material serves every entry.
struct CanvasVaryings {
	@builtin(position) position: vec4<f32>,
	// uv is in the atlas page, and uv_local from 0 to 1 over the entry's
	// region.
	@location(0) uv: vec2<f32>,
	@location(1) uv_local: vec2<f32>,
	// tint is premultiplied.
	@location(2) tint: vec4<f32>,
	// instance is the index of the fragment's instance in canvas_instances.
	@location(3) @interpolate(flat) instance: u32,
}

#if CANVAS_ARGUMENT
// canvas_argument answers the material argument of the instance in's
// fragment belongs to.
fn canvas_argument(in: CanvasVaryings) -> Argument {
	return canvas_instances[in.instance].argument;
}
#endif

// canvas_world_to_clip answers world, a point in the world, in clip space.
fn canvas_world_to_clip(world: vec2<f32>) -> vec4<f32> {
	return vec4<f32>(canvas_camera.world_to_clip * vec3<f32>(world, 1.0), 0.0, 1.0);
}

// canvas_snap answers translation, an entry's first corner in the world,
// moved onto the camera's grid, rounded half up, and as it is when the
// camera has none. Halves round alike, so an odd-sized entry centred on a
// whole unit moves a unit as its centre does, where WGSL's round, half to
// even, would hold it still every other unit. A vertex stage moves its
// whole entry by it, so a custom vertex stage that skips it draws its
// entries unsnapped.
fn canvas_snap(translation: vec2<f32>) -> vec2<f32> {
	if canvas_camera.snap <= 0.0 {
		return translation;
	}
	return floor(translation / canvas_camera.snap + 0.5) * canvas_camera.snap;
}

// canvas_width answers a stroke's or a line's width in its own space, across
// the sides x runs between and across those y runs between: its width, or
// a hairline's, of zero width, one target pixel at any zoom, measured where
// transform maps the unit square into the world.
fn canvas_width(instance_data: CanvasInstanceData) -> vec2<f32> {
	if instance_data.width > 0.0 {
		return vec2<f32>(instance_data.width);
	}
	let x_axis = instance_data.transform[0];
	let y_axis = instance_data.transform[1];
	// The distance between the square's opposite sides along one axis is
	// its area over the other axis's length.
	let area = max(abs(determinant(mat2x2<f32>(x_axis, y_axis))), 1e-30);
	return canvas_camera.pixel_size * vec2<f32>(length(y_axis), length(x_axis)) / area * instance_data.size;
}

// canvas_place_corner answers corner, a corner of the unit square, moved onto
// the corner of the part of the square instance_data draws: its crop of the
// whole square, or its part of it.
fn canvas_place_corner(instance_data: CanvasInstanceData, corner: vec2<f32>) -> vec2<f32> {
	let part = instance_data.part;
	if part == CANVAS_PART_WHOLE {
		return mix(instance_data.crop.xy, instance_data.crop.zw, corner);
	}
	let width = canvas_width(instance_data);
	let size = max(instance_data.size, vec2<f32>(1e-30));
	if part == CANVAS_PART_LINE {
		let half_band = 0.5 * width.y / size.y;
		return mix(vec2<f32>(0.0, 0.5 - half_band), vec2<f32>(1.0, 0.5 + half_band), corner);
	}
	// side_thickness is each side's thickness as a fraction of the square.
	let side_thickness = min(max(width, vec2<f32>(instance_data.radius)) / size, vec2<f32>(0.5));
	if part == CANVAS_PART_STROKE_TOP {
		return mix(vec2<f32>(0.0, 0.0), vec2<f32>(1.0, side_thickness.y), corner);
	}
	if part == CANVAS_PART_STROKE_BOTTOM {
		return mix(vec2<f32>(0.0, 1.0 - side_thickness.y), vec2<f32>(1.0, 1.0), corner);
	}
	if part == CANVAS_PART_STROKE_LEFT {
		return mix(vec2<f32>(0.0, side_thickness.y), vec2<f32>(side_thickness.x, 1.0 - side_thickness.y), corner);
	}
	return mix(vec2<f32>(1.0 - side_thickness.x, side_thickness.y), vec2<f32>(1.0, 1.0 - side_thickness.y), corner);
}

// canvas_rounded_distance answers point's signed distance from a rectangle
// centred on the origin, half_size from its centre to its sides, its
// corners rounded by radius: negative inside.
fn canvas_rounded_distance(point: vec2<f32>, half_size: vec2<f32>, radius: f32) -> f32 {
	// corner_offset is point's offset from its corner's curve's centre.
	let corner_offset = abs(point) - half_size + radius;
	return length(max(corner_offset, vec2<f32>(0.0))) + min(max(corner_offset.x, corner_offset.y), 0.0) - radius;
}

// canvas_shape_distance answers the signed distance of in's fragment from the
// edge of its shape, in the shape's own space, negative inside: from a
// fill's rectangle, a stroke's band inside it, or a line's band along its
// segment, a fill's and a stroke's corners rounded by their radius. An
// entry that is no shape answers its whole rectangle's.
fn canvas_shape_distance(in: CanvasVaryings) -> f32 {
	let instance_data = canvas_instances[in.instance];
	let half_size = instance_data.size * 0.5;
	let point = (in.uv_local - 0.5) * instance_data.size;
	let part = instance_data.part;
	if part == CANVAS_PART_LINE {
		return canvas_rounded_distance(point, vec2<f32>(half_size.x, 0.5 * canvas_width(instance_data).y), 0.0);
	}
	let radius = min(instance_data.radius, min(half_size.x, half_size.y));
	let outer = canvas_rounded_distance(point, half_size, radius);
	if part == CANVAS_PART_WHOLE {
		return outer;
	}
	let width = canvas_width(instance_data);
	let inner_half_size = half_size - width;
	if inner_half_size.x <= 0.0 || inner_half_size.y <= 0.0 {
		return outer;
	}
	return max(outer, -canvas_rounded_distance(point, inner_half_size, max(radius - max(width.x, width.y), 0.0)));
}

// canvas_covers answers whether in's fragment lies on its entry: always,
// but outside a rounded fill's or stroke's corners, and inside a rounded
// stroke's inner edge, which its sides reach, as canvas_shape_distance
// answers. A material discards the fragments it does not cover, after
// sampling; one that does not draws rounded shapes square.
fn canvas_covers(in: CanvasVaryings) -> bool {
	return canvas_instances[in.instance].radius <= 0.0 || canvas_shape_distance(in) <= 0.0;
}

// canvas_sample answers the entry's image at the fragment, premultiplied:
// its atlas page sampled at uv, whose texels canvas packed premultiplied, so
// filtering never darkens a cut-out edge.
fn canvas_sample(in: CanvasVaryings) -> vec4<f32> {
	return textureSample(canvas_atlas, canvas_sampler, in.uv, canvas_instances[in.instance].page);
}

// canvas_encode_srgb encodes linear colour as sRGB.
fn canvas_encode_srgb(linear: vec3<f32>) -> vec3<f32> {
	let low = linear * 12.92;
	let high = 1.055 * pow(linear, vec3<f32>(1.0 / 2.4)) - 0.055;
	return select(high, low, linear <= vec3<f32>(0.0031308));
}

// canvas_output answers color, premultiplied and linear, as the fragment's
// output: still premultiplied, its colour encoded as sRGB where the target's
// format does not encode it, so blending happens in the target's space. A
// colour of zero alpha, which adds under normal blending, is encoded as it
// is.
fn canvas_output(color: vec4<f32>) -> vec4<f32> {
	if canvas_camera.encode == 0u {
		return color;
	}
	let coverage = select(color.a, 1.0, color.a <= 0.0);
	return vec4<f32>(canvas_encode_srgb(color.rgb / coverage) * coverage, color.a);
}

// canvas_sprite_vertex is canvas's vertex stage for an entry drawn as a
// quad, six vertices per instance: the part of the unit square the instance
// draws, mapped by its transform and moved whole by canvas_snap, its
// coordinates spanning the instance's region over the whole square.
@vertex
fn canvas_sprite_vertex(@builtin(vertex_index) vertex: u32, @builtin(instance_index) instance: u32) -> CanvasVaryings {
	// The two triangles' corners, (0, 0), (1, 0), (0, 1), then (0, 1),
	// (1, 0), (1, 1): bit vertex of 0b110010 is x, of 0b101100 y.
	let corner = vec2<f32>(f32((0x32u >> vertex) & 1u), f32((0x2Cu >> vertex) & 1u));
	let instance_data = canvas_instances[instance];
	let placed_corner = canvas_place_corner(instance_data, corner);
	let linear = mat2x2<f32>(instance_data.transform[0], instance_data.transform[1]);
	var out: CanvasVaryings;
	out.position = canvas_world_to_clip(linear * placed_corner + canvas_snap(instance_data.transform[2]));
	out.uv = instance_data.region.xy + placed_corner * instance_data.region.zw;
	out.uv_local = placed_corner;
	out.tint = instance_data.tint;
	out.instance = instance;
	return out;
}

// canvas_viewport_vertex is canvas's vertex stage for a quad covering the
// camera's viewport, six vertices of one instance: clip space's corners,
// whatever the camera's projection, and never snapped, its coordinates
// spanning the instance's region from the viewport's top left. Only the
// instance's tint, region and page are read. canvas draws a camera's clear
// over its viewport through it, what a multisampled camera resolved, and an
// image filling its cell of an atlas.
@vertex
fn canvas_viewport_vertex(@builtin(vertex_index) vertex: u32, @builtin(instance_index) instance: u32) -> CanvasVaryings {
	// The corners of canvas_sprite_vertex's two triangles.
	let corner = vec2<f32>(f32((0x32u >> vertex) & 1u), f32((0x2Cu >> vertex) & 1u));
	let instance_data = canvas_instances[instance];
	var out: CanvasVaryings;
	out.position = vec4<f32>(corner.x * 2.0 - 1.0, 1.0 - corner.y * 2.0, 0.0, 1.0);
	out.uv = instance_data.region.xy + corner * instance_data.region.zw;
	out.uv_local = corner;
	out.tint = instance_data.tint;
	out.instance = instance;
	return out;
}
