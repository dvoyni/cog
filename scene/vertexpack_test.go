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
			Color:    m.NewColorLinear(1.0/255, 2.0/255, 3.0/255, 4.0/255),
		},
		{
			Position: m.Vec3{X: -7, Y: 11, Z: 0.03125},
			Normal:   m.Vec3{X: -1},
			Tangent:  m.Vec4{X: 0, Y: 1, Z: 0, W: 1},
			UV0:      m.Vec2{X: 1, Y: 0},
			UV1:      m.Vec2{X: -0.5, Y: 2.5},
			Color:    m.NewColorLinear(1, 254.0/255, 253.0/255, 252.0/255),
		},
	}
}

// everySkinnedAttribute is the same mesh as the loader converts it: every
// standard attribute distinct, and a joint and a weight set no two vertices
// share. Only the glTF loader can build one, which is the whole point of the
// type - so this is also the only fixture that can exercise the skinned layout
// at all.
func everySkinnedAttribute() []skinnedVertex {
	standard := everyAttribute()
	return []skinnedVertex{
		{
			Vertex:  standard[0],
			Joints:  [4]uint16{5, 6, 7, 8},
			Weights: m.Vec4{X: 0.1, Y: 0.2, Z: 0.3, W: 0.4},
		},
		{
			Vertex: standard[1],
			// 255 is the largest joint a byte holds and the cap the loader
			// rejects a skin for exceeding, so it belongs in the fixture.
			Joints:  [4]uint16{255, 0, 137, 23},
			Weights: m.Vec4{X: 1},
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
	color    m.Color
}

// readStoredVertex reads one packed vertex's six shared rows back through the
// mesh's own record, because a stored UV means nothing without it: the codes
// are positions inside the range this bake derived. stride says which of the
// two named layouts the buffer was written at; the rows this reads are the ones
// both of them share.
func readStoredVertex(t *testing.T, packed []byte, index, stride int, mesh sceneMesh) storedVertex {
	t.Helper()
	// An empty record is the one that names slot 0 at draw time, so reading one
	// back goes through the identity exactly as the shader would.
	mesh = mesh.packRecord()
	at := packed[index*stride:]
	return storedVertex{
		position: readVec3(at[storagePosition:]),
		normal: decodeStoredNormal(
			binary.NativeEndian.Uint16(at[storageNormal:]),
			binary.NativeEndian.Uint16(at[storageNormal+2:]),
		),
		tangent: decodeStoredTangent(binary.NativeEndian.Uint32(at[storageTangent:])),
		uv0:     readStoredUV(at[storageUV0:], mesh.UV0Scale, mesh.UV0Bias),
		uv1:     readStoredUV(at[storageUV1:], mesh.UV1Scale, mesh.UV1Bias),
		color:   readStoredColor(at[storageColor:]),
	}
}

// readStoredColor reads the colour the way the fetch unit does for a Unorm8x4:
// a byte a channel, divided by 255, and linear because glTF's COLOR_0 is.
func readStoredColor(at []byte) m.Color {
	return m.NewColorLinear(
		float32(at[0])/unorm8CodeMax, float32(at[1])/unorm8CodeMax,
		float32(at[2])/unorm8CodeMax, float32(at[3])/unorm8CodeMax)
}

// readStoredJoints reads the four joint indices of a skinned-layout vertex the
// way the fetch unit does for a Uint8x4.
func readStoredJoints(packed []byte, index int) [4]uint16 {
	at := packed[index*storageSkinnedStride:]
	var joints [4]uint16
	for i := range joints {
		joints[i] = uint16(at[storageJoints+i])
	}
	return joints
}

// readStoredWeights reads the four influences of a skinned-layout vertex the
// way the fetch unit does for a Unorm8x4: a byte each, divided by 255. What the
// shader then makes of them is deform.wgsl's business - it divides the deformed
// position by their total, because these four do not sum to one and nothing at
// bake can make them.
func readStoredWeights(packed []byte, index int) m.Vec4 {
	at := packed[index*storageSkinnedStride+storageWeights:]
	return m.Vec4{
		X: float32(at[0]) / unorm8CodeMax, Y: float32(at[1]) / unorm8CodeMax,
		Z: float32(at[2]) / unorm8CodeMax, W: float32(at[3]) / unorm8CodeMax,
	}
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

// What the pack writes, attribute by attribute, read back the way the fetch
// unit reads it. The position is the caller's own bytes at a new offset and
// must come back bit for bit; the normal and the tangent are encoded and come
// back as directions, the two UV sets come back inside a step of the range this
// mesh's own record carries, and the colour inside one eight-bit code.
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
			stored := readStoredVertex(t, packed, i, storageStride, record)
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
			// The colour is quantised now that it is authored as linear
			// floats, so what it owes is one eight-bit code - which is what a
			// file's own byte colour round-trips to exactly.
			if !nearColor(stored.color, authored.Color) {
				t.Errorf("%s vertex %d: colour %v, want %v within a code",
					c.what, i, stored.color, authored.Color)
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

// nearWeights reports whether four stored influences are each within one
// eight-bit code of what was authored.
func nearWeights(stored, authored m.Vec4) bool {
	return abs32(stored.X-authored.X) <= 1.0/unorm8CodeMax &&
		abs32(stored.Y-authored.Y) <= 1.0/unorm8CodeMax &&
		abs32(stored.Z-authored.Z) <= 1.0/unorm8CodeMax &&
		abs32(stored.W-authored.W) <= 1.0/unorm8CodeMax
}

// nearColor reports whether a stored colour's four channels are each within one
// eight-bit code of what was authored.
func nearColor(stored, authored m.Color) bool {
	return nearWeights(
		m.Vec4{X: stored.R, Y: stored.G, Z: stored.B, W: stored.A},
		m.Vec4{X: authored.R, Y: authored.G, Z: authored.B, W: authored.A})
}

// Both strides and every offset in them. WebGPU requires arrayStride to be a
// multiple of four unconditionally - a 30-byte stride runs on Vulkan, Metal on
// Apple silicon and D3D12 and fails on js/wasm, on GLES and on older Apple
// GPUs, which is green on a dev machine and broken in a browser - and gfx
// refuses the pipeline either way. Both named layouts satisfy it by
// construction, and this is where "by construction" is checked.
func TestBothNamedLayoutsAreFourAlignedAndUnpadded(t *testing.T) {
	if storageStride != 32 || storageStride%4 != 0 {
		t.Errorf("the standard stride is %d, want 32 and a multiple of four", storageStride)
	}
	if storageSkinnedStride != 40 || storageSkinnedStride%4 != 0 {
		t.Errorf("the skinned stride is %d, want 40 and a multiple of four", storageSkinnedStride)
	}
	end := 0
	for at, attr := range []struct {
		name          string
		offset, bytes int
	}{
		{"position", storagePosition, 12},
		{"normal", storageNormal, 4},
		{"tangent", storageTangent, 4},
		{"uv0", storageUV0, 4},
		{"uv1", storageUV1, 4},
		{"color", storageColor, 4},
		{"joints", storageJoints, 4},
		{"weights", storageWeights, 4},
	} {
		if attr.offset%4 != 0 {
			t.Errorf("%s starts at %d, which is not 4-aligned", attr.name, attr.offset)
		}
		if attr.offset != end {
			t.Errorf("%s starts at %d, want %d - neither storage vertex has padding in it",
				attr.name, attr.offset, end)
		}
		end = attr.offset + attr.bytes
		// The standard layout is exactly the rows before the joints, so the
		// stride it ends on is where the skinned layout's two extra rows start.
		if at == standardVertexAttrs-1 && end != storageStride {
			t.Errorf("the six shared rows end at %d, want the standard stride %d", end, storageStride)
		}
	}
	if end != storageSkinnedStride {
		t.Errorf("the attributes end at %d, want the skinned stride %d", end, storageSkinnedStride)
	}
}

// The two named layouts, and the closed set they make: six attributes at 32
// bytes, and the same six plus two at 40. The standard layout is a reslice of
// the skinned one rather than a second list, so a row cannot be written down
// twice and drift - and that is what this pins, along with the six the public
// vertex reports.
func TestTheTwoNamedLayoutsAreTheSixAndTheSameSixPlusTwo(t *testing.T) {
	standard, skinned := Vertex{}.VertexLayout(), skinnedVertex{}.VertexLayout()
	if len(standard) != standardVertexAttrs || len(skinned) != 8 {
		t.Fatalf("the layouts have %d and %d attributes, want 6 and 8", len(standard), len(skinned))
	}
	for i := range standard {
		if standard[i] != skinned[i] {
			t.Errorf("attribute %d differs between the layouts: %+v against %+v",
				i, standard[i], skinned[i])
		}
	}
	// The two rows the standard layout declines to supply are the two
	// SceneVertexIn declares only under SCENE_SKIN.
	if skinned[6] != gfx.Attr(storageJoints, gfx.Uint8x4) ||
		skinned[7] != gfx.Attr(storageWeights, gfx.Unorm8x4) {
		t.Errorf("the skinned layout's last two rows are %+v and %+v, want the joints and the weights",
			skinned[6], skinned[7])
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

	// The same vertices again, carried by the loader's own type, because the
	// pack consumes the slice it is given.
	consumed := make([]skinnedVertex, len(vertices))
	for i, vertex := range vertices {
		consumed[i] = skinnedVertex{Vertex: vertex, Joints: [4]uint16{9, 9, 9, 9}, Weights: m.Vec4{X: 1}}
	}
	packed := packOverAuthored(consumed, record, false)
	if len(packed) != len(want) {
		t.Fatalf("packed %d bytes over the vertices, want %d", len(packed), len(want))
	}
	for i := range want {
		if packed[i] != want[i] {
			t.Fatalf("byte %d of vertex %d is %#02x, want %#02x",
				i%storageStride, i/storageStride, packed[i], want[i])
		}
	}
	if packed := packOverAuthored(nil, record, false); packed != nil {
		t.Errorf("no vertices packed %v, want nothing", packed)
	}
}

// The skinned layout is the standard one plus eight bytes, and that is what the
// buffer has to say: the same six rows at the same offsets, at a stride of 40,
// with the joints and the weights after them. A geometry no placement skins is
// packed at 32 and the last eight bytes are simply not there.
func TestTheSkinnedLayoutAppendsTheJointsAndWeightsToTheSameSixRows(t *testing.T) {
	standard := everyAttribute()
	var arena []byte
	at, _, record := packVertices(&arena, standard)
	want := at.of(arena)

	skinned := packOverAuthored(everySkinnedAttribute(), record, true)
	if len(skinned) != len(standard)*storageSkinnedStride {
		t.Fatalf("the skinned pack wrote %d bytes, want %d vertices at %d",
			len(skinned), len(standard), storageSkinnedStride)
	}
	authored := everySkinnedAttribute()
	for i := range authored {
		// The six shared rows are byte-identical to the standard pack's, which
		// is what makes the two layouts one layout with a tail rather than two
		// descriptions of a vertex.
		shared := skinned[i*storageSkinnedStride : i*storageSkinnedStride+storageStride]
		for at := range shared {
			if shared[at] != want[i*storageStride+at] {
				t.Fatalf("skinned vertex %d differs from the standard pack at byte %d: %#02x, want %#02x",
					i, at, shared[at], want[i*storageStride+at])
			}
		}
		if got := readStoredJoints(skinned, i); got != authored[i].Joints {
			t.Errorf("vertex %d stores joints %v, want %v", i, got, authored[i].Joints)
		}
		// A weight comes back within one eight-bit code. Their total does not
		// come back at one, which is exactly why the skin path divides by it
		// rather than trusting it.
		if got := readStoredWeights(skinned, i); !nearWeights(got, authored[i].Weights) {
			t.Errorf("vertex %d stores weights %v, want %v within a code",
				i, got, authored[i].Weights)
		}
	}

	// The unskinned pack of the same converted vertices is the standard layout
	// exactly: the joints and the weights the loader read are not stored at
	// all.
	unskinned := packOverAuthored(everySkinnedAttribute(), record, false)
	if len(unskinned) != len(standard)*storageStride {
		t.Fatalf("the unskinned pack wrote %d bytes, want %d vertices at %d",
			len(unskinned), len(standard), storageStride)
	}
	for i := range want {
		if unskinned[i] != want[i] {
			t.Fatalf("unskinned byte %d is %#02x, want the standard pack's %#02x",
				i, unskinned[i], want[i])
		}
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
