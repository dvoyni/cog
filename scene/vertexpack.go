package scene

import (
	"encoding/binary"
	"math"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// The storage vertex: where each of the standard layout's attributes sits in
// the buffer scene uploads, and the stride one vertex occupies there.
//
// These are written down rather than taken from Vertex's field offsets, and
// that is the whole point of them. The authoring struct is 84 bytes of float
// and the storage vertex is 56; the normal, the tangent and both UV sets have
// moved, so an offset derived from the Go struct would silently be the wrong
// one - the authoring offset, used to address the storage buffer.
//
// Every offset is a multiple of four and so is the stride, which WebGPU
// requires of arrayStride unconditionally. A narrower attribute that broke that
// would not save a byte: it pads straight back.
const (
	storagePosition = 0
	storageNormal   = 12
	storageTangent  = 16
	storageUV0      = 20
	storageUV1      = 24
	storageColor    = 28
	storageJoints   = 32
	storageWeights  = 40
	storageStride   = 56
)

// standardVertexLayout is the storage layout the bundled PBR's vertex stage
// reads, in @location order. It describes the bytes packVertices writes, not
// the Go struct a caller fills in.
//
// The normal, the tangent and both UV sets are the narrowed rows, and what
// reads them is builtin/scene/vertexdecode.wgsl: a two-component 16-bit unorm
// holding oct32, one 32-bit word holding oct 15/15 plus handedness plus a
// reserved bit, and two more 16-bit unorm pairs holding UVs against the range
// in the mesh's own record. gfx requires a shader's declared (kind, count) at a
// location to equal what the layout supplies, so the pair here and the pair in
// vertex.wgsl cannot drift apart without a pipeline being refused.
//
// The UVs are the one narrowing the interface check cannot catch, because both
// Float32x2 and Unorm16x2 arrive as a vec2<f32>: the fetch unit's divide is the
// only difference the shader sees, and its declaration is the same either way.
// What holds those two rows together is the record - a UV read without one is
// a coordinate in [0, 1] where the mesh's range said otherwise.
var standardVertexLayout = [...]gfx.VertexAttr{
	gfx.Attr(storagePosition, gfx.Float32x3), // POSITION
	gfx.Attr(storageNormal, gfx.Unorm16x2),   // NORMAL     - oct32
	gfx.Attr(storageTangent, gfx.Uint32),     // TANGENT    - oct 15/15 + handedness
	gfx.Attr(storageUV0, gfx.Unorm16x2),      // TEXCOORD_0 - against the mesh record
	gfx.Attr(storageUV1, gfx.Unorm16x2),      // TEXCOORD_1 - against the mesh record
	gfx.Attr(storageColor, gfx.Unorm8x4),     // COLOR_0
	gfx.Attr(storageJoints, gfx.Uint16x4),    // JOINTS_0
	gfx.Attr(storageWeights, gfx.Float32x4),  // WEIGHTS_0
}

// packVertices writes the storage bytes of standard-layout vertices into the
// arena and reports the span they landed in, together with the bounding sphere
// of their positions and the per-mesh record their UVs were quantised against.
//
// It is two traversals, and the second one is why: a UV cannot be quantised
// against a range the walk has not finished deriving, so the bounds walk runs
// first and the pack reads its answer. Those are the same two walks the mint
// made before it packed at all - a copy into the arena and a pass for the
// sphere - with the UV ranges riding the bounds walk for free.
//
// It writes native-endian words for the same reason indexBytes does: a
// reinterpret of native memory is what these bytes replace, and a buffer that
// changed byte order with the mechanism would be a difference no caller could
// see coming.
//
// The sphere is reported whether or not its caller wants one - six compares a
// vertex inside a loop that is already touching the position - so that whether
// a mesh is culled by a sphere of its own stays a decision about its lifetime
// rather than about what this pass computed.
func packVertices(arena *[]byte, vertices []Vertex) (span, m.Sphere, sceneMesh) {
	if len(vertices) == 0 {
		return span{}, m.Sphere{}, sceneMesh{}
	}
	sphere, mesh := boundVertices(vertices)
	at, size := len(*arena), len(vertices)*storageStride
	*arena = append(*arena, make([]byte, size)...)
	packInto((*arena)[at:], vertices, mesh)
	return span{at: at, size: size}, sphere, mesh
}

// boundVertices walks standard-layout vertices once and reports both things a
// bake needs to know about the set as a whole: the bounding sphere of the
// positions and the record its UVs quantise against.
//
// The two ride together because a mesh is walked for the sphere anyway and the
// UV compares are four more per vertex on data already in cache. They are
// reported as one call because they are one traversal, and separating them
// would invite a caller to pay for it twice.
func boundVertices(vertices []Vertex) (m.Sphere, sceneMesh) {
	box := m.Box3{Min: vertices[0].Position, Max: vertices[0].Position}
	var uv0, uv1 uvRange
	for i := range vertices {
		box.Min = box.Min.Min(vertices[i].Position)
		box.Max = box.Max.Max(vertices[i].Position)
		uv0.add(vertices[i].UV0)
		uv1.add(vertices[i].UV1)
	}
	return box.Sphere(), meshRecordFor(uv0, uv1)
}

// packOverAuthored packs vertices over the memory they are already in and
// reports the bytes, which are the front of that same allocation.
//
// It exists for the glTF loader, which holds a whole model's converted vertices
// at once and hands them to the resource queue without copying them. A storage
// vertex is smaller than an authoring one, so the pack runs forward over the
// slice and every write lands strictly behind the vertex it has already read -
// which means the loader pays no second buffer for a model's geometry, where
// packing into a fresh one would have held the authored and the packed form of
// every primitive at the same time.
//
// The slice is consumed: after this returns, its elements are storage bytes
// wearing a Vertex's type, and nothing may read them as vertices again.
//
// The record is handed in rather than derived here, because the glTF path
// accumulated it while it was reading the accessors: readVertexAttributes
// already visits every UV element, so the ranges cost that path no traversal at
// all.
func packOverAuthored(vertices []Vertex, mesh sceneMesh) []byte {
	if len(vertices) == 0 {
		return nil
	}
	// Sound only because a storage vertex is the smaller of the two, which the
	// guard below makes a compile error rather than a trust.
	dst := unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), len(vertices)*storageStride)
	packInto(dst, vertices, mesh)
	return dst
}

