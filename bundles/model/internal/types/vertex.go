package types

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// Vertex is the authoring vertex: the struct an app fills in for a scene mesh,
// carrying the six attributes of the standard layout at locations 0..5.
//
// It is not the bytes scene uploads. Scene packs every standard-layout vertex
// into the storage layout at bake (scene/internal/types/vertexpack.go), so what a shader reads
// is that layout's offsets and formats rather than this struct's: 72 bytes of
// float are authored here and 32 are stored, because four of the six rows
// narrow - the normal, the tangent and both UV sets to four bytes each - and
// the colour quantises to a unorm byte a channel. An app writes directions in
// the m.Vec3 and m.Vec4 it would write anyway and never sees the encoding.
// Nothing in scene ever hands a Vertex back, so there is exactly one
// authoritative form, the authored one, and it flows one way.
//
// There is no joint and no weight here, and that is contract rather than an
// omission. No public path ever wrote them: a skin binding is set only from a
// loaded model's animation, so a buffer-built mesh never skins and the eight
// bytes would be dead in every mesh an app can build. The skinned layout - the
// same six attributes plus JOINTS_0 and WEIGHTS_0, 40 bytes at eight locations
// - belongs to the glTF loader and is unreachable from here.
//
// Normal and Tangent.XYZ are directions. Their length is divided out by the
// octahedral encode and is unrecoverable after bake - silently, as contract,
// because a check would fire on correct code: a normal computed from a cross
// product is one float of rounding from length 1.0001 and draws correctly.
// Tangent.W is handedness, and only its sign is stored.
//
// UV0 and UV1 store as positions inside the range the whole mesh spans, which
// scene derives at bake and re-derives on every update. A coordinate comes back
// within half a code of that range rather than exactly, and the range is what
// makes that half-code small: over the vendored corpus the worst is under half
// a texel of a 4096 texture, where a half float at the same coordinate is 32.
//
// Color is included on failure mode rather than on evidence: it is glTF core,
// costs four bytes as Unorm8x4, and leaving it out renders a vertex-coloured
// model silently white instead of erroring. It is an m.Color rather than four
// raw bytes because this is the one attribute whose authored and stored forms
// would otherwise have coincided, and a caller should not have to know which
// fields scene packs and which it copies; m.Color also says which space a
// component is in, where a byte cannot - glTF's COLOR_0 is linear, which is
// m.NewColorLinear. Its zero value is transparent black, so anything scene
// builds itself writes m.White.
type Vertex struct {
	Position m.Vec3
	Normal   m.Vec3
	Tangent  m.Vec4
	UV0      m.Vec2
	UV1      m.Vec2
	Color    m.Color
}

// VertexLayout reports the standard vertex's storage layout, in @location
// order. It is the one exported implementation whose attributes are not this
// struct's field offsets - see the interface's own documentation below.
func (Vertex) VertexLayout() []gfx.VertexAttr { return standardVertexLayout }

// VertexLayout is implemented by the plain-data vertex types scene accepts.
// The returned attributes describe the *buffer* scene uploads: they map byte
// offsets within one stored vertex to shader locations, in order, and must
// match the vertex inputs of the material the mesh is drawn with.
//
// For a custom layout the buffer is the caller's slice reinterpreted, so the
// offsets are also the Go struct's field offsets and the two readings coincide.
// scene.Vertex is the one exception a caller can see: scene packs it, so its
// method reports the storage layout and its Go fields are the authoring ones.
// The two differ - the stored normal, tangent and two UV sets are four bytes
// each against the struct's twelve, sixteen, eight and eight, and the stored
// colour is four against sixteen - so nothing may read a scene.Vertex layout as
// a description of the Go struct.
//
// There are exactly two layouts scene blesses: the standard one scene.Vertex
// reports, and the skinned one - the same six attributes plus JOINTS_0 and
// WEIGHTS_0 - which the glTF loader alone produces and which no exported type
// reports. Everything else is a custom layout and needs a custom Material.
//
// The one direction that fails is a shader input no attribute supplies. A
// layout supplying an attribute the shader never declares is legal and common,
// and gfx checks the pairing at pipeline time either way.
type VertexLayout interface {
	VertexLayout() []gfx.VertexAttr
}

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
	StoragePosition = 0
	StorageNormal   = 12
	StorageTangent  = 16
	StorageUV0      = 20
	StorageUV1      = 24
	StorageColor    = 28
	StorageStride   = 32

	StorageJoints        = 32
	StorageWeights       = 36
	StorageSkinnedStride = 40
)

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
// weights that sum to exactly one - see bundles/scene/internal/types/vertexskin.go.
//
// The UVs and the weights are the narrowings the interface check cannot catch,
// because a Float32x2 and a Unorm16x2 both arrive as a vec2<f32> and a
// Float32x4 and a Unorm8x4 both as a vec4<f32>: the fetch unit's divide is the
// only difference the shader sees, and the declaration is the same either way.
// What holds the UV rows together is the mesh record, and what holds the weight
// row together is that divide.
var (
	skinnedVertexLayout = [...]gfx.VertexAttr{
		gfx.Attr(StoragePosition, gfx.Float32x3), // POSITION
		gfx.Attr(StorageNormal, gfx.Unorm16x2),   // NORMAL     - oct32
		gfx.Attr(StorageTangent, gfx.Uint32),     // TANGENT    - oct 15/15 + handedness
		gfx.Attr(StorageUV0, gfx.Unorm16x2),      // TEXCOORD_0 - against the mesh record
		gfx.Attr(StorageUV1, gfx.Unorm16x2),      // TEXCOORD_1 - against the mesh record
		gfx.Attr(StorageColor, gfx.Unorm8x4),     // COLOR_0
		gfx.Attr(StorageJoints, gfx.Uint8x4),     // JOINTS_0   - one byte a joint, capped at 256
		gfx.Attr(StorageWeights, gfx.Unorm8x4),   // WEIGHTS_0  - renormalised in the shader
	}
	standardVertexLayout = skinnedVertexLayout[:StandardVertexAttrs]
)

// StandardVertexAttrs is how many of the eight rows the standard layout keeps.
// It is the seam itself: the six every variant reads, against the two only
// SCENE_SKIN declares.
const StandardVertexAttrs = 6

// SkinnedVertexLayout reports the skinned storage layout, all eight rows. It is
// a function rather than an exported variable so that no caller can write a
// row of the one table both layouts are sliced from.
func SkinnedVertexLayout() []gfx.VertexAttr { return skinnedVertexLayout[:] }
