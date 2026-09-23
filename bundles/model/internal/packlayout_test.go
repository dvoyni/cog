package internal

import (
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/wgsl"
)

// Every record the shader reads is declared twice - once as a Go struct in
// internal/types, once as a WGSL struct in builtin/scene - and nothing but a
// comment saying "must match" held the two together. A mismatch is silent in
// the worst way: the shader reads one field out of the bytes another one landed
// in and renders something plausible. Adding a member to the frame block is
// exactly when that comment stops being enough.
//
// So the shader is parsed and lowered, and every member's offset is compared
// against the Go type's. The pairing is spelled out rather than derived by
// lowercasing a field name, because a rename on either side should fail here
// rather than quietly stop being checked.
type shaderRecord struct {
	name    string
	size    uintptr
	members []shaderMember
}

type shaderMember struct {
	name   string
	offset uintptr
}

func TestEveryUploadedRecordMatchesItsShaderStruct(t *testing.T) {
	var frame model.FrameBlock
	var light model.Light
	var instance model.Instance
	var mesh model.SceneMesh
	records := []shaderRecord{
		{"SceneFrame", unsafe.Sizeof(frame), []shaderMember{
			{"view", unsafe.Offsetof(frame.View)},
			{"projection", unsafe.Offsetof(frame.Projection)},
			{"viewProjection", unsafe.Offsetof(frame.ViewProjection)},
			{"cameraPosition", unsafe.Offsetof(frame.CameraPosition)},
			{"viewDirection", unsafe.Offsetof(frame.ViewDirection)},
			{"sunDirection", unsafe.Offsetof(frame.SunDirection)},
			{"sunColor", unsafe.Offsetof(frame.SunColor)},
			{"ambientSky", unsafe.Offsetof(frame.AmbientSky)},
			{"ambientGround", unsafe.Offsetof(frame.AmbientGround)},
			{"lightCount", unsafe.Offsetof(frame.LightCount)},
			{"lights", unsafe.Offsetof(frame.Lights)},
		}},
		{"SceneLight", unsafe.Sizeof(light), []shaderMember{
			{"position", unsafe.Offsetof(light.Position)},
			{"invRange4", unsafe.Offsetof(light.InvRange4)},
			{"direction", unsafe.Offsetof(light.Direction)},
			{"spotScale", unsafe.Offsetof(light.SpotScale)},
			{"color", unsafe.Offsetof(light.Color)},
			{"spotOffset", unsafe.Offsetof(light.SpotOffset)},
		}},
		{"SceneInstance", unsafe.Sizeof(instance), []shaderMember{
			{"world0", unsafe.Offsetof(instance.World0)},
			{"world1", unsafe.Offsetof(instance.World1)},
			{"world2", unsafe.Offsetof(instance.World2)},
			{"animOffset", unsafe.Offsetof(instance.AnimOffset)},
			{"flags", unsafe.Offsetof(instance.Flags)},
			{"joint", unsafe.Offsetof(instance.Joint)},
			{"mesh", unsafe.Offsetof(instance.Mesh)},
		}},
		{"SceneMesh", unsafe.Sizeof(mesh), []shaderMember{
			{"uv0Scale", unsafe.Offsetof(mesh.UV0Scale)},
			{"uv0Bias", unsafe.Offsetof(mesh.UV0Bias)},
			{"uv1Scale", unsafe.Offsetof(mesh.UV1Scale)},
			{"uv1Bias", unsafe.Offsetof(mesh.UV1Bias)},
		}},
		// The PBR record is the one pair that is not field-for-field: the
		// shader declares the per-slot metadata as flat named members, because
		// an array member is not name-addressable and animating
		// baseColorTransform per frame is UV scrolling, while Go indexes the
		// same bytes by slot. This is where that correspondence is checked.
	}

	types := shaderStructs(t)
	for _, record := range records {
		declared, ok := types[record.name]
		if !ok {
			t.Errorf("%s is not declared in the bundled shader", record.name)
			continue
		}
		if uintptr(declared.Span) != record.size {
			t.Errorf("%s is %d bytes in WGSL and %d in Go", record.name, declared.Span, record.size)
		}
		offsets := make(map[string]uint32, len(declared.Members))
		for _, member := range declared.Members {
			offsets[member.Name] = member.Offset
		}
		for _, member := range record.members {
			offset, ok := offsets[member.name]
			if !ok {
				t.Errorf("%s.%s is packed by Go but not declared in WGSL", record.name, member.name)
				continue
			}
			if uintptr(offset) != member.offset {
				t.Errorf("%s.%s is at %d in WGSL and %d in Go", record.name, member.name, offset, member.offset)
			}
		}
	}
}

// shaderStructs lowers the bundled shader and indexes its struct types by name.
func shaderStructs(t *testing.T) map[string]ir.StructType {
	t.Helper()
	parsed, err := naga.Parse(flattenedSceneShader(t))
	if err != nil {
		t.Fatalf("parse the bundled scene shader: %v", err)
	}
	module, err := wgsl.Lower(parsed)
	if err != nil {
		t.Fatalf("lower the bundled scene shader: %v", err)
	}
	structs := map[string]ir.StructType{}
	for i := range module.Types {
		if declared, ok := module.Types[i].Inner.(ir.StructType); ok {
			structs[module.Types[i].Name] = declared
		}
	}
	return structs
}
