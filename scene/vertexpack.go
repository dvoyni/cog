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
// and the storage vertex is 64; the normal and the tangent have moved, so an
// offset derived from the Go struct would silently be the wrong one - the
// authoring offset, used to address the storage buffer.
//
// Every offset is a multiple of four and so is the stride, which WebGPU
// requires of arrayStride unconditionally. A narrower attribute that broke that
// would not save a byte: it pads straight back.
const (
	storagePosition = 0
	storageNormal   = 12
	storageTangent  = 16
	storageUV0      = 20
	storageUV1      = 28
	storageColor    = 36
	storageJoints   = 40
	storageWeights  = 48
	storageStride   = 64
)

// standardVertexLayout is the storage layout the bundled PBR's vertex stage
// reads, in @location order. It describes the bytes packVertices writes, not
// the Go struct a caller fills in.
//
// The normal and the tangent are the two narrowed rows, and what reads them is
// builtin/scene/vertexdecode.wgsl: a two-component 16-bit unorm holding oct32,
// and one 32-bit word holding oct 15/15 plus handedness plus a reserved bit.
// gfx requires a shader's declared (kind, count) at a location to equal what
// the layout supplies, so the pair here and the pair in vertex.wgsl cannot
// drift apart without a pipeline being refused.
var standardVertexLayout = [...]gfx.VertexAttr{
	gfx.Attr(storagePosition, gfx.Float32x3), // POSITION
	gfx.Attr(storageNormal, gfx.Unorm16x2),   // NORMAL   - oct32
	gfx.Attr(storageTangent, gfx.Uint32),     // TANGENT  - oct 15/15 + handedness
	gfx.Attr(storageUV0, gfx.Float32x2),      // TEXCOORD_0
	gfx.Attr(storageUV1, gfx.Float32x2),      // TEXCOORD_1
	gfx.Attr(storageColor, gfx.Unorm8x4),     // COLOR_0
	gfx.Attr(storageJoints, gfx.Uint16x4),    // JOINTS_0
	gfx.Attr(storageWeights, gfx.Float32x4),  // WEIGHTS_0
}

// packVertices writes the storage bytes of standard-layout vertices into the
// arena and reports the span they landed in, together with the bounding sphere
// of their positions.
//
// It is one traversal doing both jobs, because both were already being paid
// for: the mint copied every byte into the arena to stage it and walked the
// positions again for the sphere. The copy becomes a transform-copy and the
// bounds ride along with it, so packing costs a mint nothing it was not
// already spending.
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
func packVertices(arena *[]byte, vertices []Vertex) (span, m.Sphere) {
	if len(vertices) == 0 {
		return span{}, m.Sphere{}
	}
	at, size := len(*arena), len(vertices)*storageStride
	*arena = append(*arena, make([]byte, size)...)
	return span{at: at, size: size}, packInto((*arena)[at:], vertices)
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
func packOverAuthored(vertices []Vertex) []byte {
	if len(vertices) == 0 {
		return nil
	}
	// Sound only because a storage vertex is the smaller of the two, which the
	// guard below makes a compile error rather than a trust.
	dst := unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), len(vertices)*storageStride)
	packInto(dst, vertices)
	return dst
}

// A storage vertex must fit inside an authoring one for packOverAuthored's walk
// to stay behind itself. This is that requirement, spelled so that breaking it
// fails the build with a negative array length rather than corrupting a model's
// geometry at load.
var _ [unsafe.Sizeof(Vertex{}) - storageStride]byte

// packInto writes every vertex's storage bytes at its stride in dst and reports
// the bounding sphere of their positions. dst may be the vertices' own memory;
// each vertex is copied into the call before anything is written, and the write
// for one vertex ends before the next one begins.
func packInto(dst []byte, vertices []Vertex) m.Sphere {
	box := m.Box3{Min: vertices[0].Position, Max: vertices[0].Position}
	for i := range vertices {
		box.Min = box.Min.Min(vertices[i].Position)
		box.Max = box.Max.Max(vertices[i].Position)
		packVertex(dst[i*storageStride:], vertices[i])
	}
	return box.Sphere()
}

// packVertex writes one authoring vertex as its storage bytes. dst is the whole
// of the arena from this vertex's offset on, so every write is at a constant
// offset from its start.
//
// The vertex arrives by value rather than by pointer, which is what lets the
// glTF loader pack a slice over itself: the copy is made before the first store
// into dst, so a dst that overlaps the vertex reads the right bytes.
//
// The normal and the tangent are encoded here rather than stored: four bytes
// each, decoded in the vertex stage from builtin/scene/vertexdecode.wgsl. The
// normal's two 16-bit unorm codes are written as two words rather than one,
// because a Unorm16x2 attribute's components are consecutive in the buffer and
// a single native-endian uint32 would order them by the host's endianness.
func packVertex(dst []byte, vertex Vertex) {
	putVec3(dst[storagePosition:], vertex.Position)
	normalX, normalY := packNormal(vertex.Normal)
	binary.NativeEndian.PutUint16(dst[storageNormal:], normalX)
	binary.NativeEndian.PutUint16(dst[storageNormal+2:], normalY)
	binary.NativeEndian.PutUint32(dst[storageTangent:], packTangent(vertex.Tangent))
	putVec2(dst[storageUV0:], vertex.UV0)
	putVec2(dst[storageUV1:], vertex.UV1)
	copy(dst[storageColor:storageColor+4], vertex.Color[:])
	for i, joint := range vertex.Joints {
		binary.NativeEndian.PutUint16(dst[storageJoints+i*2:], joint)
	}
	putVec4(dst[storageWeights:], vertex.Weights)
}

func putFloat32(dst []byte, value float32) {
	binary.NativeEndian.PutUint32(dst, math.Float32bits(value))
}

func putVec2(dst []byte, value m.Vec2) {
	putFloat32(dst[0:], value.X)
	putFloat32(dst[4:], value.Y)
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
