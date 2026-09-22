package model

import "github.com/dvoyni/cog/bundles/model/internal/types"

// Vertex is the authoring vertex: the struct an app fills in for a mesh,
// carrying the six attributes of the standard layout at locations 0..5. Its
// VertexLayout method reports the storage layout it packs into, not its own
// field offsets.
type Vertex = types.Vertex

// VertexLayout is implemented by the plain-data vertex types a mesh accepts.
// The returned attributes describe the buffer that is uploaded.
type VertexLayout = types.VertexLayout

// The storage vertex's two strides: the bytes one standard and one skinned
// vertex take in the buffer a mesh uploads. Vertex's VertexLayout method
// reports the standard layout's rows.
const (
	StorageStride        = types.StorageStride
	StorageSkinnedStride = types.StorageSkinnedStride
)

// StandardVertexAttrs is how many of the skinned layout's eight rows the
// standard layout keeps.
const StandardVertexAttrs = types.StandardVertexAttrs

// LightPoint and LightSpot are the two punctual lights a LightDescr describes.
const (
	LightPoint = types.LightPoint
	LightSpot  = types.LightSpot
)

// MaxLights is the per-pass light cap: the length of the shader's Lights array.
const MaxLights = types.MaxLights

// MaxClipPlays is how many clips one draw may blend. A fifth play is dropped
// by lowest weight and reported once per model rather than failing the draw.
const MaxClipPlays = types.MaxClipPlays

// LightKind is which of the two punctual lights a LightDescr describes.
// PointLight and SpotLight set it themselves; it is exposed so an Op can be
// read back.
type LightKind = types.LightKind

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
type LightDescr = types.LightDescr

// MeshRef names one mesh scene can draw. It is an opaque value: a source, a
// dense scene id that doubles as the sort key's meshID, and a generation that
// makes a recycled id detectable. Its zero value is no mesh.
type MeshRef = types.MeshRef

// ModelLight is one KHR_lights_punctual light a model file declares, in the
// model's own space, with its node's flattened transform already applied.
//
// Lights are exposed as data and nothing converts one automatically. A file's
// lights are authored for the file, not for the scene it is dropped into: a
// lamp prop placed forty times would silently blow the sixteen-light per-pass
// cap, and which of a level's lights matter is the app's judgement, not the
// loader's. So an app reads these and declares the ones it wants through
// PointLight and SpotLight, at whatever world transform it drew the model at.
type ModelLight = types.ModelLight

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
type ModelRef = types.ModelRef

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
type ClipPlay = types.ClipPlay

// ClipInfo is one clip a model file declares, as Clips reports it.
//
// Duration is here because a caller needs it to know when a one-shot play has
// ended, which is a question only the clip's own length answers and the one
// piece of clip state gameplay cannot compute for itself.
type ClipInfo = types.ClipInfo

// ModelHandle is a plain slot index into the Lookup's dense model table. A
// ModelRef resolves to one once, through the load facade's Resolve, and a read
// by handle is then one index, with no path clean and no string hash. There is
// no generation: a handle goes stale only when its model is unloaded, and
// drawing an unloaded model is undefined behaviour. The zero value is no model.
type ModelHandle = types.ModelHandle

// LookupReadAccess is the read facade: everything a draw reads from a resident
// model, by ModelHandle and by MeshRef. It never loads, and a model that is
// not resident is absent. Acquire a *Lookup read dependency in a handler,
// build one with NewLookupReadAccess, and never store the result. Readers run
// side by side, because everything it reads is immutable from install to
// unload.
type LookupReadAccess = types.LookupReadAccess

// LookupAccess is the scoped facade for everything about a Lookup that neither
// loads a model nor frees a GPU texture: the mesh verbs, UnloadModel and the
// two memory totals. Acquire a *Lookup write dependency in a handler, build one
// with NewLookupAccess, and pass it to consumers for the duration of that
// handler. Never store the result: the handles behind it are valid only while
// the handler holds its lock.
type LookupAccess = types.LookupAccess

// LookupDeviceAccess is the scoped facade for everything about a Lookup that
// needs the device: Preload, State and the model queries, all of which load,
// and the two unload verbs that free a GPU texture at the call. Acquire
// *Lookup write, storage.FileSystem read and *gfx.ResourceQueue write in a
// handler, build one with NewLookupDeviceAccess, and never store the result.
type LookupDeviceAccess = types.LookupDeviceAccess

// MeshSource discriminates where a MeshRef came from. model knows only its own
// durable meshes; a renderer that mints frame-local meshes declares its own
// sources past MeshDurable and builds their refs with NewMeshRef.
type MeshSource = types.MeshSource

const (
	MeshNone    = types.MeshNone
	MeshDurable = types.MeshDurable
)

