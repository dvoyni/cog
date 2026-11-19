// canvas's quads over a camera's viewport, drawn through the include's
// canvas_viewport_vertex: a camera's clear, over the white texel, and what a
// multisampled camera resolved. What they sample is premultiplied already,
// so it is read as it is, times the tint.
#include ./canvas.wgsl

@fragment
fn canvas_viewport_fragment(in: CanvasVaryings) -> @location(0) vec4<f32> {
	let texel = textureSample(canvas_atlas, canvas_sampler, in.uv, canvas_instances[in.instance].page);
	return canvas_output(texel * in.tint);
}
