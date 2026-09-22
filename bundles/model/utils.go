package model

import (
	"io/fs"

	"github.com/dvoyni/cog/bundles/model/internal/types"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/qmuntal/gltf"
)

// SkinnedVertexLayout reports the skinned storage layout: the standard layout's
// six rows plus JOINTS_0 and WEIGHTS_0.
func SkinnedVertexLayout() []gfx.VertexAttr { return types.SkinnedVertexLayout() }

// UnitBoxGeometry builds the 1x1x1 cube centred on the origin, four vertices a
// face so every face keeps its own flat normal, wound counter-clockwise seen
// from outside.
func UnitBoxGeometry() ([]Vertex, []uint32) { return types.UnitBoxGeometry() }

// UnitPlaneGeometry builds the 1x1 square in the XZ plane centred on the
// origin, facing +Y, and its mirror facing -Y.
func UnitPlaneGeometry() ([]Vertex, []uint32) { return types.UnitPlaneGeometry() }

// UnitSphereGeometry builds the radius-1 UV sphere of 16 segments by 12 rings.
func UnitSphereGeometry() ([]Vertex, []uint32) { return types.UnitSphereGeometry() }

// DecodeModel parses and decodes one glTF or GLB file's bytes. modelPath is the
// file's storage path: a .gltf file's relative buffer URIs resolve against its
// directory in fsys, external images are named against it, and every report
// names the model by it. Images are named and never opened.
func DecodeModel(data []byte, modelPath string, fsys fs.FS) (*DecodedModel, error) {
	return types.DecodeModel(data, modelPath, fsys)
}

// DecodeDocument decodes one already parsed glTF document, opening nothing.
func DecodeDocument(doc *gltf.Document, modelPath string) (*DecodedModel, error) {
	return types.DecodeDocument(doc, modelPath)
}

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

// VariantFor picks the shader variant a draw needs from what it deforms.
func VariantFor(skinned, morphed bool) ShaderVariant { return types.VariantFor(skinned, morphed) }

// DefaultPbrRecord is glTF's own default material record.
func DefaultPbrRecord() ScenePbrRecord { return types.DefaultPbrRecord() }

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

// SceneShader describes the bundled PBR module under the given options.
func SceneShader(opts ...gfx.ShaderOption) gfx.ShaderDescr { return types.SceneShader(opts...) }

// PackVertices packs standard vertices into the storage layout at the end of
// arena, and reports where they landed, their bounding sphere and the per-mesh
// UV record they were quantised against.
func PackVertices(arena *[]byte, vertices []Vertex) (Span, m.Sphere, SceneMesh) {
	return types.PackVertices(arena, vertices)
}