// UnitMesh names one of the unit meshes, the box, sphere and plane a
// renderer's debug vocabulary draws. Lookup.EnsureUnit bakes each on first use.
type UnitMesh = types.UnitMesh

const (
	UnitBox    = types.UnitBox
	UnitSphere = types.UnitSphere
	UnitPlane  = types.UnitPlane
)

// The residency a renderer's flush reads and drives. They are what the Lookup
// hands a renderer holding it for writing, and scene names each of them.
type (
	// MeshRecord is one resident mesh: the buffers it draws from and the
	// geometry gfx needs to describe it.
	MeshRecord = types.MeshRecord
	// MeshBaker is the flush's GPU half, handed to Lookup.DrainMeshes.
	MeshBaker = types.MeshBaker
	// BakeFunc uploads one buffer's bytes and returns its durable descriptor.
	BakeFunc = types.BakeFunc
	// BakeTextureFunc uploads one texture's texels and returns its durable
	// descriptor.
	BakeTextureFunc = types.BakeTextureFunc
	// Span locates one stretch of a mint's arena.
	Span = types.Span
	// LayoutCache interns vertex layouts by their Go type. A renderer that
	// mints frame-local meshes keeps one of its own.
	LayoutCache = types.LayoutCache
	// MeshInput is one mint's geometry, validated, described and written into
	// an arena.
	MeshInput = types.MeshInput
	// ModelView is what one draw's Scene and Node selectors resolve to on a
	// resident model.
	ModelView = types.ModelView
	// ModelSelectorError is a selector failure that knows the key it reports
	// under.
	ModelSelectorError = types.ModelSelectorError
	// ResidentAnimation is a model's baked animation once it is resident.
	ResidentAnimation = types.ResidentAnimation
	// BakedClip is one clip on the sampled grid.
	BakedClip = types.BakedClip
	// SkinBuffers is the group 2 bindings a draw reads.
	SkinBuffers = types.SkinBuffers
	// MorphBinding is everything one primitive says about morphing.
	MorphBinding = types.MorphBinding
	// WeightFrames is the frame pair one play landed on, as rows of the
	// model's morph-weight grid.
	WeightFrames = types.WeightFrames
	// ReportOnce is the report-once callback the animation resolution takes.
	ReportOnce = types.ReportOnce
)

// The bundled PBR shader: its variants, its material slots and its alpha
// modes. The records it reads are in records.go.
type (
	// PbrDefaults are the 1x1 textures every absent texture slot binds.
	PbrDefaults = types.PbrDefaults
	// ShaderVariant is which of the bundled module's four variants a draw
	// needs.
	ShaderVariant = types.ShaderVariant
	// PbrSlot is one texture slot of the bundled material: the names of its
	// texture, its sampler, and the two record members that place it.
	PbrSlot = types.PbrSlot
	// AlphaMode is glTF's alphaMode, which selects fixed-function state and,
	// for alphaMask, a shader discard.
	AlphaMode = types.AlphaMode
)

const (
	VariantStatic = types.VariantStatic
	VariantCount  = types.VariantCount

	AlphaOpaque = types.AlphaOpaque
	AlphaMask   = types.AlphaMask
	AlphaBlend  = types.AlphaBlend

	// NormalSlot is the one PbrSlots slot whose default is the flat normal
	// rather than the white texel.
	NormalSlot = types.NormalSlot

	// The morph block's word counts: one slot's range, one target's header,
	// and the byte a block is counted in.
	MorphRangeWords        = types.MorphRangeWords
	MorphTargetHeaderWords = types.MorphTargetHeaderWords
	MorphWordSize          = types.MorphWordSize

	// SceneShaderPath is the bundled PBR module's storage path, and
	// VertexDecodePath the storage path of the module a custom material imports
	// to read the storage vertex.
	SceneShaderPath  = types.SceneShaderPath
	VertexDecodePath = types.VertexDecodePath
)

// PbrSlots are the bundled material's five texture slots, in record order, and
// PbrSampler the sampler every default slot binds.
var (
	PbrSlots   = types.PbrSlots
	PbrSampler = types.PbrSampler
)

// The shader's records, below, and their packers in utils.go.
//
// Every byte the bundled shader reads is written by model's packers, so two
// renderers drawing through it write the same bytes, and a shader change that
// compiles against one cannot draw garbage in the other. A renderer owns
// everything around the bytes - its arenas, culling, sorting and emission -
// and appends what the packers return into its own arenas.
//
// Fixed-size records come back by value: PackInstance, PackLight and
// PackFrameLighting. The variable-length animation block is appended by
// AppendAnim, because its layout is the order of its parts and its padding.

