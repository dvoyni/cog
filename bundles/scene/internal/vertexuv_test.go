package internal

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal/types"
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/libs/m"
)

// uvTriangle is the smallest standard-layout mesh carrying a UV range worth a
// record: one face whose TEXCOORD_0 tiles well outside 0..1, which is the case
// a half float loses four texels of.
func uvTriangle() []scene.Vertex {
	white := m.White
	return []scene.Vertex{
		{Position: m.Vec3{X: -1, Y: -1}, Normal: m.Vec3{Z: 1}, UV0: m.Vec2{X: 2.5, Y: -13.5}, Color: white},
		{Position: m.Vec3{X: 1, Y: -1}, Normal: m.Vec3{Z: 1}, UV0: m.Vec2{X: 18.5, Y: 0.5}, Color: white},
		{Position: m.Vec3{Y: 1}, Normal: m.Vec3{Z: 1}, UV0: m.Vec2{X: 10, Y: -6}, Color: white},
	}
}

// boundRecords reads back what one binding was bound to, as the bytes the
// backend was handed. Everything downstream of the arena is an offset and a
// size, so this is the only place a record scene packed is observable.
func boundRecords(t *testing.T, h *harness, name string) []byte {
	t.Helper()
	bound := h.backend.buffersBoundTo(name)
	if len(bound) == 0 {
		t.Fatalf("%s was never bound; a declared binding left unbound takes the frame down", name)
	}
	data := h.backend.baked[bound[0].buffer]
	if bound[0].size == 0 {
		return data[bound[0].offset:]
	}
	return data[bound[0].offset : bound[0].offset+bound[0].size]
}

func readMeshRecord(data []byte, index int) types.SceneMesh {
	at := data[index*meshRecordSize:]
	read := func(offset int) m.Vec2 {
		return m.Vec2{X: readFloat32(at[offset:]), Y: readFloat32(at[offset+4:])}
	}
	return types.SceneMesh{UV0Scale: read(0), UV0Bias: read(8), UV1Scale: read(16), UV1Bias: read(24)}
}

func readInstanceMesh(data []byte, index int) uint32 {
	var instance sceneInstance
	return binary.NativeEndian.Uint32(data[index*instanceSize+int(unsafe.Offsetof(instance.Mesh)):])
}

// The per-mesh buffer is bound on every draw, slot 0 holds the identity, and a
// mesh with a range of its own gets a slot after it which its instance names.
func TestAMeshWithAUVRangeNamesItsOwnSlotAfterTheIdentity(t *testing.T) {
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		ref := q.TemporaryMesh(uvTriangle(), nil, gpu.TopologyTriangleList)
		q.Mesh(0, ref, scene.MeshDraw{NeverCull: true})
	})
	h.frame()

	meshes := boundRecords(t, h, "sceneMeshes")
	if len(meshes) != 2*meshRecordSize {
		t.Fatalf("the per-mesh buffer holds %d bytes, want the identity and one record at %d each",
			len(meshes), meshRecordSize)
	}
	if slot0 := readMeshRecord(meshes, 0); slot0 != types.IdentityMesh {
		t.Fatalf("slot 0 is %+v, want the identity %+v", slot0, types.IdentityMesh)
	}
	_, _, want := types.PackVertices(new([]byte), uvTriangle())
	if got := readMeshRecord(meshes, 1); got != want {
		t.Fatalf("slot 1 is %+v, want the range the pack derived %+v", got, want)
	}
	if named := readInstanceMesh(boundRecords(t, h, "sceneInstances"), 0); named != 1 {
		t.Fatalf("the instance names slot %d, want the mesh's own slot 1", named)
	}
}

// A custom-layout mesh has no range scene could derive - it cannot find a UV
// inside a struct it does not know - so it names slot 0 and the buffer stays
// one record long.
func TestACustomLayoutMeshNamesTheIdentitySlot(t *testing.T) {
	custom := []customVertex{{Position: m.Vec3{X: -1, Y: -1}}, {Position: m.Vec3{X: 1, Y: -1}}, {}}
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		ref := q.TemporaryMesh(custom, nil, gpu.TopologyTriangleList)
		q.Mesh(0, ref, scene.MeshDraw{Material: opaqueMaterial(7), NeverCull: true})
	})
	h.frame()

	meshes := boundRecords(t, h, "sceneMeshes")
	if len(meshes) != meshRecordSize {
		t.Fatalf("the per-mesh buffer holds %d bytes, want the identity alone", len(meshes))
	}
	if slot0 := readMeshRecord(meshes, 0); slot0 != types.IdentityMesh {
		t.Fatalf("slot 0 is %+v, want the identity %+v", slot0, types.IdentityMesh)
	}
	if named := readInstanceMesh(boundRecords(t, h, "sceneInstances"), 0); named != 0 {
		t.Fatalf("the custom-layout draw names slot %d, want the identity at 0", named)
	}
}
