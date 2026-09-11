package scene

import (
	"math"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// everyAttribute is a mesh whose every field is written with a value nothing
// else in it shares, so a pack that swapped two attributes, wrote one at the
// wrong offset or dropped one entirely cannot come out byte-identical by
// accident.
func everyAttribute() []Vertex {
	return []Vertex{
		{
			Position: m.Vec3{X: 1.5, Y: -2.25, Z: 3.125},
			Normal:   m.Vec3{X: 0.5, Y: -0.5, Z: 0.7071},
			Tangent:  m.Vec4{X: -1, Y: 0.25, Z: 0.75, W: -1},
			UV0:      m.Vec2{X: 0.125, Y: 0.875},
			UV1:      m.Vec2{X: 18.52, Y: -13.49},
			Color:    [4]uint8{1, 2, 3, 4},
			Joints:   [4]uint16{5, 6, 7, 8},
			Weights:  m.Vec4{X: 0.1, Y: 0.2, Z: 0.3, W: 0.4},
		},
		{
			Position: m.Vec3{X: -7, Y: 11, Z: 0.03125},
			Normal:   m.Vec3{X: -1},
			Tangent:  m.Vec4{X: 0, Y: 1, Z: 0, W: 1},
			UV0:      m.Vec2{X: 1, Y: 0},
			UV1:      m.Vec2{X: -0.5, Y: 2.5},
			Color:    [4]uint8{255, 254, 253, 252},
			Joints:   [4]uint16{65535, 0, 300, 23},
			Weights:  m.Vec4{X: 1},
		},
	}
}

// The expand step's whole claim: scene now writes the storage bytes itself
// rather than reinterpreting the caller's slice, and what it writes is what the
// reinterpret produced. Nothing narrows and nothing moves, so the two byte
// strings are equal - which is what makes the next three tickets "change one
// attribute's format" rather than "rewrite the upload path".
//
// This test dies with the last format change that leaves the layout as it is.
func TestPackedVerticesAreByteIdenticalToTheReinterpret(t *testing.T) {
	box, _ := unitBoxGeometry()
	sphere, _ := unitSphereGeometry()
	for _, c := range []struct {
		what     string
		vertices []Vertex
	}{
		{"every attribute", everyAttribute()},
		// Scene's own meshes take the same pack, and they are what every demo
		// draws that draws no model at all.
		{"the unit box", box},
		{"the unit sphere", sphere},
	} {
		var arena []byte
		at, _ := packVertices(&arena, c.vertices)

		packed := at.of(arena)
		want := uploadBytes(c.vertices)
		if len(packed) != len(want) {
			t.Fatalf("%s packed %d bytes, the reinterpret is %d", c.what, len(packed), len(want))
		}
		for i := range want {
			if packed[i] != want[i] {
				t.Fatalf("%s: byte %d of vertex %d is %#02x, want %#02x",
					c.what, i%storageStride, i/storageStride, packed[i], want[i])
			}
		}
	}
}

// The pack appends, because a mint stages its vertices into an arena that
// already holds this frame's other meshes.
func TestPackedVerticesLandAfterWhateverTheArenaAlreadyHeld(t *testing.T) {
	arena := []byte{0xAA, 0xBB, 0xCC}
	at, _ := packVertices(&arena, everyAttribute())
	if at.at != 3 || at.size != 2*storageStride {
		t.Fatalf("packed at %+v, want 3 + %d bytes", at, 2*storageStride)
	}
	if arena[0] != 0xAA || arena[1] != 0xBB || arena[2] != 0xCC {
		t.Fatal("the pack overwrote what the arena already held")
	}
	if len(arena) != 3+2*storageStride {
		t.Fatalf("the arena is %d bytes, want %d", len(arena), 3+2*storageStride)
	}
}

// The traversal that packs is the traversal that bounds: one pass over the
// vertices does both, rather than the pack being a second walk beside the one
// mintMesh already made. The sphere is the circumsphere of the positions' box,
// the same shape glTF's POSITION min/max gives a loaded primitive.
func TestPackingTheVerticesAlsoBoundsThem(t *testing.T) {
	vertices := []Vertex{
		{Position: m.Vec3{X: 1, Y: 2, Z: 3}},
		{Position: m.Vec3{X: 5, Y: 2, Z: 3}},
		{Position: m.Vec3{X: 3, Y: 0, Z: 3}},
	}
	var arena []byte
	_, sphere := packVertices(&arena, vertices)
	if sphere.Center != (m.Vec3{X: 3, Y: 1, Z: 3}) {
		t.Fatalf("centre %v, want the box centre (3,1,3)", sphere.Center)
	}
	if want := float32(math.Sqrt(5)); !near(sphere.Radius, want) {
		t.Fatalf("radius %v, want half the box diagonal %v", sphere.Radius, want)
	}
	var empty []byte
	if at, sphere := packVertices(&empty, nil); at != (span{}) || sphere != (m.Sphere{}) {
		t.Fatalf("no vertices packed %+v with sphere %v, want neither", at, sphere)
	}
}

// The storage offsets are written down rather than taken from the authoring
// struct, and today they still agree with it. That is the point of doing the
// divergence now: this test asserts the two are the same while they are, and is
// deleted by the first ticket that moves an attribute.
func TestTheStorageOffsetsStillAgreeWithTheAuthoringStruct(t *testing.T) {
	var vertex Vertex
	for _, c := range []struct {
		field   string
		storage int
		field_  uintptr
	}{
		{"Position", storagePosition, unsafe.Offsetof(vertex.Position)},
		{"Normal", storageNormal, unsafe.Offsetof(vertex.Normal)},
		{"Tangent", storageTangent, unsafe.Offsetof(vertex.Tangent)},
		{"UV0", storageUV0, unsafe.Offsetof(vertex.UV0)},
		{"UV1", storageUV1, unsafe.Offsetof(vertex.UV1)},
		{"Color", storageColor, unsafe.Offsetof(vertex.Color)},
		{"Joints", storageJoints, unsafe.Offsetof(vertex.Joints)},
		{"Weights", storageWeights, unsafe.Offsetof(vertex.Weights)},
	} {
		if uintptr(c.storage) != c.field_ {
			t.Errorf("%s is stored at %d and authored at %d", c.field, c.storage, c.field_)
		}
	}
	if uintptr(storageStride) != unsafe.Sizeof(vertex) {
		t.Errorf("the storage stride is %d and the authoring struct is %d bytes",
			storageStride, unsafe.Sizeof(vertex))
	}
}

// The bake path stages packed bytes, not a copy of the caller's slice. The
// staging arena is the only place that is observable: downstream of the drain
// the vertices are a buffer id and a size.
func TestBakeMeshStagesThePackedVertices(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) { q.Camera(testCamera, testCameraDescr()) })
	vertices := everyAttribute()
	ref := h.bake(vertices, nil, gfx.TopologyLineList)
	if ref.ID() == 0 {
		t.Fatal("the bake was refused")
	}

	var staged []byte
	h.kernel.ExecuteCommand[lookupProbeCmd](lookupProbeRequest{lookup: func(lookup *Lookup) {
		pending := lookup.pendingMeshes[len(lookup.pendingMeshes)-1]
		staged = append(staged, pending.vertices.of(lookup.staging)...)
	}})
	want := uploadBytes(vertices)
	if len(staged) != len(want) {
		t.Fatalf("staged %d vertex bytes, want %d", len(staged), len(want))
	}
	for i := range want {
		if staged[i] != want[i] {
			t.Fatalf("staged byte %d is %#02x, want %#02x", i, staged[i], want[i])
		}
	}
}
