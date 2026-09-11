package scene

import (
	"encoding/binary"
	"math"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// The storage vertex: where each attribute sits in the buffer scene uploads,
// and the two strides one vertex occupies there - 32 bytes for the standard
// layout's six attributes, 40 for the skinned layout's eight.
//
// These are written down rather than taken from a Go struct's field offsets,
// and that is the whole point of them. The authoring struct is 72 bytes of
// float and the storage vertex is 32; the normal, the tangent and both UV sets
// have all moved, so an offset derived from the Go struct would silently be the
// wrong one - the authoring offset, used to address the storage buffer.
//
// Every offset is a multiple of four and so is either stride, which WebGPU
// requires of arrayStride unconditionally. A narrower attribute that broke that
// would not save a byte: it pads straight back.
const (
	storagePosition = 0
	storageNormal   = 12
	storageTangent  = 16
	storageUV0      = 20
	storageUV1      = 24
	storageColor    = 28
	storageStride   = 32

	storageJoints        = 32
	storageWeights       = 36
	storageSkinnedStride = 40
)

// unorm8CodeMax is the largest code an 8-bit unorm holds, and the divisor the
// fetch unit has already applied by the time a shader sees one. Both stored
// unorm bytes - the colour and a skin weight - scale by exactly it.
const unorm8CodeMax = 0xFF

// skinnedVertexLayout is the storage layout of a mesh some placement skins, in
// @location order, and standardVertexLayout is its first six rows - which is
// the whole relationship between the two named layouts, spelled as a reslice so
// that no row can be written down twice and drift.
//
// They describe the bytes the packers below write, not the Go struct a caller
// fills in. The cut between them is the one SceneVertexIn has had since the
// module was split: locations 6 and 7 are declared only under SCENE_SKIN, and
// the Go side honours it here rather than supplying joints and weights to a
// variant that never reads them.
//
// Four of the six shared rows are narrowed, and what reads them is
// builtin/scene/vertexdecode.wgsl: a two-component 16-bit unorm holding oct32,
// one 32-bit word holding oct 15/15 plus handedness plus a reserved bit, and
// two more 16-bit unorm pairs holding UVs against the range in the mesh's own
// record. gfx requires a shader's declared (kind, count) at a location to equal
// what the layout supplies, so the pair here and the pair in vertex.wgsl cannot
// drift apart without a pipeline being refused.
//
// The joints and the weights need no decode source at all: a Uint8x4 arrives as
// the same vec4<u32> a Uint16x4 did and a Unorm8x4 as the same vec4<f32> a
// Float32x4 did, so the fetch unit does the whole of it. What the narrowing
// does need is the divide in deform.wgsl, because eight bits cannot hold four
// weights that sum to exactly one - see scene/vertexskin.go.
//
// The UVs and the weights are the narrowings the interface check cannot catch,
// because a Float32x2 and a Unorm16x2 both arrive as a vec2<f32> and a
// Float32x4 and a Unorm8x4 both as a vec4<f32>: the fetch unit's divide is the
// only difference the shader sees, and the declaration is the same either way.
// What holds the UV rows together is the mesh record, and what holds the weight
// row together is that divide.
var (
	skinnedVertexLayout = [...]gfx.VertexAttr{
		gfx.Attr(storagePosition, gfx.Float32x3), // POSITION
		gfx.Attr(storageNormal, gfx.Unorm16x2),   // NORMAL     - oct32
		gfx.Attr(storageTangent, gfx.Uint32),     // TANGENT    - oct 15/15 + handedness
		gfx.Attr(storageUV0, gfx.Unorm16x2),      // TEXCOORD_0 - against the mesh record
		gfx.Attr(storageUV1, gfx.Unorm16x2),      // TEXCOORD_1 - against the mesh record
		gfx.Attr(storageColor, gfx.Unorm8x4),     // COLOR_0
		gfx.Attr(storageJoints, gfx.Uint8x4),     // JOINTS_0   - one byte a joint, capped at 256
		gfx.Attr(storageWeights, gfx.Unorm8x4),   // WEIGHTS_0  - renormalised in the shader
	}
	standardVertexLayout = skinnedVertexLayout[:standardVertexAttrs]
)

// standardVertexAttrs is how many of the eight rows the standard layout keeps.
// It is the seam itself: the six every variant reads, against the two only
// SCENE_SKIN declares.
const standardVertexAttrs = 6

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
func (skinnedVertex) VertexLayout() []gfx.VertexAttr { return skinnedVertexLayout[:] }

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
// accumulated it while it was reading the accessors: readVertexAttributes
// already visits every UV element, so the ranges cost that path no traversal at
// all.
func packOverAuthored(vertices []skinnedVertex, mesh sceneMesh, skinned bool) []byte {
	if len(vertices) == 0 {
		return nil
	}
	stride := storageStride
	if skinned {
		stride = storageSkinnedStride
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
var _ [unsafe.Sizeof(skinnedVertex{}) - storageSkinnedStride]byte

// packInto writes every vertex's standard storage bytes at its stride in dst,
// quantising the UVs against the mesh's record.
func packInto(dst []byte, vertices []Vertex, mesh sceneMesh) {
	mesh = mesh.packRecord()
	for i := range vertices {
		packVertex(dst[i*storageStride:], vertices[i], mesh)
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
func packVertex(dst []byte, vertex Vertex, mesh sceneMesh) {
	putVec3(dst[storagePosition:], vertex.Position)
	normalX, normalY := packNormal(vertex.Normal)
	binary.NativeEndian.PutUint16(dst[storageNormal:], normalX)
	binary.NativeEndian.PutUint16(dst[storageNormal+2:], normalY)
	binary.NativeEndian.PutUint32(dst[storageTangent:], packTangent(vertex.Tangent))
	putUV(dst[storageUV0:], vertex.UV0, mesh.UV0Scale, mesh.UV0Bias)
	putUV(dst[storageUV1:], vertex.UV1, mesh.UV1Scale, mesh.UV1Bias)
	colour := vertex.Color
	dst[storageColor+0] = packUnorm8(colour.R)
	dst[storageColor+1] = packUnorm8(colour.G)
	dst[storageColor+2] = packUnorm8(colour.B)
	dst[storageColor+3] = packUnorm8(colour.A)
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
		dst[storageJoints+i] = packJoint(joint)
	}
	for i, weight := range [4]float32{weights.X, weights.Y, weights.Z, weights.W} {
		dst[storageWeights+i] = packWeight(weight)
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
func appendArena(arena *[]byte, data []byte) span {
	if len(data) == 0 {
		return span{}
	}
	at := len(*arena)
	*arena = append(*arena, data...)
	return span{at: at, size: len(data)}
}
