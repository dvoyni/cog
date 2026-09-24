package model

import "github.com/dvoyni/cog/bundles/model/internal"

// Vertex is the authoring vertex: the struct an app fills in for a mesh,
// carrying the six attributes of the standard layout at locations 0..5. Its
// VertexLayout method reports the storage layout it packs into, not its own
// field offsets.
type Vertex = internal.Vertex

// VertexLayout is implemented by the plain-data vertex types a mesh accepts.
// The returned attributes describe the buffer that is uploaded.
type VertexLayout = internal.VertexLayout

// The storage vertex's two strides: the bytes one standard and one skinned
// vertex take in the buffer a mesh uploads. Vertex's VertexLayout method
// reports the standard layout's rows.
const (
	StorageStride        = internal.StorageStride
	StorageSkinnedStride = internal.StorageSkinnedStride
)

// StandardVertexAttrs is how many of the skinned layout's eight rows the
// standard layout keeps.
const StandardVertexAttrs = internal.StandardVertexAttrs

// LightPoint and LightSpot are the two punctual lights a LightDescr describes.
const (
	LightPoint = internal.LightPoint
	LightSpot  = internal.LightSpot
)

// MaxLights is the per-pass light cap: the length of the shader's Lights array.
const MaxLights = internal.MaxLights

// MaxClipPlays is how many clips one draw may blend. A fifth play is dropped
// by lowest weight and reported once per model rather than failing the draw.
const MaxClipPlays = internal.MaxClipPlays

// LightKind is which of the two punctual lights a LightDescr describes.
// PointLight and SpotLight set it themselves; it is exposed so an Op can be
// read back.
type LightKind = internal.LightKind

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
type LightDescr = internal.LightDescr

// MeshRef names one mesh scene can draw. It is an opaque value: a source, a
// dense scene id that doubles as the sort key's meshID, and a generation that
// makes a recycled id detectable. Its zero value is no mesh.
type MeshRef = internal.MeshRef

// ModelLight is one KHR_lights_punctual light a model file declares, in the
// model's own space, with its node's flattened transform already applied.
//
// Lights are exposed as data and nothing converts one automatically. A file's
// lights are authored for the file, not for the scene it is dropped into: a
// lamp prop placed forty times would silently blow the sixteen-light per-pass
// cap, and which of a level's lights matter is the app's judgement, not the
// loader's. So an app reads these and declares the ones it wants through
// PointLight and SpotLight, at whatever world transform it drew the model at.
type ModelLight = internal.ModelLight

// ModelRef names what a scene- or node-scoped query is asking about: a glTF
// path and, optionally, a scene and a node inside it.
//
// It is a struct rather than three bare strings because the bare form has a
// transposition bug that compiles: Bounds(path, "crate", "") and
// Bounds(path, "", "crate") are both valid calls and mean different things.
//
// Only Nodes, Bounds and AABB take one. Everything else on the facade is per
// path, because path is the whole cache key: a model has one joint index space,
// and MorphTargets is one flattened list that Node re-rooting does not renumber.
type ModelRef = internal.ModelRef

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
type ClipPlay = internal.ClipPlay

// ClipInfo is one clip a model file declares, as Clips reports it.
//
// Duration is here because a caller needs it to know when a one-shot play has
// ended, which is a question only the clip's own length answers and the one
// piece of clip state gameplay cannot compute for itself.
type ClipInfo = internal.ClipInfo

// ClipMachine is a clip state machine: the caller keeps it, fires triggers into
// it and steps it each tick, and it hands back the frame's clip plays and what
// happened on the way. It is a plain storable value, so a game keeps one in a
// field, a map or a Component. Its tables are
// m.Lists, so a copy shares them read-only and then steps on its own.
type ClipMachine = internal.ClipMachine

// ClipState is one state of a ClipMachine: a named clip, whether it loops, and
// the Rate that scales the dt Step is given. Rate zero reads as 1.
type ClipState = internal.ClipState

// ClipTransition is one way out of a state, taken by Fire on its On trigger or
// by Step when a non-looping From finishes. Exactly one of the two is set.
type ClipTransition = internal.ClipTransition

