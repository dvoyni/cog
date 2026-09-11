package scene

import (
	"encoding/binary"
	"math"
	"testing"

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

// storedVertex reads one packed vertex back out of the arena, through the same
// offsets and formats the layout hands the fetch unit. It is what makes the
// tests below assertions about the buffer rather than about the packer's
// internals: every read here is the read a GPU makes.
type storedVertex struct {
	position m.Vec3
	normal   m.Vec3
	tangent  m.Vec4
	uv0, uv1 m.Vec2
	color    [4]uint8
	joints   [4]uint16
	weights  m.Vec4
}

// readStoredVertex reads one packed vertex back through the mesh's own record,
// because a stored UV means nothing without it: the codes are positions inside
// the range this bake derived.
func readStoredVertex(t *testing.T, packed []byte, index int, mesh sceneMesh) storedVertex {
	t.Helper()
	// An empty record is the one that names slot 0 at draw time, so reading one
	// back goes through the identity exactly as the shader would.
	mesh = mesh.packRecord()
	at := packed[index*storageStride:]
	stored := storedVertex{
		position: readVec3(at[storagePosition:]),
		normal: decodeStoredNormal(
			binary.NativeEndian.Uint16(at[storageNormal:]),
			binary.NativeEndian.Uint16(at[storageNormal+2:]),
		),
		tangent: decodeStoredTangent(binary.NativeEndian.Uint32(at[storageTangent:])),
		uv0:     readStoredUV(at[storageUV0:], mesh.UV0Scale, mesh.UV0Bias),
		uv1:     readStoredUV(at[storageUV1:], mesh.UV1Scale, mesh.UV1Bias),
		weights: readVec4(at[storageWeights:]),
	}
	copy(stored.color[:], at[storageColor:storageColor+4])
	for i := range stored.joints {
		stored.joints[i] = binary.NativeEndian.Uint16(at[storageJoints+i*2:])
	}
	return stored
}

func readFloat32(at []byte) float32 { return math.Float32frombits(binary.NativeEndian.Uint32(at)) }

// readStoredUV reads one stored UV set the way the fetch unit and
// sceneDecodeUV between them do: two unorm codes divided by 65535, scaled and
// biased by the mesh's record.
func readStoredUV(at []byte, scale, bias m.Vec2) m.Vec2 {
	return decodeUV([2]uint16{
		binary.NativeEndian.Uint16(at[0:]),
		binary.NativeEndian.Uint16(at[2:]),
	}, scale, bias)
}

// checkStoredUV reports a UV that came back further from what was authored than
// one code of its own range - which is what a mis-scaled, mis-biased or
// mis-offset UV looks like, where a correctly quantised one lands inside half
// a code.
func checkStoredUV(t *testing.T, what string, index int, set string, stored, authored, scale m.Vec2) {
	t.Helper()
	if abs32(stored.X-authored.X) > scale.X/uvCodeMax || abs32(stored.Y-authored.Y) > scale.Y/uvCodeMax {
		t.Errorf("%s vertex %d: %s %v, want %v within one code of the range %v",
			what, index, set, stored, authored, scale)
	}
}

func readVec3(at []byte) m.Vec3 {
	return m.Vec3{X: readFloat32(at[0:]), Y: readFloat32(at[4:]), Z: readFloat32(at[8:])}
}

func readVec4(at []byte) m.Vec4 {
	return m.Vec4{
		X: readFloat32(at[0:]), Y: readFloat32(at[4:]),
		Z: readFloat32(at[8:]), W: readFloat32(at[12:]),
	}
}

// What the pack writes, attribute by attribute, read back the way the fetch
// unit reads it. Four of the eight are the caller's own bytes at a new offset
// and must come back bit for bit; the normal and the tangent are encoded and
// come back as directions, and the two UV sets come back inside a step of the
// range this mesh's own record carries.
//
// The vertices are the ones with no two fields alike, so a pack that swapped
// two attributes or wrote one at the wrong offset cannot pass by accident.
func TestEveryStoredAttributeReadsBackAsWhatWasAuthored(t *testing.T) {
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
		at, _, record := packVertices(&arena, c.vertices)
		packed := at.of(arena)
		if len(packed) != len(c.vertices)*storageStride {
			t.Fatalf("%s packed %d bytes, want %d vertices at %d",
				c.what, len(packed), len(c.vertices), storageStride)
		}
		for i, authored := range c.vertices {
			stored := readStoredVertex(t, packed, i, record)
			// The four exact attributes. A position that moved would crack a
			// seam between two primitives, so it is not quantised at all.
			if stored.position != authored.Position {
				t.Errorf("%s vertex %d: position %v, want %v", c.what, i, stored.position, authored.Position)
			}
			// The two quantised ones. A UV is stored as a position inside the
			// mesh's own range, so what it owes is one code step of that range
			// - the range itself is what makes that step small.
			checkStoredUV(t, c.what, i, "uv0", stored.uv0, authored.UV0, record.packRecord().UV0Scale)
			checkStoredUV(t, c.what, i, "uv1", stored.uv1, authored.UV1, record.packRecord().UV1Scale)
			if stored.color != authored.Color || stored.joints != authored.Joints {
				t.Errorf("%s vertex %d: colour %v joints %v, want %v %v",
					c.what, i, stored.color, stored.joints, authored.Color, authored.Joints)
			}
			if stored.weights != authored.Weights {
				t.Errorf("%s vertex %d: weights %v, want %v", c.what, i, stored.weights, authored.Weights)
			}
			// The two encoded ones. Direction only: magnitude is divided out
			// by the encode and handedness is the tangent's w.
			if angle := angleBetween(stored.normal, authored.Normal); angle > 0.01 {
				t.Errorf("%s vertex %d: normal %v is %.5f degrees off the authored %v",
					c.what, i, stored.normal, angle, authored.Normal)
			}
			tangent := m.Vec3{X: stored.tangent.X, Y: stored.tangent.Y, Z: stored.tangent.Z}
			authoredTangent := m.Vec3{X: authored.Tangent.X, Y: authored.Tangent.Y, Z: authored.Tangent.Z}
			if angle := angleBetween(tangent, authoredTangent); angle > 0.02 {
				t.Errorf("%s vertex %d: tangent %v is %.5f degrees off the authored %v",
					c.what, i, tangent, angle, authoredTangent)
			}
			if want := signNotZero(authored.Tangent.W); stored.tangent.W != want {
				t.Errorf("%s vertex %d: handedness %v, want %v", c.what, i, stored.tangent.W, want)
			}
		}
	}
}

