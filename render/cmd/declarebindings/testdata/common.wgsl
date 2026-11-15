// common.wgsl is the camera every test shader includes.

struct Camera {
	view_projection: mat4x4f,
	exposure: f32,
}

@group(0) @binding(0) var<uniform> camera: Camera;
