package scene

import "github.com/dvoyni/cog/bundles/scene/internal"

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
type CameraID = internal.CameraID

// ProjectionKind selects how a camera flattens the world.
//
// Perspective and Orthographic both project along the camera's forward axis, so
// revealing a vertical face always costs ground-plane scale: tilt a camera to
// elevation phi and the ground foreshortens by exactly sin phi. Oblique
// separates the two, which is why it is a kind rather than something an app
// composes from outside - see Shear.
type ProjectionKind = internal.ProjectionKind

const (
	Perspective  = internal.Perspective
	Orthographic = internal.Orthographic
	// Oblique is Orthographic with view-space depth sheared into screen-up by
	// Shear, and at Shear 0 it is exactly Orthographic. It reads Height the
	// same way; what it adds is that the plane the camera sits in renders at
	// true scale while depth is displaced instead.
	Oblique = internal.Oblique
)

// PassTag names what a pass is for, and selects which of a material's gfx
// materials serves it. It stays a string so the readable API costs no
// registration handshake; scene interns it once per pass, not once per draw.
type PassTag = internal.PassTag

// TagForward is the pass every lit and unlit surface draws into, and what an
// empty PassTag means.
const TagForward = internal.TagForward

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
type Pass = internal.Pass

// CameraDescr is everything a camera is. Clears live on its passes, not here:
// carrying them on both, with the camera's ignored once Passes is non-empty, is
// a silent-override rule that produces bug reports.
type CameraDescr = internal.CameraDescr

// LayerMask selects which cameras see a recorded item. A camera draws an item
// iff layers & CullMask is non-zero.
//
// Zero reads as LayersAll on both sides, so the degenerate frame — one camera,
// one box, no mask written anywhere — works without the caller learning about
// layers at all.
type LayerMask = internal.LayerMask

// LayersAll is every layer, which is also what a mask nobody wrote means.
const LayersAll = internal.LayersAll

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

// MeshDraw is everything one Mesh call says beyond which mesh it draws.
//
// A zero MeshDraw is a valid draw at the origin with the bundled PBR, culled by
// the mesh's own baked sphere.
type MeshDraw = internal.MeshDraw

// ModelDraw is everything one Model call says beyond which file it draws.
//
// A zero ModelDraw is a valid draw of the file's default scene at the origin,
// culled by the bounds the file declares.
type ModelDraw = internal.ModelDraw

// OpKind identifies which recording call produced an Op.
type OpKind = internal.OpKind

const (
	OpCamera     = internal.OpCamera
	OpBox        = internal.OpBox
	OpSphere     = internal.OpSphere
	OpPlane      = internal.OpPlane
	OpLine3D     = internal.OpLine3D
	OpWireBox    = internal.OpWireBox
	OpMesh       = internal.OpMesh
	OpPointLight = internal.OpPointLight
	OpSpotLight  = internal.OpSpotLight
	OpModel      = internal.OpModel
)

// Op is a read-only view of one recorded operation, canvas's shape exactly: it
// reports the call as the recorder made it, not the draws scene derived from
// it, so a WireBox is one Op.
type Op = internal.Op

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
type PassView = internal.PassView

// BatchView is one run of instances drawn from one mesh with one material: one
// gfx draw call and InstanceCount contiguous instances of the pass's own
// instance slice starting at FirstInstance.
//
// What the flush batches is a run of equal opaque draws: the surviving
// instances of one instanced call, and any draws recorded separately that
// share its mesh, material, per-draw parameters and animation. A blended draw
// is never batched, instanced or not; it stays one batch per instance so its
// entries keep their own depths.
type BatchView = internal.BatchView
