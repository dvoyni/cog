package wgpu

import (
	"testing"

	cgfx "github.com/dvoyni/cog/gfx"
)

const testSceneWGSL = `
struct SceneLight {
	direction: vec4<f32>,
	colour: vec4<f32>,
};

struct SceneFrame {
	viewProjection: mat4x4<f32>,
	ambient: vec4<f32>,
	lights: array<SceneLight, 16>,
};

@group(0) @binding(0) var<storage, read> frame: SceneFrame;

@vertex
fn vs_main(@location(0) pos: vec3<f32>) -> @builtin(position) vec4<f32> {
	return frame.viewProjection * vec4<f32>(pos, 1.0);
}

@fragment
fn fs_main() -> @location(0) vec4<f32> {
	return frame.ambient + frame.lights[0].colour;
}
`

func TestStorageStructMembersAreReflected(t *testing.T) {
	layout, err := reflectShaderLayout(testSceneWGSL)
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}
	if len(layout.Resources) != 1 || !layout.Resources[0].StorageBuffer {
		t.Fatalf("resources = %+v, want one storage buffer", layout.Resources)
	}
	members := map[string]cgfx.StorageMember{}
	for _, member := range layout.Resources[0].Members {
		members[member.Name] = member
	}
	if got := members["viewProjection"]; got.Offset != 0 {
		t.Errorf("viewProjection = %+v, want offset 0", got)
	}
	if got := members["ambient"]; got.Offset != 64 {
		t.Errorf("ambient = %+v, want offset 64 (after the mat4)", got)
	}
	// An array of structs is the case that forces a stride: the reader needs
	// both where the array starts and how far apart its elements are.
	lights := members["lights"]
	if lights.Offset != 80 {
		t.Errorf("lights offset = %d, want 80", lights.Offset)
	}
	if lights.Stride != 32 || lights.Count != 16 {
		t.Errorf("lights = %+v, want 16 elements of stride 32", lights)
	}
}

const testTwoUniformBlocksWGSL = `
struct A { x: vec4<f32> };
struct B { y: vec4<f32> };
@group(0) @binding(0) var<uniform> a: A;
@group(0) @binding(1) var<uniform> b: B;

@vertex
fn vs_main() -> @builtin(position) vec4<f32> { return a.x + b.y; }

@fragment
fn fs_main() -> @location(0) vec4<f32> { return a.x; }
`

func TestSecondUniformBlockIsRejected(t *testing.T) {
	// Silently overwriting the first block misbehaves in a near-undiagnosable
	// way: every parameter lands at the wrong offset.
	if _, err := reflectShaderLayout(testTwoUniformBlocksWGSL); err == nil {
		t.Fatal("two uniform blocks were accepted, want an error")
	}
}

func TestUnreflectableShaderSourceIsAnError(t *testing.T) {
	// A shader whose bindings cannot be read is unusable, not degraded: nothing
	// would bind and every draw through it would render undefined.
	if _, err := reflectShaderLayout("this is not WGSL"); err == nil {
		t.Fatal("unparseable source was accepted, want an error")
	}
}
