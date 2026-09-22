package scene

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene/internal/types"
)

// CameraID orders a camera among every other pass in the frame, and is the
// default gfx.Order for the passes it emits. It is a defined type over
// gfx.Order rather than an alias because it carries meaning the order does not:
// it is also a camera's identity, and registering one twice is an error.
//
// gfx reserves no ranges, so a camera interleaves with canvas by taking an
// order between two layer values. Scene cameras conventionally take negative
// ids when canvas draws entirely over them, since an app's canvas layers
// usually start at zero. Two recorders emitting passes at an equal Order is an app-level bug
// that promises nothing: gfx breaks the tie by declaration sequence, and canvas
// and scene flush from separate subscriptions with no order defined between
// them, so which one lands first is not a property the app can rely on.
type CameraID = types.CameraID

// ProjectionKind selects how a camera flattens the world.
//
// Perspective and Orthographic both project along the camera's forward axis, so
// revealing a vertical face always costs ground-plane scale: tilt a camera to
// elevation phi and the ground foreshortens by exactly sin phi. Oblique
// separates the two, which is why it is a kind rather than something an app
// composes from outside - see Shear.
type ProjectionKind = types.ProjectionKind

const (
	Perspective  = types.Perspective
	Orthographic = types.Orthographic
	// Oblique is Orthographic with view-space depth sheared into screen-up by
	// Shear, and at Shear 0 it is exactly Orthographic. It reads Height the
	// same way; what it adds is that the plane the camera sits in renders at
	// true scale while depth is displaced instead.
	Oblique = types.Oblique
)

// PassTag names what a pass is for, and selects which of a material's gfx
// materials serves it. It stays a string so the readable API costs no
// registration handshake; scene interns it once per pass, not once per draw.
type PassTag = types.PassTag

// TagForward is the pass every lit and unlit surface draws into, and what an
// empty PassTag means.
const TagForward = types.TagForward

// Pass is one render pass a camera emits. Its zero value is the forward pass
// into the screen with automatic depth, which is what the default pass is.
//
// Target is the gfx handle passed through untouched: the screen sentinel, a
// durable texture from the resource queue, or a frame-local target from
// gfx.OpQueue.TemporaryTarget. Scene keeps no name registry over it and offers
// no allocator of its own, because minting a texture takes the gfx queue, which
// a scene recorder does not hold; an app that renders a camera into a temporary
// target locks both queues and hands the target across. A pass with NoTarget()
// takes its size from an explicit depth texture, and one with neither, or with
// NoTarget() and a ClearColor, is reported.
//
// Depth sharing with canvas: canvas declares DepthAuto on every pass it emits,
// so a screen-sized camera pass and canvas draw against the same pooled depth
// texture. That is harmless today, because no built-in canvas material tests
// depth, but a caller-supplied depth-testing canvas material will interact with
// the 3D depth left there. An app wanting isolation gets it by not ordering the
// camera adjacent to canvas, or by giving the pass a depth texture of its own.
//
// Store ops are inferred, not exposed. Depth is kept iff Depth names a texture;
// colour is always kept, and LoadDiscard on colour is not offered, since it
// only pays for a pass that provably covers its whole target.
type Pass = types.Pass

// CameraDescr is everything a camera is. Clears live on its passes, not here:
// carrying them on both, with the camera's ignored once Passes is non-empty, is
// a silent-override rule that produces bug reports.
type CameraDescr = types.CameraDescr

// LayerMask selects which cameras see a recorded item. A camera draws an item
// iff layers & CullMask is non-zero.
//
// Zero reads as LayersAll on both sides, so the degenerate frame — one camera,
// one box, no mask written anywhere — works without the caller learning about
// layers at all.
type LayerMask = types.LayerMask

// LayersAll is every layer, which is also what a mask nobody wrote means.
const LayersAll = types.LayersAll

// LightKind is which of the two punctual lights a LightDescr describes.
// PointLight and SpotLight set it themselves; it is exposed so an Op can be
// read back.
type LightKind = model.LightKind

const (
	LightPoint = model.LightPoint
	LightSpot  = model.LightSpot
)

// LightDescr is one punctual light, point or spot, over the one struct: call
// sites stay explicit through PointLight and SpotLight, a hand-written point
// light leaves the cone fields zero, and the per-camera light buffer is
// homogeneous without scene converting between two structs.
//
// Every zero is a default. Intensity zero means 1. Range zero means infinite,
// glTF's own default - a forgotten Range yields a light that reaches too far,
// which you see immediately, rather than a silently skipped light. OuterCone
// zero means pi/4, glTF's default; InnerCone zero is a real value, falloff
// from the axis.
type LightDescr = model.LightDescr

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
type Material = types.Material

// MaterialTag binds one pass tag to the gfx material that serves it.
//
// A tag entry is a whole gfx.MaterialDescr rather than a shader, because two
// independent things vary per tag. Pipeline state is strictly per material with
// no pass or draw override, so a shadow pass takes its cull mode from its own
// entry; and a declared-but-unused WGSL binding is still reflected and must be
// bound, so the parameter set is tag-specific too — an alphaMode MASK shadow
// shader declares baseColorTexture and alphaCutoff, an opaque one declares
// neither.
type MaterialTag = types.MaterialTag

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
type Vertex = model.Vertex

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
type VertexLayout = model.VertexLayout

// MeshRef names one mesh scene can draw. It is an opaque value: a source, a
// dense scene id that doubles as the sort key's meshID, and a generation that
// makes a recycled id detectable. Its zero value is no mesh.
type MeshRef = model.MeshRef

