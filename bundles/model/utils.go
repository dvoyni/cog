package model

import (
	"io/fs"

	"github.com/dvoyni/cog/bundles/model/internal/types"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// NewLookup builds an empty Lookup at model's default configuration, for tests
// and tools that drive one without the plugin.
func NewLookup() *Lookup { return types.NewLookup() }

// NewLookupReadAccess builds the read facade over lookup. Call it inside a
// handler that holds the *Lookup read lock, and never store the result.
func NewLookupReadAccess(lookup *Lookup) LookupReadAccess {
	return types.NewLookupReadAccess(lookup)
}

// NewLookupAccess builds the scoped facade over lookup. Call it inside a
// handler that holds the *Lookup write lock, and never store the result.
func NewLookupAccess(k kernel.Kernel, lookup *Lookup) LookupAccess {
	return types.NewLookupAccess(k, lookup)
}

// NewLookupDeviceAccess builds the device facade over lookup. Call it inside a
// handler that holds the *Lookup write lock, the filesystem read lock and the
// resource queue write lock, and never store the result.
func NewLookupDeviceAccess(
	k kernel.Kernel, lookup *Lookup, fsys fs.FS, resources *gfx.ResourceQueue,
) LookupDeviceAccess {
	return types.NewLookupDeviceAccess(k, lookup, fsys, resources)
}

// NewMeshRef builds the ref of a mesh a renderer minted itself, under a source
// it declared past MeshDurable. Nothing in model resolves such a ref.
func NewMeshRef(source MeshSource, id, generation uint32) MeshRef {
	return types.NewMeshRef(source, id, generation)
}

// MintMesh validates one caller's geometry, describes it, and writes its bytes
// into arena. durable says the geometry outlives the frame, which buys a
// bounding sphere and narrowed indices. A renderer mints its frame-local meshes
// through it, into an arena and a LayoutCache of its own.
func MintMesh[TVertex VertexLayout](
	cache *LayoutCache, arena *[]byte, vertices []TVertex, indices []uint32,
	topology gfx.PrimitiveTopology, durable bool,
) (MeshInput, error) {
	return types.MintMesh[TVertex](cache, arena, vertices, indices, topology, durable)
}

// NewClipMachine builds a clip state machine over the clips a model declares,
// as LookupDeviceAccess.Clips reports them. Every clip and state name is
// resolved here, once; Step never touches the lookup. The machine starts in
// states[0] at time 0. An unknown clip or state, a transition with both
// triggers or neither, and an empty state list are each an error.
func NewClipMachine(clips []ClipInfo, states []ClipState, transitions []ClipTransition) (ClipMachine, error) {
	return types.NewClipMachine(clips, states, transitions)
}

// VariantFor picks the shader variant a draw needs from what it deforms.
func VariantFor(skinned, morphed bool) ShaderVariant { return types.VariantFor(skinned, morphed) }

// ResolvePlays resolves one draw's plays against a resident animation into the
// play records the shader reads and the weight frames the morph blend reads,
// appending to dst and weights.
func ResolvePlays(
	anim *ResidentAnimation, path string, plays []ClipPlay,
	dst []ScenePlayRecord, weights []WeightFrames, report ReportOnce,
) ([]ScenePlayRecord, []WeightFrames) {
	return types.ResolvePlays(anim, path, plays, dst, weights, report)
}

// BlendJoint blends one joint's pose across the resolved plays on the CPU, for
// a re-root that has to follow a moving bone.
func BlendJoint(anim *ResidentAnimation, plays []ScenePlayRecord, joint int) (m.Mat4, bool) {
	return types.BlendJoint(anim, plays, joint)
}

// BlendMorphWeights blends one draw's morph weights over the model's flattened
// slot list, appending to dst.
func BlendMorphWeights(
	anim *ResidentAnimation, path string, plays []ScenePlayRecord,
	frames []WeightFrames, override []float32, overridden bool,
	dst []float32, report ReportOnce,
) []float32 {
	return types.BlendMorphWeights(anim, path, plays, frames, override, overridden, dst, report)
}

// SelectMorphTargets culls one primitive's blended weights to the sparse list
// of active targets the shader reads, appending to dst.
func SelectMorphTargets(
	weights []float32, dst []SceneMorphWeight, path string, report ReportOnce,
) []SceneMorphWeight {
	return types.SelectMorphTargets(weights, dst, path, report)
}

// PbrState maps glTF's alphaMode and doubleSided onto pipeline state.
func PbrState(alpha AlphaMode, doubleSided bool) gfx.MaterialState {
	return types.PbrState(alpha, doubleSided)
}

// BundledPbr builds the bundled PBR material's forward gfx material, once per
// shader variant, around the given default textures.
func BundledPbr(defaults PbrDefaults) [VariantCount]gfx.MaterialDescr {
	return types.BundledPbr(defaults)
}

// BundledIngredients are a baked mesh's ingredients around the given default
// textures: white and flat in every slot, opaque, single-sided and painted
// white.
func BundledIngredients(defaults PbrDefaults) MaterialIngredients {
	return types.BundledIngredients(defaults)
}

// VariantShader is shader under variant's SCENE_SKIN and SCENE_MORPH defines,
// added to its own supply. The zero shader reads as the bundled PBR.
func VariantShader(shader gfx.ShaderDescr, variant ShaderVariant) gfx.ShaderDescr {
	return types.VariantShader(shader, variant)
}

// SceneShader describes the bundled PBR module under the given options.
func SceneShader(opts ...gfx.ShaderOption) gfx.ShaderDescr { return types.SceneShader(opts...) }

// PackVertices packs standard vertices into the storage layout at the end of
// arena, and reports where they landed, their bounding sphere and the per-mesh
// UV record they were quantised against.
func PackVertices(arena *[]byte, vertices []Vertex) (Span, m.Sphere, SceneMesh) {
	return types.PackVertices(arena, vertices)
}

// PackInstance builds the instance record for one world matrix under one
// batch's animation, naming the per-mesh record slot its geometry decodes
// against. It flags a non-uniformly scaled matrix, so the shader takes the
// inverse-transpose for that instance's normals.
func PackInstance(world m.Mat4, anim InstanceAnim, mesh uint32) Instance {
	return types.PackInstance(world, anim, mesh)
}

// PackLight resolves one punctual light's defaults into its packed record. A
// spot with no direction, or with its inner cone at or past its outer cone,
// returns ErrSpotDirectionMissing or ErrSpotConeInverted, and the renderer
// reports it and skips the light.
func PackLight(descr LightDescr) (Light, error) { return types.PackLight(descr) }

// ContributionAt is what one packed light is worth at a point: the shader's
// own falloff there times the colour's luminance. Evaluated at the eye, it is
// the score a renderer offers the light to a LightSelection at.
func ContributionAt(light *Light, point m.Vec3) float32 {
	return types.ContributionAt(light, point)
}

// PackFrameLighting writes the sun, the ambient, the selected lights and
// LightCount into one pass's frame block, resolving the defaults: the sun
// direction is normalised, a zero one is no sun, and a zero intensity means 1.
// The view fields are left as they came in.
func PackFrameLighting(block FrameBlock, lighting FrameLighting, selection *LightSelection) FrameBlock {
	return types.PackFrameLighting(block, lighting, selection)
}

// AppendAnim appends one draw's sceneAnim block to dst - the header, the
// plays, the morph list, padded to a whole vec4 - and returns the grown slice
// with the AnimOffset an instance carries. A draw with no plays and no morph
// targets appends nothing and returns SceneNoAnim.
func AppendAnim(dst []byte, plays []ScenePlayRecord, morph AnimMorph) ([]byte, uint32) {
	return types.AppendAnim(dst, plays, morph)
}

// PaintParams appends the params that turn the bundled PBR's white paint into
// paint of one colour, lit or self-lit, for a renderer to lay over it on the
// draw.
func PaintParams(dst []gfx.ParameterDescr, color m.Color, selfLit bool) []gfx.ParameterDescr {
	return types.PaintParams(dst, color, selfLit)
}
