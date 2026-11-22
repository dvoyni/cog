// canvas's include, "cog.canvas/canvas.wgsl": every binding of @group(0) and
// @group(2), and the functions over them. A 2D material's shader includes
// it, declares its material parameters at @group(1), and, when it takes a
// material argument, its struct Argument, which canvas preprocesses it with
// the define CANVAS_ARGUMENT for. A material with only a fragment entry
// point draws through the include's vertex stage, canvas_sprite_vertex,
// which a custom vertex stage replaces by naming its own, and every
// material draws a mesh of canvas.Vertex through canvas_mesh_vertex.

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
	// slice_frame and slice_square are a sliced sprite's centre cell,
	// within its frame and within the unit square, each its left, top,
	// right and bottom edges as fractions, which canvas_sample maps the
	// square's cells onto the frame's by.
	slice_frame: vec4<f32>,
	slice_square: vec4<f32>,
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
	// them square. A glyph's width is its distance field's spread instead,
	// in field texels, half its font's field range, which a text material
	// turns the field's value into texels by: (0.5 - value) * 2 * width;
	// a bitmap font's glyph's is -1, its region its coverage, not a field;
	// an icon's, a picture placed in text, is zero, its region one of
	// canvas_sprite_atlas.
	size: vec2<f32>,
	width: f32,
	radius: f32,
	// decode is 1 where canvas_sample decodes the image's colour from sRGB
	// as it samples it: an image of a render texture marked sRGB whose
	// format encodes nothing. An atlas's format, or an sRGB texture's,
	// decodes it itself.
	decode: u32,
	// mode is how canvas_sample maps the unit square into the entry's
	// frame, one of the CANVAS_MODE constants.
	mode: u32,
	// tiles is how many of a tiled sprite's tiles span the unit square
	// along each axis, a glyph's pen as a point of its unit square
	// instead, and a mesh's axes its UVs repeat along, 1 along each its
	// image wraps along and 0 along the others.
	tiles: vec2<f32>,
#if CANVAS_ARGUMENT
	argument: Argument,
#endif
}

// The parts of the unit square an instance draws. CANVAS_PART_WHOLE draws
// all of it for an entry that is no shape, and CANVAS_PART_FILL all of it
// for a fill. CANVAS_PART_LINE draws a band across its middle, the line's
// width tall. A stroke's sides do not overlap: the top and the bottom span
// the square, the left and the right the height between them, each as
// thick as the stroke's width, or its radius where that is larger, so a
// rounded corner's curve lies within them.
const CANVAS_PART_WHOLE = 0u;
const CANVAS_PART_FILL = 1u;
const CANVAS_PART_LINE = 2u;
const CANVAS_PART_STROKE_TOP = 3u;
const CANVAS_PART_STROKE_BOTTOM = 4u;
const CANVAS_PART_STROKE_LEFT = 5u;
const CANVAS_PART_STROKE_RIGHT = 6u;

// canvas_is_shape answers whether instance_data draws a part of a shape: a
// fill, a stroke's side or a line, whose distance canvas_shape_distance
// answers from its rectangle, not from an image.
fn canvas_is_shape(instance_data: CanvasInstanceData) -> bool {
	return instance_data.part != CANVAS_PART_WHOLE;
}

// canvas_draws_whole answers whether instance_data draws its whole unit
// square: an entry that is no shape, or a fill.
fn canvas_draws_whole(instance_data: CanvasInstanceData) -> bool {
	return instance_data.part <= CANVAS_PART_FILL;
}

// The ways canvas_sample maps a point of the unit square into the entry's
// frame. CANVAS_MODE_STRETCHED maps the square onto the frame.
// CANVAS_MODE_SLICED maps it piecewise, slice_square's cells onto
// slice_frame's, so a sliced sprite's corners keep their size, and
// CANVAS_MODE_SLICED_HOLLOW leaves its centre cell empty.
// CANVAS_MODE_TILED maps it onto tiles of the frame, wrapped.
// CANVAS_MODE_MESH maps a mesh's UVs onto the frame, repeating along the
// axes its image wraps along and continuing its edge along the others.
const CANVAS_MODE_STRETCHED = 0u;
const CANVAS_MODE_SLICED = 1u;
const CANVAS_MODE_SLICED_HOLLOW = 2u;
const CANVAS_MODE_TILED = 3u;
const CANVAS_MODE_MESH = 4u;