// MeshDraw is everything one Mesh call says beyond which mesh it draws.
//
// A zero MeshDraw is a valid draw at the origin with the bundled PBR, culled by
// the mesh's own baked sphere.
type MeshDraw = types.MeshDraw

const VertexDecodePath = model.VertexDecodePath

// ModelDraw is everything one Model call says beyond which file it draws.
//
// A zero ModelDraw is a valid draw of the file's default scene at the origin,
// culled by the bounds the file declares.
type ModelDraw = types.ModelDraw

// ModelLight is one KHR_lights_punctual light a model file declares, in the
// model's own space, with its node's flattened transform already applied.
//
// Lights are exposed as data and nothing converts one automatically. A file's
// lights are authored for the file, not for the scene it is dropped into: a
// lamp prop placed forty times would silently blow the sixteen-light per-pass
// cap, and which of a level's lights matter is the app's judgement, not the
// loader's. So an app reads these and declares the ones it wants through
// PointLight and SpotLight, at whatever world transform it drew the model at.
type ModelLight = model.ModelLight

// ModelRef names what a scene- or node-scoped query is asking about, mirroring
// ModelDraw's own selectors field for field.
//
// It is a struct rather than three bare strings because the bare form has a
// transposition bug that compiles: Bounds(path, "crate", "") and
// Bounds(path, "", "crate") are both valid calls and mean different things.
//
// Only Nodes, Bounds and AABB take one. Everything else on the facade is per
// path, because path is the whole cache key: a model has one joint index space,
// and MorphTargets is one flattened list that Node re-rooting does not renumber.
type ModelRef = model.ModelRef

// ClipPlay is one animation clip playing on one model draw.
//
// Animation is stateless: nothing in scene advances Time, and no play survives
// the frame that recorded it. Gameplay - or the anim plugin - owns the clock
// and hands the result to the draw, which is what makes scrubbing, reversing
// and pausing the caller's business rather than an API scene has to grow.
//
// Clips are addressed by name, first match. An unknown name is reported once
// per model and the play dropped, so a typo costs the one play rather than the
// whole character.
type ClipPlay = model.ClipPlay

// ClipInfo is one clip a model file declares, as Clips reports it.
//
// Duration is here because a caller needs it to know when a one-shot play has
// ended, which is a question only the clip's own length answers and the one
// piece of clip state gameplay cannot compute for itself.
type ClipInfo = model.ClipInfo

// OpKind identifies which recording call produced an Op.
type OpKind = types.OpKind

const (
	OpCamera     = types.OpCamera
	OpBox        = types.OpBox
	OpSphere     = types.OpSphere
	OpPlane      = types.OpPlane
	OpLine3D     = types.OpLine3D
	OpWireBox    = types.OpWireBox
	OpMesh       = types.OpMesh
	OpPointLight = types.OpPointLight
	OpSpotLight  = types.OpSpotLight
	OpModel      = types.OpModel
)

// Op is a read-only view of one recorded operation, canvas's shape exactly: it
// reports the call as the recorder made it, not the draws scene derived from
// it, so a WireBox is one Op.
type Op = types.Op

// PassView is the flush result for one pass: what scene decided, in numbers a
// test can assert with no GPU anywhere.
//
// Recorded is how many draws the camera's cull mask selected, Culled how many
// of those its frustum rejected, and Instances how many the pass packed after
// its tag filtered the survivors. Instances counts instances, so it is not
// len(Batches): an instanced call's survivors are one batch of N. Batches are
// in emission order: every opaque and alpha-masked draw sorted by material then
// mesh, then every blended draw back to front. Recording order is not preserved
// within a pass.
//
// The numbers are per pass, not per frame: a draw two cameras both see is
// counted, sorted and packed once in each of their passes, because the sort is
// what a pass is. That is the cost of sorting and it is not worked around.
//
// Frustum is published because asserting that a specific sphere was rejected by
// a specific frustum is the whole point of a culling test; without it such a
// test can only count.
//
// Lights is how many punctual lights the pass packed after its own frustum
// culled them and the cap of 16 took the brightest at the eye. The lights
// dropped past the cap are not counted anywhere: the drop is silent by design.
type PassView = types.PassView

// BatchView is one run of instances drawn from one mesh with one material: one
// gfx draw call, one material record, and InstanceCount contiguous instances of
// the pass's own instance slice starting at FirstInstance.
//
// InstanceCount is 1 for everything but an explicit instanced draw. What the
// flush batches is the surviving instances of one instanced call - not
// consecutive equal draws recorded separately, which is a deferred
// optimisation, and not a blended instanced draw, which stays one batch per
// instance so its entries keep their own depths.
type BatchView = types.BatchView

// LookupAccess is the scoped facade for everything about a Lookup that neither
// loads a model nor frees a GPU texture: the mesh verbs, UnloadModel and the
// two memory totals. Acquire a *Lookup write dependency in a handler, build one
// with NewLookupAccess, and pass it to consumers for the duration of that
// handler. Never store the result: the handles behind it are valid only while
// the handler holds its lock.
type LookupAccess = model.LookupAccess

// LookupDeviceAccess is the scoped facade for everything about a Lookup that
// needs the device: Preload, State and the model queries, all of which load,
// and the two unload verbs that free a GPU texture at the call. Acquire
// *Lookup write, storage.FileSystem read and *gfx.ResourceQueue write in a
// handler, build one with NewLookupDeviceAccess, and never store the result.
type LookupDeviceAccess = model.LookupDeviceAccess
