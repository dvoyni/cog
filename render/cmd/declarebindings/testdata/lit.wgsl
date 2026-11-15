// lit.wgsl draws a textured mesh, skinned by up to JOINT_COUNT joints when
// SKINNED is defined.

#include ./common.wgsl
#const JOINT_COUNT=2

@group(1) @binding(0) var albedo: texture_2d<f32>;
@group(1) @binding(1) var albedo_sampler: sampler;
#if SKINNED
@group(2) @binding(0) var<uniform> joints: array<mat4x4f, JOINT_COUNT>;
#endif

struct VertexInput {
	@location(0) position: vec3f,
#if SKINNED
	@location(1) weights: vec4f,
#endif
}

@vertex
fn vertex_main(input: VertexInput) -> @builtin(position) vec4f {
	var position = vec4f(input.position, 1);
#if SKINNED
	position = joints[0] * position * input.weights.x;
#endif
	return camera.view_projection * position;
}

@fragment
fn fragment_main() -> @location(0) vec4f {
	return textureSample(albedo, albedo_sampler, vec2f(0.5)) * camera.exposure;
}