// The bundled shader's storage bindings, by the names its WGSL declares them
// under.
const (
	// BindingSceneFrame is one pass's FrameBlock, bound as a range of
	// FrameBlockSize.
	BindingSceneFrame = types.BindingSceneFrame
	// BindingSceneInstances is one pass's slice of Instance records, bound as
	// a range so that instance_index stays pass-relative.
	BindingSceneInstances = types.BindingSceneInstances
	// BindingSceneAnim is the frame's whole animation arena, which AppendAnim
	// writes and an instance's AnimOffset indexes in vec4s. It is bound on
	// every draw, so a frame that animates nothing still binds one vec4.
	BindingSceneAnim = types.BindingSceneAnim
	// BindingSceneMeshes is the frame's whole SceneMesh arena, which an
	// instance's Mesh indexes in records. Slot 0 is IdentityMesh.
	BindingSceneMeshes = types.BindingSceneMeshes
	// BindingScenePbrMaterial is one batch's ScenePbrRecord, bound as a range
	// of ScenePbrRecordSize.
	BindingScenePbrMaterial = types.BindingScenePbrMaterial
	// BindingScenePoses and BindingSceneSkinJoints are a model's two durable
	// pose buffers and BindingSceneMorphDeltas its delta buffer, from
	// SkinBuffers: group 2, bound only where the draw's variant declares them.
	BindingScenePoses       = types.BindingScenePoses
	BindingSceneSkinJoints  = types.BindingSceneSkinJoints
	BindingSceneMorphDeltas = types.BindingSceneMorphDeltas
)

// The sizes of the records the shader reads, in bytes, which binding ranges
// and record indices are built from. PoseSize and SkinJointSize are the
// records of the two durable pose buffers; AnimHeaderVec4s and
// PlayRecordVec4s are the header and a play in the vec4s AnimOffset counts.
const (
	InstanceSize         = types.InstanceSize
	FrameBlockSize       = types.FrameBlockSize
	LightSize            = types.LightSize
	ScenePbrRecordSize   = types.ScenePbrRecordSize
	SceneMeshSize        = types.SceneMeshSize
	SceneAnimHeaderSize  = types.SceneAnimHeaderSize
	ScenePlayRecordSize  = types.ScenePlayRecordSize
	SceneMorphWeightSize = types.SceneMorphWeightSize
	PoseSize             = types.PoseSize
	SkinJointSize        = types.SkinJointSize
	AnimHeaderVec4s      = types.AnimHeaderVec4s
	PlayRecordVec4s      = types.PlayRecordVec4s
)

type (
	// Instance is the 64-byte per-instance record sceneInstances holds: the
	// 4x3 world matrix as three rows, the animation offset, the flags, the
	// plain-bound joint and the per-mesh record index.
	Instance = types.Instance
	// InstanceAnim is what an instance record says about animation, shared by
	// every instance of one batch.
	InstanceAnim = types.InstanceAnim
	// FrameBlock is one pass's sceneFrame block. A renderer fills View,
	// Projection, ViewProjection, CameraPosition and ViewDirection from its
	// own camera, and PackFrameLighting fills the rest.
	FrameBlock = types.FrameBlock
	// FrameLighting is a camera's sun and hemispheric ambient under the names
	// a camera declares them by, which a renderer copies from its camera.
	FrameLighting = types.FrameLighting
	// Light is the 48-byte packed punctual light the frame block's array
	// holds.
	Light = types.Light
	// LightSelection is one pass's light array, capped at MaxLights, filled
	// by Offer and packed by PackFrameLighting.
	LightSelection = types.LightSelection
	// ScenePbrRecord is the bundled PBR's per-batch record.
	ScenePbrRecord = types.ScenePbrRecord
	// SceneMesh is the 32-byte per-mesh record the stored UVs decode against.
	SceneMesh = types.SceneMesh
	// ScenePlayRecord is one play as the shader reads it.
	ScenePlayRecord = types.ScenePlayRecord
	// SceneAnimHeader is the sceneAnim block's two-vec4 header.
	SceneAnimHeader = types.SceneAnimHeader
	// SceneMorphWeight is one active morph target as the shader reads it.
	SceneMorphWeight = types.SceneMorphWeight
	// AnimMorph is the morph half of one draw's sceneAnim block.
	AnimMorph = types.AnimMorph
)

// SceneNoAnim is the AnimOffset of an instance that animates nothing.
const SceneNoAnim = types.SceneNoAnim

// The instance flags, named after the WGSL constants the shader tests.
// SceneNoSkin and ScenePlainJoint are mutually exclusive by construction.
const (
	SceneNonUniform = types.SceneNonUniform
	SceneNoSkin     = types.SceneNoSkin
	ScenePlainJoint = types.ScenePlainJoint
)

// IdentityMesh is the per-mesh record of a mesh with no UV range of its own,
// and slot 0 of every frame's sceneMeshes arena.
var IdentityMesh = types.IdentityMesh