// A storage vertex must fit inside an authoring one for packOverAuthored's walk
// to stay behind itself. This is that requirement, spelled so that breaking it
// fails the build with a negative array length rather than corrupting a model's
// geometry at load.
var _ [unsafe.Sizeof(Vertex{}) - storageStride]byte

// packInto writes every vertex's storage bytes at its stride in dst, quantising
// the UVs against the mesh's record. dst may be the vertices' own memory; each
// vertex is copied into the call before anything is written, and the write for
// one vertex ends before the next one begins.
func packInto(dst []byte, vertices []Vertex, mesh sceneMesh) {
	mesh = mesh.packRecord()
	for i := range vertices {
		packVertex(dst[i*storageStride:], vertices[i], mesh)
	}
}

// packVertex writes one authoring vertex as its storage bytes. dst is the whole
// of the arena from this vertex's offset on, so every write is at a constant
// offset from its start.
//
// The vertex arrives by value rather than by pointer, which is what lets the
// glTF loader pack a slice over itself: the copy is made before the first store
// into dst, so a dst that overlaps the vertex reads the right bytes.
//
// The normal, the tangent and both UV sets are encoded here rather than stored:
// four bytes each, decoded in the vertex stage from
// builtin/scene/vertexdecode.wgsl. Every 16-bit unorm pair is written as two
// words rather than one, because a Unorm16x2 attribute's components are
// consecutive in the buffer and a single native-endian uint32 would order them
// by the host's endianness.
//
// mesh is the record this mesh's UVs are quantised against, already resolved to
// the identity where the mesh has no range of its own, so there is no branch
// here for the mesh that names slot 0.
func packVertex(dst []byte, vertex Vertex, mesh sceneMesh) {
	putVec3(dst[storagePosition:], vertex.Position)
	normalX, normalY := packNormal(vertex.Normal)
	binary.NativeEndian.PutUint16(dst[storageNormal:], normalX)
	binary.NativeEndian.PutUint16(dst[storageNormal+2:], normalY)
	binary.NativeEndian.PutUint32(dst[storageTangent:], packTangent(vertex.Tangent))
	putUV(dst[storageUV0:], vertex.UV0, mesh.UV0Scale, mesh.UV0Bias)
	putUV(dst[storageUV1:], vertex.UV1, mesh.UV1Scale, mesh.UV1Bias)
	copy(dst[storageColor:storageColor+4], vertex.Color[:])
	for i, joint := range vertex.Joints {
		binary.NativeEndian.PutUint16(dst[storageJoints+i*2:], joint)
	}
	putVec4(dst[storageWeights:], vertex.Weights)
}

// putUV writes one UV set's two unorm codes against the mesh's range.
func putUV(dst []byte, uv, scale, bias m.Vec2) {
	binary.NativeEndian.PutUint16(dst[0:], quantizeUV(uv.X, scale.X, bias.X))
	binary.NativeEndian.PutUint16(dst[2:], quantizeUV(uv.Y, scale.Y, bias.Y))
}

func putFloat32(dst []byte, value float32) {
	binary.NativeEndian.PutUint32(dst, math.Float32bits(value))
}

func putVec3(dst []byte, value m.Vec3) {
	putFloat32(dst[0:], value.X)
	putFloat32(dst[4:], value.Y)
	putFloat32(dst[8:], value.Z)
}

func putVec4(dst []byte, value m.Vec4) {
	putFloat32(dst[0:], value.X)
	putFloat32(dst[4:], value.Y)
	putFloat32(dst[8:], value.Z)
	putFloat32(dst[12:], value.W)
}

// appendArena copies data into the arena and reports the span it landed in.
// Both minting surfaces stage through it - the Lookup's durable arena and the
// recording's frame-local one - because the only difference between them is
// which arena.
func appendArena(arena *[]byte, data []byte) span {
	if len(data) == 0 {
		return span{}
	}
	at := len(*arena)
	*arena = append(*arena, data...)
	return span{at: at, size: len(data)}
}