// EaseKind names the curve a crossfade's incoming weight follows: anim's
// easings of the same names, as an enum so that a ClipMachine stays storable.
type EaseKind = internal.EaseKind

const (
	EaseLinear     = internal.EaseLinear
	EaseCubicIn    = internal.EaseCubicIn
	EaseCubicOut   = internal.EaseCubicOut
	EaseCubicInOut = internal.EaseCubicInOut
)

// ClipEvent is one thing a ClipMachine reports from a Step: a state, by index,
// entered, exited or finished. StateName names the index.
type ClipEvent = internal.ClipEvent

// ClipEventKind is what happened to a ClipEvent's state. Its zero value is no
// event.
type ClipEventKind = internal.ClipEventKind

const (
	ClipEntered  = internal.ClipEntered
	ClipExited   = internal.ClipExited
	ClipFinished = internal.ClipFinished
)

// ModelHandle is a plain slot index into the Lookup's dense model table. A
// ModelRef resolves to one once, through the load facade's Resolve, and a read
// by handle is then one index, with no path clean and no string hash. There is
// no generation: a handle goes stale only when its model is unloaded, and
// drawing an unloaded model is undefined behaviour. The zero value is no model.
type ModelHandle = internal.ModelHandle

// LookupReadAccess is the read facade: everything a draw reads from a resident
// model, by ModelHandle and by MeshRef. It never loads, and a model that is
// not resident is absent. Acquire a *Lookup read dependency in a handler,
// build one with NewLookupReadAccess, and never store the result. Readers run
// side by side, because everything it reads is immutable from install to
// unload.
type LookupReadAccess = internal.LookupReadAccess

// LookupAccess is the scoped facade for everything about a Lookup that neither
// loads a model nor frees a GPU texture: the mesh verbs, UnloadModel and the
// two memory totals. Acquire a *Lookup write dependency in a handler, build one
// with NewLookupAccess, and pass it to consumers for the duration of that
// handler. Never store the result: the handles behind it are valid only while
// the handler holds its lock.
type LookupAccess = internal.LookupAccess

// LookupDeviceAccess is the scoped facade for everything about a Lookup that
// needs the device: Preload, State and the model queries, all of which load,
// and the two unload verbs that free a GPU texture at the call. Acquire
// *Lookup write, storage.FileSystem read and *gfx.ResourceQueue write in a
// handler, build one with NewLookupDeviceAccess, and never store the result.
type LookupDeviceAccess = internal.LookupDeviceAccess

// MeshSource discriminates where a MeshRef came from. model knows only its own
// durable meshes; a renderer that mints frame-local meshes declares its own
// sources past MeshDurable and builds their refs with NewMeshRef.
type MeshSource = internal.MeshSource

const (
	MeshNone    = internal.MeshNone
	MeshDurable = internal.MeshDurable
)

// UnitMesh names one of the unit meshes, the box, sphere and plane a
// renderer's debug vocabulary draws. Lookup.EnsureUnit bakes each on first use.
type UnitMesh = internal.UnitMesh

const (
	UnitBox    = internal.UnitBox
	UnitSphere = internal.UnitSphere
	UnitPlane  = internal.UnitPlane
)

