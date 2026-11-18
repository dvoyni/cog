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
// instance_index: canvas's fixed part, then its material argument.
struct CanvasInstanceData {
	// tint is premultiplied.
	tint: vec4<f32>,
	// region is the entry's rectangle in its atlas page, its origin in xy
	// and its size in zw, in texture coordinates.
	region: vec4<f32>,
	// transform maps the unit square, the entry's quad, into the world.
	transform: mat3x2<f32>,
	page: u32,
#if CANVAS_ARGUMENT
	argument: Argument,
#endif
}

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
// moved onto the camera's grid, and as it is when the camera has none. A
// vertex stage moves its whole entry by it, so a custom vertex stage that
// skips it draws its entries unsnapped.
fn canvas_snap(translation: vec2<f32>) -> vec2<f32> {
	if canvas_camera.snap <= 0.0 {
		return translation;
	}
	return round(translation / canvas_camera.snap) * canvas_camera.snap;
}

// canvas_sample answers the entry's image at the fragment: its atlas page
// sampled at uv.
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
// quad, one instance per entry and six vertices per instance: the unit
// square mapped by the instance's transform, moved whole by canvas_snap,
// its coordinates spanning the instance's region.
@vertex
fn canvas_sprite_vertex(@builtin(vertex_index) vertex: u32, @builtin(instance_index) instance: u32) -> CanvasVaryings {
	// The two triangles' corners, (0, 0), (1, 0), (0, 1), then (0, 1),
	// (1, 0), (1, 1): bit vertex of 0b110010 is x, of 0b101100 y.
	let corner = vec2<f32>(f32((0x32u >> vertex) & 1u), f32((0x2Cu >> vertex) & 1u));
	let instance_data = canvas_instances[instance];
	let linear = mat2x2<f32>(instance_data.transform[0], instance_data.transform[1]);
	var out: CanvasVaryings;
	out.position = canvas_world_to_clip(linear * corner + canvas_snap(instance_data.transform[2]));
	out.uv = instance_data.region.xy + corner * instance_data.region.zw;
	out.uv_local = corner;
	out.tint = instance_data.tint;
	out.instance = instance;
	return out;
}
