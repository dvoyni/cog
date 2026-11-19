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

// canvas_pack_fragment draws an image's texel, of straight alpha as its file
// holds it, premultiplied, into its cell of an atlas, whose format encodes
// it as its colour space asks.
@fragment
fn canvas_pack_fragment(in: CanvasVaryings) -> @location(0) vec4<f32> {
	let texel = textureSample(canvas_atlas, canvas_sampler, in.uv, canvas_instances[in.instance].page);
	return vec4<f32>(texel.rgb * texel.a, texel.a);
}