// The residency a renderer's flush reads and drives. They are what the Lookup
// hands a renderer holding it for writing, and scene names each of them.
type (
	// MeshRecord is one resident mesh: the buffers it draws from and the
	// geometry gfx needs to describe it.
	MeshRecord = internal.MeshRecord
	// MeshBaker is the flush's GPU half, handed to Lookup.DrainMeshes.
	MeshBaker = internal.MeshBaker
	// BakeFunc uploads one buffer's bytes and returns its durable descriptor.
	BakeFunc = internal.BakeFunc
	// BakeTextureFunc uploads one texture's texels and returns its durable
	// descriptor.
	BakeTextureFunc = internal.BakeTextureFunc
	// Span locates one stretch of a mint's arena.
	Span = internal.Span
	// LayoutCache interns vertex layouts by their Go type. A renderer that
	// mints frame-local meshes keeps one of its own.
	LayoutCache = internal.LayoutCache
	// MeshInput is one mint's geometry, validated, described and written into
	// an arena.
	MeshInput = internal.MeshInput
	// ModelView is what one draw's Scene and Node selectors resolve to on a
	// resident model.
	ModelView = internal.ModelView
	// ModelSelectorError is a selector failure that knows the key it reports
	// under.
	ModelSelectorError = internal.ModelSelectorError
	// ResidentAnimation is a model's baked animation once it is resident.
	ResidentAnimation = internal.ResidentAnimation
	// BakedClip is one clip on the sampled grid.
	BakedClip = internal.BakedClip
	// SkinBuffers is the group 2 bindings a draw reads.
	SkinBuffers = internal.SkinBuffers
	// MorphBinding is everything one primitive says about morphing.
	MorphBinding = internal.MorphBinding
	// WeightFrames is the frame pair one play landed on, as rows of the
	// model's morph-weight grid.
	WeightFrames = internal.WeightFrames
	// ReportOnce is the report-once callback the animation resolution takes.
	ReportOnce = internal.ReportOnce
)

// The bundled PBR shader: its variants, its material slots and its alpha
// modes. The records it reads are in records.go.
type (
	// PbrDefaults are the 1x1 textures every absent texture slot binds.
	PbrDefaults = internal.PbrDefaults
	// ShaderVariant is which of the bundled module's four variants a draw
	// needs.
	ShaderVariant = internal.ShaderVariant
	// PbrSlot is one texture slot of the bundled material: the names of its
	// texture, its sampler, and the two record members that place it.
	PbrSlot = internal.PbrSlot
	// AlphaMode is glTF's alphaMode, which selects fixed-function state and,
	// for alphaMask, a shader discard.
	AlphaMode = internal.AlphaMode
	// MaterialIngredients are what a draw's material is resolved from apart
	// from its shader: the params gfx binds by name - the textures, the
	// samplers and the members of the scenePbrMaterial uniform block - and the
	// pipeline state. A file's material keeps its own, and a baked mesh has
	// BundledIngredients.
	MaterialIngredients = internal.MaterialIngredients
	// SceneShaderDescr is the default scene shader: the shader a draw uses
	// when nothing it names sets one, and params of its own overlaid under
	// the draw's. Its zero value is the bundled PBR. See
	// LookupAccess.SetDefaultSceneShader.
	SceneShaderDescr = internal.SceneShaderDescr
)

