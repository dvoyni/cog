package scene

import "github.com/dvoyni/cog/bundles/scene/internal"

// Material is a scene material: the gfx materials it serves, one per pass tag.
// A pass whose tag has no entry skips every draw using this material, so tag
// participation is purely a material property — a draw gets no say in which
// passes it appears in.
//
// A nil Material is the bundled PBR, so every draw literal that omits the field
// is untouched, and the hand-written one-entry case is
// Material{{Descr: descr}}. In v1 the only tag is forward; when shadows land
// they add a shadow entry to that same value and every draw that passed nil
// gains shadow casting with no call-site change.
type Material = internal.Material

// MaterialTag binds one pass tag to the gfx material that serves it.
//
// A tag entry is a whole gfx.MaterialDescr rather than a shader, because two
// independent things vary per tag. Pipeline state is strictly per material with
// no pass or draw override, so a shadow pass takes its cull mode from its own
// entry; and a declared-but-unused WGSL binding is still reflected and must be
// bound, so the parameter set is tag-specific too — an alphaMode MASK shadow
// shader declares baseColorTexture and alphaCutoff, an opaque one declares
// neither.
type MaterialTag = internal.MaterialTag

// Vertex is the authoring vertex: the struct an app fills in for a scene mesh,
// carrying the six attributes of the standard layout at locations 0..5.
//
// It is not the bytes scene uploads. Scene packs every standard-layout vertex
// into the storage layout at bake (scene/internal/vertexpack.go), so what a shader reads
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
type Vertex = internal.Vertex

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
type VertexLayout = internal.VertexLayout

// MeshRef names one mesh scene can draw. It is an opaque value: a source, a
// dense scene id that doubles as the sort key's meshID, and a generation that
// makes a recycled id detectable. Its zero value is no mesh.
type MeshRef = internal.MeshRef

// MeshDraw is everything one Mesh call says beyond which mesh it draws.
//
// A zero MeshDraw is a valid draw at the origin with the bundled PBR, culled by
// the mesh's own baked sphere.
type MeshDraw = internal.MeshDraw

const VertexDecodePath = internal.VertexDecodePath