@group(2) @binding(0) var<storage, read> canvas_instances: array<CanvasInstanceData>;
@group(2) @binding(1) var canvas_atlas: texture_2d_array<f32>;
@group(2) @binding(2) var canvas_sampler: sampler;
// canvas_sprite_atlas is the sRGB sprite atlas, which a text material draws
// a run's icons from beside the glyph atlas, its canvas_atlas, so a run
// with icons is one batch.
@group(2) @binding(3) var canvas_sprite_atlas: texture_2d_array<f32>;

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
	return canvas_pixel_width(instance_data);
}

// canvas_pixel_width answers one target pixel in instance_data's own space,
// across the sides x runs between and across those y runs between, at any
// zoom, measured where transform maps the unit square into the world: a
// hairline's width, and how far a distance must reach to be anti-aliased.
fn canvas_pixel_width(instance_data: CanvasInstanceData) -> vec2<f32> {
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
	if canvas_draws_whole(instance_data) {
		return mix(instance_data.crop.xy, instance_data.crop.zw, corner);
	}
	return canvas_place_grown_corner(instance_data, corner, vec2<f32>(0.0));
}

// canvas_place_grown_corner answers corner, a corner of the unit square,
// moved onto the corner of the part of the square a shape's instance_data
// draws, grown by grown, a fraction of the square along each axis: a fill's
// whole square, a line's band, or a stroke's side, each side's band grown
// both ways and the sides never overlapping, so what reaches inside a stroke
// draws once. canvas_place_corner places a shape's parts through it
// ungrown, and a vertex stage drawing past a shape, as the SDF material's
// does, grows them.
fn canvas_place_grown_corner(instance_data: CanvasInstanceData, corner: vec2<f32>, grown: vec2<f32>) -> vec2<f32> {
	let part = instance_data.part;
	let low = -grown;
	let high = vec2<f32>(1.0) + grown;
	if canvas_draws_whole(instance_data) {
		return mix(low, high, corner);
	}
	let width = canvas_width(instance_data);
	let size = max(instance_data.size, vec2<f32>(1e-30));
	if part == CANVAS_PART_LINE {
		let half_band = 0.5 * width.y / size.y + grown.y;
		return mix(vec2<f32>(low.x, 0.5 - half_band), vec2<f32>(high.x, 0.5 + half_band), corner);
	}
	// side_thickness is each side's thickness as a fraction of the square,
	// and near and far are where each side's band ends inside the square,
	// from its near edge and from its far one, meeting at its middle at
	// most.
	let side_thickness = min(max(width, vec2<f32>(instance_data.radius)) / size, vec2<f32>(0.5));
	let near = min(side_thickness + grown, vec2<f32>(0.5));
	let far = max(vec2<f32>(1.0) - side_thickness - grown, vec2<f32>(0.5));
	if part == CANVAS_PART_STROKE_TOP {
		return mix(low, vec2<f32>(high.x, near.y), corner);
	}
	if part == CANVAS_PART_STROKE_BOTTOM {
		return mix(vec2<f32>(low.x, far.y), high, corner);
	}
	if part == CANVAS_PART_STROKE_LEFT {
		return mix(vec2<f32>(low.x, near.y), vec2<f32>(near.x, far.y), corner);
	}
	return mix(vec2<f32>(far.x, near.y), vec2<f32>(high.x, far.y), corner);
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
	if canvas_draws_whole(instance_data) {
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
// but outside a rounded fill's or stroke's corners, inside a rounded
// stroke's inner edge, which its sides reach, as canvas_shape_distance
// answers, and within a hollow sliced sprite's centre. A material discards
// the fragments it does not cover, after sampling; one that does not draws
// rounded shapes square and fills a hollow centre with its own colour.
fn canvas_covers(in: CanvasVaryings) -> bool {
	let instance_data = canvas_instances[in.instance];
	if instance_data.mode == CANVAS_MODE_SLICED_HOLLOW && all(in.uv_local > instance_data.slice_square.xy) && all(in.uv_local < instance_data.slice_square.zw) {
		return false;
	}
	return instance_data.radius <= 0.0 || canvas_shape_distance(in) <= 0.0;
}

// canvas_sample answers the entry's image at the fragment, premultiplied
// and linear: a stretched entry's sampled at uv, a sliced one's at
// uv_local remapped piecewise, its centre transparent where it is hollow,
// a tiled one's at uv_local counted in tiles and wrapped within its
// region, whose gutter wraps along the axes its image tiles along, a
// hollow centre, which canvas_covers does not cover, sampling nothing, and
// a mesh's at uv_local wrapped within its region along the axes its image
// tiles along and held within it along the others. It
// is the one way a material samples an entry's image, since a material
// sampling uv itself stretches a sliced or tiled sprite. Its atlas page's
// texels canvas packed premultiplied, so filtering never darkens a cut-out
// edge, and a render texture's are premultiplied as a camera draws them,
// its colour decoded from sRGB where its instance says, after filtering.
// It samples with the gradients of the coordinate before wrapping, so a
// tile's seam reads the mip level its neighbours do, which a material
// calls it for in uniform control flow, before any discard.
fn canvas_sample(in: CanvasVaryings) -> vec4<f32> {
	let instance_data = canvas_instances[in.instance];
	let mode = instance_data.mode;
	let region = instance_data.region;
	// frame_point is the fragment's point of the frame, a tiled entry's
	// counted in tiles, and unwrapped its coordinate in the page before
	// wrapping, each worked out for the entry's mode alone.
	var frame_point = in.uv_local;
	var unwrapped = in.uv;
	if mode == CANVAS_MODE_TILED {
		frame_point = in.uv_local * instance_data.tiles;
		unwrapped = region.xy + frame_point * region.zw;
	} else if mode == CANVAS_MODE_SLICED || mode == CANVAS_MODE_SLICED_HOLLOW {
		unwrapped = region.xy + canvas_slice(in.uv_local, instance_data.slice_frame, instance_data.slice_square) * region.zw;
	}
	let uv_gradient_x = dpdx(unwrapped);
	let uv_gradient_y = dpdy(unwrapped);
	if mode == CANVAS_MODE_SLICED_HOLLOW && all(in.uv_local > instance_data.slice_square.xy) && all(in.uv_local < instance_data.slice_square.zw) {
		return vec4<f32>(0.0);
	}
	var uv = unwrapped;
	if mode == CANVAS_MODE_TILED {
		uv = region.xy + fract(frame_point) * region.zw;
	} else if mode == CANVAS_MODE_MESH {
		let held_point = clamp(frame_point, vec2<f32>(0.0), vec2<f32>(1.0));
		uv = region.xy + select(held_point, fract(frame_point), canvas_mesh_wraps(instance_data)) * region.zw;
	}
	let texel = textureSampleGrad(canvas_atlas, canvas_sampler, uv, instance_data.page, uv_gradient_x, uv_gradient_y);
	if instance_data.decode == 0u {
		return texel;
	}
	let coverage = select(texel.a, 1.0, texel.a <= 0.0);
	return vec4<f32>(canvas_decode_srgb(texel.rgb / coverage) * coverage, texel.a);
}

// canvas_slice answers point, a point of the unit square, mapped piecewise
// into the frame, both from 0 to 1: the square's cells, cut at
// square_edges, onto the frame's, cut at frame_edges, each the centre
// cell's left, top, right and bottom edges, so a corner keeps its size.
fn canvas_slice(point: vec2<f32>, frame_edges: vec4<f32>, square_edges: vec4<f32>) -> vec2<f32> {
	let tiny = vec2<f32>(1e-30);
	let leading = mix(vec2<f32>(0.0), frame_edges.xy, point / max(square_edges.xy, tiny));
	let middle = mix(frame_edges.xy, frame_edges.zw, (point - square_edges.xy) / max(square_edges.zw - square_edges.xy, tiny));
	let trailing = mix(frame_edges.zw, vec2<f32>(1.0), (point - square_edges.zw) / max(1.0 - square_edges.zw, tiny));
	return select(select(middle, trailing, point > square_edges.zw), leading, point < square_edges.xy);
}

// CANVAS_LARGEST_INNER bounds a band's inner, which Outline and the other
// constructors of a profile make the largest float32, so the ramp at its
// edge stays finite.
const CANVAS_LARGEST_INNER = 1e9;

// canvas_ramp answers 0 where amount is half a pixel below zero and 1 half a
// pixel above it, rising across it, pixel being how much amount changes
// over a pixel: an edge anti-aliased.
fn canvas_ramp(amount: f32, pixel: f32) -> f32 {
	return clamp(amount / pixel + 0.5, 0.0, 1.0);
}

// canvas_band_coverage answers how much of a fragment a band of a distance
// from an edge covers, as a profile describes it, distance positive outside
// the edge and changing by pixel over a pixel: from inner inside the edge
// to outer outside it, solid out to plateau, then falling off along
// (1 - t) to the power falloff, t running from 0 at plateau to 1 at outer,
// a falloff of zero a hard edge at outer, each edge anti-aliased.
fn canvas_band_coverage(inner: f32, outer: f32, plateau: f32, falloff: f32, distance: f32, pixel: f32) -> f32 {
	let inside = canvas_ramp(distance + min(inner, CANVAS_LARGEST_INNER), pixel);
	if falloff <= 0.0 {
		return inside * canvas_ramp(outer - distance, pixel);
	}
	// The fall spans at least a pixel, so even a band of no softness is
	// anti-aliased.
	let fall = clamp((distance - plateau) / max(outer - plateau, pixel), 0.0, 1.0);
	return inside * pow(max(1.0 - fall, 1e-30), falloff);
}

// canvas_find_unit_offset answers offset, a vector in the world, in the unit
// square, through the inverse of linear, which maps the unit square into
// the world, so a profile's offset falls the same way under any rotation or
// mirroring.
fn canvas_find_unit_offset(linear: mat2x2<f32>, offset: vec2<f32>) -> vec2<f32> {
	let area = determinant(linear);
	if abs(area) < 1e-30 {
		return vec2<f32>(0.0);
	}
	return vec2<f32>(linear[1].y * offset.x - linear[1].x * offset.y, linear[0].x * offset.y - linear[0].y * offset.x) / area;
}

// canvas_glyph_pen answers a glyph's pen, where its line's baseline meets its
// advance's start, as a point of its unit square, which instance_data's
// tiles carry, as only a tiled sprite's otherwise do.
fn canvas_glyph_pen(instance_data: CanvasInstanceData) -> vec2<f32> {
	return instance_data.tiles;
}

// canvas_glyph_is_icon answers whether instance_data, of a run of text, is
// an icon rather than a glyph: its width, a glyph's spread, is zero, and
// its region is in canvas_sprite_atlas.
fn canvas_glyph_is_icon(instance_data: CanvasInstanceData) -> bool {
	return instance_data.width == 0.0;
}

// canvas_glyph_is_bitmap answers whether instance_data, of a run of text, is
// a bitmap font's glyph, its region in canvas_atlas how much of each texel
// the glyph covers rather than a distance field: its width, a field's
// spread, is -1.
fn canvas_glyph_is_bitmap(instance_data: CanvasInstanceData) -> bool {
	return instance_data.width < 0.0;
}

// canvas_glyph_spread answers a glyph's field's spread, in field texels, half
// its font's field range, which instance_data's width carries: a field's
// value v lies (0.5 - v) * 2 * spread texels outside its glyph's edge. A
// bitmap glyph has no field, and answers -1.
fn canvas_glyph_spread(instance_data: CanvasInstanceData) -> f32 {
	return instance_data.width;
}

// canvas_mesh_wraps answers the axes a mesh's UVs repeat along, those its
// image wraps along, which instance_data's tiles carry, as only a tiled
// sprite's otherwise do.
fn canvas_mesh_wraps(instance_data: CanvasInstanceData) -> vec2<bool> {
	return instance_data.tiles > vec2<f32>(0.0);
}

// canvas_decode_srgb decodes colour encoded as sRGB into linear colour.
fn canvas_decode_srgb(encoded: vec3<f32>) -> vec3<f32> {
	let low = encoded / 12.92;
	let high = pow((encoded + 0.055) / 1.055, vec3<f32>(2.4));
	return select(high, low, encoded <= vec3<f32>(0.04045));
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

// canvas_quad_corner answers the corner of the unit square that vertex, one
// of a quad's six, lies on: the two triangles' corners, (0, 0), (1, 0),
// (0, 1), then (0, 1), (1, 0), (1, 1), bit vertex of 0b110010 its x and of
// 0b101100 its y.
fn canvas_quad_corner(vertex: u32) -> vec2<f32> {
	return vec2<f32>(f32((0x32u >> vertex) & 1u), f32((0x2Cu >> vertex) & 1u));
}

// canvas_make_quad_varyings answers the varyings of a vertex of instance, its
// data instance_data, at placed_corner, a point of its unit square or of a
// mesh's own space, mapped by its transform and moved whole by canvas_snap,
// its coordinates those of sampled_corner, the point of the square it
// samples or the mesh vertex's UV, over its region.
fn canvas_make_quad_varyings(instance: u32, instance_data: CanvasInstanceData, placed_corner: vec2<f32>, sampled_corner: vec2<f32>) -> CanvasVaryings {
	let linear = mat2x2<f32>(instance_data.transform[0], instance_data.transform[1]);
	var out: CanvasVaryings;
	out.position = canvas_world_to_clip(linear * placed_corner + canvas_snap(instance_data.transform[2]));
	out.uv = instance_data.region.xy + sampled_corner * instance_data.region.zw;
	out.uv_local = sampled_corner;
	out.tint = instance_data.tint;
	out.instance = instance;
	return out;
}

// canvas_sprite_vertex is canvas's vertex stage for an entry drawn as a
// quad, six vertices per instance: the part of the unit square the instance
// draws, mapped by its transform and moved whole by canvas_snap, its
// coordinates spanning the instance's region over the whole square.
@vertex
fn canvas_sprite_vertex(@builtin(vertex_index) vertex: u32, @builtin(instance_index) instance: u32) -> CanvasVaryings {
	let instance_data = canvas_instances[instance];
	let placed_corner = canvas_place_corner(instance_data, canvas_quad_corner(vertex));
	return canvas_make_quad_varyings(instance, instance_data, placed_corner, placed_corner);
}

// CanvasVertex is canvas.Vertex, canvas's standard vertex layout: its
// position in the mesh's own space, its UV from 0 to 1 over its image's
// region, and its colour, straight alpha, zero meaning white.
struct CanvasVertex {
	@location(0) position: vec2<f32>,
	@location(1) uv: vec2<f32>,
	@location(2) color: vec4<f32>,
}

// canvas_mesh_vertex is canvas's vertex stage for a mesh of canvas.Vertex,
// one instance of it per entry: each vertex's position mapped by its
// instance's transform, the translation moved by canvas_snap, the vertex
// itself unsnapped, so a mesh authored on the grid stays on it and a turned
// one moves whole; its UV mapped over the instance's region; and its
// colour, white for zero, premultiplied, times the instance's tint.
@vertex
fn canvas_mesh_vertex(vertex: CanvasVertex, @builtin(instance_index) instance: u32) -> CanvasVaryings {
	let color = select(vertex.color, vec4<f32>(1.0), all(vertex.color == vec4<f32>(0.0)));
	var out = canvas_make_quad_varyings(instance, canvas_instances[instance], vertex.position, vertex.uv);
	out.tint *= vec4<f32>(color.rgb * color.a, color.a);
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
	let corner = canvas_quad_corner(vertex);
	let instance_data = canvas_instances[instance];
	var out: CanvasVaryings;
	out.position = vec4<f32>(corner.x * 2.0 - 1.0, 1.0 - corner.y * 2.0, 0.0, 1.0);
	out.uv = instance_data.region.xy + corner * instance_data.region.zw;
	out.uv_local = corner;
	out.tint = instance_data.tint;
	out.instance = instance;
	return out;
}