const (
	VariantStatic = internal.VariantStatic
	VariantCount  = internal.VariantCount
	AlphaOpaque   = internal.AlphaOpaque
	AlphaMask     = internal.AlphaMask
	AlphaBlend    = internal.AlphaBlend
	// NormalSlot is the one PbrSlots slot whose default is the flat normal
	// rather than the white texel.
	NormalSlot = internal.NormalSlot
	// The morph block's word counts: one slot's range, one target's header,
	// and the byte a block is counted in.
	MorphRangeWords        = internal.MorphRangeWords
	MorphTargetHeaderWords = internal.MorphTargetHeaderWords
	MorphWordSize          = internal.MorphWordSize
	// SceneShaderPath is the bundled PBR module's storage path.
	SceneShaderPath = internal.SceneShaderPath
	// VertexDecodePath is the storage path of the source a custom material
	// includes to read the storage vertex. It declares the functions
	// sceneOctDecode, sceneDecodeNormal, sceneDecodeTangent and sceneDecodeUV
	// and the constants SCENE_OCT_TANGENT_MAX, SCENE_TANGENT_Y_SHIFT and
	// SCENE_TANGENT_HANDEDNESS, and no binding and no struct; do not declare
	// those names again.
	VertexDecodePath = internal.VertexDecodePath
	// FramePath is the storage path of the pass's view of the world. It
	// declares the structs SceneFrame, SceneLight and SceneLightSample; the
	// binding sceneFrame, read-only storage at @group(0) @binding(0), which
	// scene binds on every draw; the functions sceneCameraPosition,
	// sceneViewDirection, sceneAmbient, sceneSun, sceneLightCount and
	// sceneLightSample; and the //#const SCENE_MAX_LIGHTS, whose default is
	// MaxLights, so an includer supplies nothing. Do not declare those names
	// again.
	FramePath = internal.FramePath
	// PbrPath is the storage path of the bundled material's BRDF and lighting
	// loop. It declares the structs SceneSurface and ScenePbrSurface, the
	// constants SCENE_PI and SCENE_DIELECTRIC_F0, and the functions sceneD_GGX,
	// sceneV_SmithGGXCorrelated, sceneF_Schlick, sceneEnvBRDFApprox,
	// scenePunctualContribution and sceneShadeSurface, and no binding of its
	// own. It includes FramePath, so everything FramePath declares comes with
	// it, once. Do not declare those names again.
	PbrPath = internal.PbrPath
	// VertexStagePath is the storage path of the bundled vertex stage whole.
	// It declares vs_main, and through the private sources it includes the
	// structs SceneVertexIn, SceneVertexOut and SceneVertex and the group 0
	// and group 2 bindings the stage reads, each named scene, Scene or SCENE_.
	// The renderer supplies SCENE_SKIN and SCENE_MORPH for the draw's
	// geometry, so an includer declares neither. Do not declare those names
	// again.
	VertexStagePath = internal.VertexStagePath
	// FragmentStagePath is the storage path of the bundled fragment stage as
	// a function. It declares scenePbrFragment, which a custom fs_main calls
	// for the shaded colour, and through what it includes SceneVertexOut,
	// PbrPath, FramePath and the material's group 1 bindings, which the
	// renderer fills on every draw. Group 3 is the includer's own, and its
	// per-draw numbers go in the material's uniform block, composed from the
	// three Material paths below. Do not declare those names again.
	FragmentStagePath = internal.FragmentStagePath
	// MaterialProloguePath is the storage path of the opening of the
	// material's uniform block: it declares the struct ScenePbrMaterial. A
	// shader adding per-draw numbers of its own includes it, then a fields
	// source of its own that includes MaterialFieldsPath and lists its members
	// after it, then MaterialEpiloguePath, before FragmentStagePath. Includes
	// are once per path, so the material's own three are skipped and the block
	// holds every member once. Do not declare those names again.
	MaterialProloguePath = internal.MaterialProloguePath
	// MaterialFieldsPath is the storage path of the material's uniform block's
	// members: baseColorFactor, emissiveFactor, baseColorTransform,
	// metallicRoughnessTransform, normalTransform, occlusionTransform,
	// emissiveTransform, baseColorRotation, metallicRoughnessRotation,
	// normalRotation, occlusionRotation, emissiveRotation, metallicFactor,
	// roughnessFactor, normalScale, occlusionStrength, alphaCutoff and uvSets,
	// 160 of the 256 bytes gfx allows a block. Do not declare those names
	// again.
	MaterialFieldsPath = internal.MaterialFieldsPath
	// MaterialEpiloguePath is the storage path of the close of the material's
	// uniform block, which declares its binding, scenePbrMaterial, at
	// @group(1) @binding(0). Do not declare those names again.
	MaterialEpiloguePath = internal.MaterialEpiloguePath
)

// PbrSlots are the bundled material's five texture slots, in record order, and
// PbrSampler the sampler every default slot binds.
var (
	PbrSlots   = internal.PbrSlots
	PbrSampler = internal.PbrSampler
)