// The stride and every offset in it. WebGPU requires arrayStride to be a
// multiple of four unconditionally - a 30-byte stride runs on Vulkan, Metal on
// Apple silicon and D3D12 and fails on js/wasm, on GLES and on older Apple
// GPUs, which is green on a dev machine and broken in a browser - and gfx
// refuses the pipeline either way. Both named layouts satisfy it by
// construction, and this is where "by construction" is checked.
func TestTheStorageVertexIsFiftySixFourAlignedBytes(t *testing.T) {
	if storageStride != 56 || storageStride%4 != 0 {
		t.Errorf("the storage stride is %d, want 56 and a multiple of four", storageStride)
	}
	end := 0
	for _, attr := range []struct {
		name          string
		offset, bytes int
	}{
		{"position", storagePosition, 12},
		{"normal", storageNormal, 4},
		{"tangent", storageTangent, 4},
		{"uv0", storageUV0, 4},
		{"uv1", storageUV1, 4},
		{"color", storageColor, 4},
		{"joints", storageJoints, 8},
		{"weights", storageWeights, 16},
	} {
		if attr.offset%4 != 0 {
			t.Errorf("%s starts at %d, which is not 4-aligned", attr.name, attr.offset)
		}
		if attr.offset != end {
			t.Errorf("%s starts at %d, want %d - the storage vertex has no padding in it",
				attr.name, attr.offset, end)
		}
		end = attr.offset + attr.bytes
	}
	if end != storageStride {
		t.Errorf("the attributes end at %d, want the stride %d", end, storageStride)
	}
}

// The pack appends, because a mint stages its vertices into an arena that
// already holds this frame's other meshes.
func TestPackedVerticesLandAfterWhateverTheArenaAlreadyHeld(t *testing.T) {
	arena := []byte{0xAA, 0xBB, 0xCC}
	at, _, _ := packVertices(&arena, everyAttribute())
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
	_, sphere, _ := packVertices(&arena, vertices)
	if sphere.Center != (m.Vec3{X: 3, Y: 1, Z: 3}) {
		t.Fatalf("centre %v, want the box centre (3,1,3)", sphere.Center)
	}
	if want := float32(math.Sqrt(5)); !near(sphere.Radius, want) {
		t.Fatalf("radius %v, want half the box diagonal %v", sphere.Radius, want)
	}
	var empty []byte
	if at, sphere, record := packVertices(&empty, nil); at != (span{}) || sphere != (m.Sphere{}) || record != (sceneMesh{}) {
		t.Fatalf("no vertices packed %+v with sphere %v, want neither", at, sphere)
	}
}

// The glTF loader packs a model's converted vertices over the memory they are
// already in, so that a load never holds both forms of a primitive at once. The
// result has to be the bytes the arena path writes - the two are one packer -
// and the walk has to stay behind itself, which is the half that a wrong stride
// would break silently: a vertex overwritten before it is read comes back as
// whatever the previous vertex left there.
func TestPackingOverTheAuthoredVerticesWritesTheSameBytes(t *testing.T) {
	sphere, _ := unitSphereGeometry()
	vertices := append(everyAttribute(), sphere...)
	var arena []byte
	at, _, record := packVertices(&arena, vertices)
	want := append([]byte(nil), at.of(arena)...)

	// The same vertices again, because the pack consumes the slice it is given.
	consumed := append(everyAttribute(), sphere...)
	packed := packOverAuthored(consumed, record)
	if len(packed) != len(want) {
		t.Fatalf("packed %d bytes over the vertices, want %d", len(packed), len(want))
	}
	for i := range want {
		if packed[i] != want[i] {
			t.Fatalf("byte %d of vertex %d is %#02x, want %#02x",
				i%storageStride, i/storageStride, packed[i], want[i])
		}
	}
	if packed := packOverAuthored(nil, record); packed != nil {
		t.Errorf("no vertices packed %v, want nothing", packed)
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
	var arena []byte
	at, _, _ := packVertices(&arena, vertices)
	want := at.of(arena)
	if len(staged) != len(want) {
		t.Fatalf("staged %d vertex bytes, want %d", len(staged), len(want))
	}
	for i := range want {
		if staged[i] != want[i] {
			t.Fatalf("staged byte %d is %#02x, want %#02x", i, staged[i], want[i])
		}
	}
}
