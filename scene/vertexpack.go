package scene

import (
	"encoding/binary"
	"math"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// The storage vertex: where each of the standard layout's attributes sits in
// the buffer scene uploads, and the stride one vertex occupies there.
//
// These are written down rather than taken from Vertex's field offsets, and
// that is the whole point of them. The authoring struct and the storage bytes
// are two layouts that happen to coincide today; the moment one attribute
// changes format they stop coinciding, and an offset derived from the Go
// struct would silently become the wrong one - the authoring offset, used to
// address the storage buffer. Writing them here means the divergence is a diff
// in one place rather than a class of bug.
//
// scene/vertexpack_test.go asserts that they still agree with the struct
// today, and is deleted by the first ticket that moves an attribute.
const (
	storagePosition = 0
	storageNormal   = 12
	storageTangent  = 24
	storageUV0      = 40
	storageUV1      = 48
	storageColor    = 56
	storageJoints   = 60
	storageWeights  = 68
	storageStride   = 84
)

// standardVertexLayout is the storage layout the bundled PBR's vertex stage
// reads, in @location order. It describes the bytes packVertices writes, not
// the Go struct a caller fills in.
var standardVertexLayout = [...]gfx.VertexAttr{
	gfx.Attr(storagePosition, gfx.Float32x3), // POSITION
	gfx.Attr(storageNormal, gfx.Float32x3),   // NORMAL
	gfx.Attr(storageTangent, gfx.Float32x4),  // TANGENT
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
	dst := (*arena)[at:]
	box := m.Box3{Min: vertices[0].Position, Max: vertices[0].Position}
	for i := range vertices {
		vertex := &vertices[i]
		box.Min, box.Max = box.Min.Min(vertex.Position), box.Max.Max(vertex.Position)
		packVertex(dst[i*storageStride:], vertex)
	}
	return span{at: at, size: size}, box.Sphere()
}

// packVertex writes one authoring vertex as its storage bytes. dst is the whole
// of the arena from this vertex's offset on, so every write is at a constant
// offset from its start.
func packVertex(dst []byte, vertex *Vertex) {
	putVec3(dst[storagePosition:], vertex.Position)
	putVec3(dst[storageNormal:], vertex.Normal)
	putVec4(dst[storageTangent:], vertex.Tangent)
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
