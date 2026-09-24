package internal

import (
	"encoding/binary"
	"math"
	"unsafe"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// unorm8CodeMax is the largest code an 8-bit unorm holds, and the divisor the
// fetch unit has already applied by the time a shader sees one. Both stored
// unorm bytes - the colour and a skin weight - scale by exactly it.
const unorm8CodeMax = 0xFF

// skinnedVertex is the glTF loader's conversion vertex: the authoring vertex
// plus the two attributes only a skinned draw reads.
//
// It is unexported and it stays that way. No public path ever wrote joints or
// weights - a skin binding is set only from a loaded model's animation - so the
// skinned layout is the loader's alone, and an app cannot author a mesh into
// it. That is what makes "a 32-byte layout paired with a skinning variant"
// unrepresentable rather than merely unchecked: only the loader can produce
// either half, and it derives both from one load-time fact.
//
// The joints arrive in the file's own numbering and in uint16, because that is
// the widest glTF component JOINTS_0 permits; bindGeometryJoints remaps them
// into the model's single numbering and the pack then narrows each to the byte
// the storage vertex holds.
type skinnedVertex struct {
	Vertex
	Joints  [4]uint16
	Weights m.Vec4
}

// VertexLayout reports the skinned storage layout. It exists so that the mesh
// table resolves a model's geometry through the same layout cache every other
// mesh takes - one dense id per Go type - rather than through a second path
// that would have to intern layouts of its own.
func (skinnedVertex) VertexLayout() []gfx.VertexAttr { return SkinnedVertexLayout() }

// PackVertices writes the storage bytes of standard-layout vertices into the
// arena and reports the span they landed in, together with the bounding sphere
// of their positions and the per-mesh record their UVs were quantised against.
//
// It is two traversals, and it stays two. A UV cannot be quantised against a
// range the walk has not finished deriving, so the bounds walk runs first and
// the pack reads its answer. Those are the same two walks the mint made before
// it packed at all - a copy into the arena and a pass for the sphere - with the
// UV ranges riding the bounds walk for free.
//
// Two was measured rather than settled for. BenchmarkBoundVertices and
// BenchmarkPackVertices put the bounds walk at ~314us of a ~1.72ms bake over
// 65,536 authored vertices - 18% - and that is the whole of what a fused bake
// could be competing for. It cannot win it: the quantisation must read every
// authored UV after the range is known, so no shape here is one traversal over
// the authored vertices.
//
// Both fused shapes were built and measured, and neither is faster. Packing
// everything but the UVs while accumulating, then rewriting the eight UV bytes
// per vertex in a second, narrower pass, came out 4.5% slower at 4,096
// vertices, 1.7% slower at 262,144 and inside the noise at 65,536; writing the
// whole vertex and then fixing it up was 7-13% slower at every size. The fixup
// re-streams the same source cache lines the wide read already pulled and
// touches the destination twice, which is what eats the walk it saves. For a
// change that measures at best like nothing, it would split packVertex - the
// one function that writes a standard vertex's storage bytes, and which the
// glTF path shares - into two halves only this path uses.
//
// See bundles/model/docs/specs/mesh.md, "The per-mesh record", and
// github.com/dvoyni/cog/issues/261.
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
func PackVertices(arena *[]byte, vertices []Vertex) (Span, m.Sphere, SceneMesh) {
	if len(vertices) == 0 {
		return Span{}, m.Sphere{}, SceneMesh{}
	}
	sphere, mesh := boundVertices(vertices)
	at, size := len(*arena), len(vertices)*StorageStride
	*arena = append(*arena, make([]byte, size)...)
	packInto((*arena)[at:], vertices, mesh)
	return Span{at: at, size: size}, sphere, mesh
}

// boundVertices walks standard-layout vertices once and reports both things a
// bake needs to know about the set as a whole: the bounding sphere of the
// positions and the record its UVs quantise against.
//
// The two ride together because a mesh is walked for the sphere anyway and the
// UV compares are four more per vertex on data already in cache. They are
// reported as one call because they are one traversal, and separating them
// would invite a caller to pay for it twice.
func boundVertices(vertices []Vertex) (m.Sphere, SceneMesh) {
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

// packOverAuthored packs a loaded model's vertices over the memory they are
// already in and reports the bytes, which are the front of that same
// allocation. skinned picks the layout: the skinned one at 40 bytes when some
// placement draws this geometry under SCENE_SKIN, the standard one at 32 when
// none does, in which case the joints and the weights are simply not written.
//
// It exists for the glTF loader, which holds a whole model's converted vertices
// at once and hands them to the resource queue without copying them. A storage
// vertex is smaller than a converted one under either layout, so the pack runs
// forward over the slice and every write lands strictly behind the vertex it
// has already read - which means the loader pays no second buffer for a model's
// geometry, where packing into a fresh one would have held the converted and
// the packed form of every primitive at the same time.
//
// The slice is consumed: after this returns, its elements are storage bytes
// wearing a skinnedVertex's type, and nothing may read them as vertices again.
//
// The record is handed in rather than derived here, because the glTF path
// accumulated it while it was copying the decoded attributes: fillVertices
// already visits every UV element, so the ranges cost that path no traversal at
// all.
func packOverAuthored(vertices []skinnedVertex, mesh SceneMesh, skinned bool) []byte {
	if len(vertices) == 0 {
		return nil
	}
	stride := StorageStride
	if skinned {
		stride = StorageSkinnedStride
	}
	// Sound only because a storage vertex is the smaller of the two, which the
	// guard below makes a compile error rather than a trust.
	dst := unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), len(vertices)*stride)
	mesh = mesh.packRecord()
	for i := range vertices {
		// The vertex is copied out before the first store, which is what lets
		// dst overlap it: the write for one vertex reads nothing afterwards and
		// ends before the next one begins.
		vertex := vertices[i]
		packVertex(dst[i*stride:], vertex.Vertex, mesh)
		if skinned {
			packSkin(dst[i*stride:], vertex.Joints, vertex.Weights)
		}
	}
	return dst
}

