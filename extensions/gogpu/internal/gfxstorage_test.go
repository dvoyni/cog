package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx"
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
	if len(layout.Resources) != 1 || layout.Resources[0].Kind.Base() != gfx.ResourceStorageBuffer {
		t.Fatalf("resources = %+v, want one storage buffer", layout.Resources)
	}
	members := map[string]gfx.StorageMember{}
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

// Each uniform block is its own resource at its own binding, with its own
// members at offsets from its own start: a second block neither overwrites the
// first nor shifts its members.
func TestEveryUniformBlockIsReflected(t *testing.T) {
	layout, err := reflectShaderLayout(testTwoUniformBlocksWGSL)
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}
	blocks := uniformBlocks(layout)
	if len(blocks) != 2 {
		t.Fatalf("uniform blocks = %+v, want a and b", blocks)
	}
	for i, want := range []struct{ name, member string }{{"a", "x"}, {"b", "y"}} {
		block := blocks[i]
		if block.Name != want.name || block.Group != 0 || block.Binding != i || block.Size != 16 {
			t.Errorf("block %d = %+v, want %s at 0/%d, 16 bytes", i, block, want.name, i)
		}
		if len(block.Members) != 1 || block.Members[0].Name != want.member || block.Members[0].Offset != 0 {
			t.Errorf("block %s members = %+v, want %s at 0", want.name, block.Members, want.member)
		}
	}
}

func TestUnreflectableShaderSourceIsAnError(t *testing.T) {
	// A shader whose bindings cannot be read is unusable, not degraded: nothing
	// would bind and every draw through it would render undefined.
	if _, err := reflectShaderLayout("this is not WGSL"); err == nil {
		t.Fatal("unparseable source was accepted, want an error")
	}
}
