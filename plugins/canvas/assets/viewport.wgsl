// canvas's quads over a viewport, drawn through the include's
// canvas_viewport_vertex: a camera's clear, over the white texel, and what a
// multisampled camera resolved, through canvas_viewport_fragment; and an
// image filling its cell of an atlas, through canvas_pack_fragment.
#include ./canvas.wgsl

// canvas_viewport_fragment draws what is premultiplied already, an atlas's
// texel or what a multisampled camera resolved, as it is, times the tint.
@fragment
fn canvas_viewport_fragment(in: CanvasVaryings) -> @location(0) vec4<f32> {
	let texel = textureSample(canvas_atlas, canvas_sampler, in.uv, canvas_instances[in.instance].page);
	return canvas_output(texel * in.tint);
}

// canvas_pack_fragment draws a frame's texel, read from its rectangle of its
// source, the atlas binding's one layer, of straight alpha as its file holds
// it, premultiplied, into its cell of an atlas, whose format encodes it as
// its colour space asks. Its instance is no entry's: its region spans the
// cell in the frame's texels from the frame's top left, its crop is the
// frame's rectangle of the source in texels, its top left in xy and its
// bottom right in zw, and its part is the axes the gutter wraps along, bit 0 x and bit 1
// y. Along an axis it wraps, the gutter holds the frame's opposite edge;
// along any other, its edge continued.
@fragment
fn canvas_pack_fragment(in: CanvasVaryings) -> @location(0) vec4<f32> {
	let instance_data = canvas_instances[in.instance];
	let size = vec2<i32>(instance_data.crop.zw - instance_data.crop.xy);
	let texel = vec2<i32>(floor(in.uv));
	let wrapped = ((texel % size) + size) % size;
	let clamped = clamp(texel, vec2<i32>(0), size - vec2<i32>(1));
	let wraps = vec2<bool>((instance_data.part & 1u) != 0u, (instance_data.part & 2u) != 0u);
	let color = textureLoad(canvas_atlas, vec2<i32>(instance_data.crop.xy) + select(clamped, wrapped, wraps), 0, 0);
	return vec4<f32>(color.rgb * color.a, color.a);
}