// A storage vertex must fit inside a converted one for packOverAuthored's walk
// to stay behind itself. This is that requirement, spelled so that breaking it
// fails the build with a negative array length rather than corrupting a model's
// geometry at load. The skinned stride is the larger of the two, so it is the
// one that has to fit.
var _ [unsafe.Sizeof(skinnedVertex{}) - StorageSkinnedStride]byte

// packInto writes every vertex's standard storage bytes at its stride in dst,
// quantising the UVs against the mesh's record.
func packInto(dst []byte, vertices []Vertex, mesh SceneMesh) {
	mesh = mesh.packRecord()
	for i := range vertices {
		packVertex(dst[i*StorageStride:], vertices[i], mesh)
	}
}

// packVertex writes one authoring vertex as the standard layout's thirty-two
// storage bytes. dst is the whole of the arena from this vertex's offset on, so
// every write is at a constant offset from its start.
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
// The colour is quantised rather than copied, because the authored form is
// m.Color's linear floats and the stored one is a Unorm8x4. glTF's COLOR_0 is
// linear too, so a file's byte colour round-trips through the float form
// exactly.
//
// mesh is the record this mesh's UVs are quantised against, already resolved to
// the identity where the mesh has no range of its own, so there is no branch
// here for the mesh that names slot 0.
func packVertex(dst []byte, vertex Vertex, mesh SceneMesh) {
	putVec3(dst[StoragePosition:], vertex.Position)
	normalX, normalY := packNormal(vertex.Normal)
	binary.NativeEndian.PutUint16(dst[StorageNormal:], normalX)
	binary.NativeEndian.PutUint16(dst[StorageNormal+2:], normalY)
	binary.NativeEndian.PutUint32(dst[StorageTangent:], packTangent(vertex.Tangent))
	putUV(dst[StorageUV0:], vertex.UV0, mesh.UV0Scale, mesh.UV0Bias)
	putUV(dst[StorageUV1:], vertex.UV1, mesh.UV1Scale, mesh.UV1Bias)
	colour := vertex.Color
	dst[StorageColor+0] = packUnorm8(colour.R)
	dst[StorageColor+1] = packUnorm8(colour.G)
	dst[StorageColor+2] = packUnorm8(colour.B)
	dst[StorageColor+3] = packUnorm8(colour.A)
}

// packSkin writes the skinned layout's last eight bytes over a vertex whose
// first thirty-two packVertex has already written: four joint indices at a byte
// each and four weights at a unorm byte each.
//
// Neither needs a decode source, because the fetch unit hands the shader the
// same types the wide forms did. What the weight's eight bits do need is
// deform.wgsl's divide, because no rounding of four weights into four bytes
// makes them sum to exactly one - and the divide covers a malformed file into
// the bargain.
//
// It is called only where the geometry's layout is the skinned one, so nothing
// writes past the standard layout's stride.
func packSkin(dst []byte, joints [4]uint16, weights m.Vec4) {
	for i, joint := range joints {
		dst[StorageJoints+i] = packJoint(joint)
	}
	for i, weight := range [4]float32{weights.X, weights.Y, weights.Z, weights.W} {
		dst[StorageWeights+i] = packWeight(weight)
	}
}

// packUnorm8 quantises one value in [0, 1] to its 8-bit unorm code, clamping
// the ends and rounding to nearest. It is the one rounding both the stored
// colour and the stored weights take - see packWeight for why the weights are
// not made to sum to anything.
func packUnorm8(value float32) byte {
	scaled := float64(value) * unorm8CodeMax
	if !(scaled > 0) {
		return 0
	}
	if scaled >= unorm8CodeMax {
		return unorm8CodeMax
	}
	return byte(scaled + 0.5)
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

// appendArena copies data into the arena and reports the span it landed in.
// Both minting surfaces stage through it - the Lookup's durable arena and the
// recording's frame-local one - because the only difference between them is
// which arena.
func appendArena(arena *[]byte, data []byte) Span {
	if len(data) == 0 {
		return Span{}
	}
	at := len(*arena)
	*arena = append(*arena, data...)
	return Span{at: at, size: len(data)}
}