// The bundled shader's storage bindings, by the names its WGSL declares them
// under.
const (
	// BindingSceneFrame is one pass's FrameBlock, bound as a range of
	// FrameBlockSize.
	BindingSceneFrame = internal.BindingSceneFrame
	// BindingSceneInstances is one pass's slice of Instance records, bound as
	// a range so that instance_index stays pass-relative.
	BindingSceneInstances = internal.BindingSceneInstances
	// BindingSceneAnim is the frame's whole animation arena, which AppendAnim
	// writes and an instance's AnimOffset indexes in vec4s. It is bound on
	// every draw, so a frame that animates nothing still binds one vec4.
	BindingSceneAnim = internal.BindingSceneAnim
	// BindingSceneMeshes is the frame's whole SceneMesh arena, which an
	// instance's Mesh indexes in records. Slot 0 is IdentityMesh.
	BindingSceneMeshes = internal.BindingSceneMeshes
	// BindingScenePoses and BindingSceneSkinJoints are a model's two durable
	// pose buffers and BindingSceneMorphDeltas its delta buffer, from
	// SkinBuffers: group 2, bound only where the draw's variant declares them.
	BindingScenePoses       = internal.BindingScenePoses
	BindingSceneSkinJoints  = internal.BindingSceneSkinJoints
	BindingSceneMorphDeltas = internal.BindingSceneMorphDeltas
)

// The sizes of the records the shader reads, in bytes, which binding ranges
// and record indices are built from. PoseSize and SkinJointSize are the
// records of the two durable pose buffers; AnimHeaderVec4s and
// PlayRecordVec4s are the header and a play in the vec4s AnimOffset counts.
const (
	InstanceSize         = internal.InstanceSize
	FrameBlockSize       = internal.FrameBlockSize
	LightSize            = internal.LightSize
	SceneMeshSize        = internal.SceneMeshSize
	SceneAnimHeaderSize  = internal.SceneAnimHeaderSize
	ScenePlayRecordSize  = internal.ScenePlayRecordSize
	SceneMorphWeightSize = internal.SceneMorphWeightSize
	PoseSize             = internal.PoseSize
	SkinJointSize        = internal.SkinJointSize
	AnimHeaderVec4s      = internal.AnimHeaderVec4s
	PlayRecordVec4s      = internal.PlayRecordVec4s
)

type (
	// Instance is the 64-byte per-instance record sceneInstances holds: the
	// 4x3 world matrix as three rows, the animation offset, the flags, the
	// plain-bound joint and the per-mesh record index.
	Instance = internal.Instance
	// InstanceAnim is what an instance record says about animation, shared by
	// every instance of one batch.
	InstanceAnim = internal.InstanceAnim
	// FrameBlock is one pass's sceneFrame block. A renderer fills View,
	// Projection, ViewProjection, CameraPosition and ViewDirection from its
	// own camera, and PackFrameLighting fills the rest.
	FrameBlock = internal.FrameBlock
	// FrameLighting is a camera's sun and hemispheric ambient under the names
	// a camera declares them by, which a renderer copies from its camera.
	FrameLighting = internal.FrameLighting
	// Light is the 48-byte packed punctual light the frame block's array
	// holds.
	Light = internal.Light
	// LightSelection is one pass's light array, capped at MaxLights, filled
	// by Offer and packed by PackFrameLighting.
	LightSelection = internal.LightSelection
	// SceneMesh is the 32-byte per-mesh record the stored UVs decode against.
	SceneMesh = internal.SceneMesh
	// ScenePlayRecord is one play as the shader reads it.
	ScenePlayRecord = internal.ScenePlayRecord
	// SceneAnimHeader is the sceneAnim block's two-vec4 header.
	SceneAnimHeader = internal.SceneAnimHeader
	// SceneMorphWeight is one active morph target as the shader reads it.
	SceneMorphWeight = internal.SceneMorphWeight
	// AnimMorph is the morph half of one draw's sceneAnim block.
	AnimMorph = internal.AnimMorph
)

// SceneNoAnim is the AnimOffset of an instance that animates nothing.
const SceneNoAnim = internal.SceneNoAnim

// The instance flags, named after the WGSL constants the shader tests.
// SceneNoSkin and ScenePlainJoint are mutually exclusive by construction.
const (
	SceneNonUniform = internal.SceneNonUniform
	SceneNoSkin     = internal.SceneNoSkin
	ScenePlainJoint = internal.ScenePlainJoint
)

// IdentityMesh is the per-mesh record of a mesh with no UV range of its own,
// and slot 0 of every frame's sceneMeshes arena.
var IdentityMesh = internal.IdentityMesh
