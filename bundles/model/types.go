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

// The storage vertex: the byte offset of each attribute in the buffer a
// standard or skinned mesh uploads, and the two strides. The packers write to
// these offsets and the two layouts Vertex and SkinnedVertexLayout report read
// from them.
const (
	StoragePosition = types.StoragePosition
	StorageNormal   = types.StorageNormal
	StorageTangent  = types.StorageTangent
	StorageUV0      = types.StorageUV0
	StorageUV1      = types.StorageUV1
	StorageColor    = types.StorageColor
	StorageStride   = types.StorageStride

	StorageJoints        = types.StorageJoints
	StorageWeights       = types.StorageWeights
	StorageSkinnedStride = types.StorageSkinnedStride
)

// StandardVertexAttrs is how many of the skinned layout's eight rows the
// standard layout keeps.
const StandardVertexAttrs = types.StandardVertexAttrs

// DecodedModel is one glTF file decoded into plain data: vertex attribute
// arrays and index lists, unbaked curves and skins, morph target floats,
// material parameters as plain values, image references, lights, and the
// flattened scene walk. It holds no GPU layout and no handle. Vertex packing,
// the pose bake, morph packing and the PBR record fill are applied to it
// after the decoder returns.
type DecodedModel = types.DecodedModel

// The parts of a DecodedModel.
type (
	DecodedGeometry      = types.DecodedGeometry
	DecodedMorphTarget   = types.DecodedMorphTarget
	DecodedPrimitive     = types.DecodedPrimitive
	DecodedScene         = types.DecodedScene
	DecodedSceneNode     = types.DecodedSceneNode
	DecodedMaterial      = types.DecodedMaterial
	DecodedSlot          = types.DecodedSlot
	DecodedAlphaMode     = types.DecodedAlphaMode
	DecodedImage         = types.DecodedImage
	DecodedLight         = types.DecodedLight
	DecodedJoint         = types.DecodedJoint
	DecodedSkin          = types.DecodedSkin
	DecodedNode          = types.DecodedNode
	DecodedClip          = types.DecodedClip
	DecodedClipNode      = types.DecodedClipNode
	DecodedClipWeights   = types.DecodedClipWeights
	DecodedCurve         = types.DecodedCurve
	DecodedWeightCurve   = types.DecodedWeightCurve
	DecodedInterpolation = types.DecodedInterpolation
)

// A decoded material's glTF alphaMode.
const (
	DecodedAlphaOpaque = types.DecodedAlphaOpaque
	DecodedAlphaMask   = types.DecodedAlphaMask
	DecodedAlphaBlend  = types.DecodedAlphaBlend
)

// A decoded curve's interpolation between keyframes.
const (
	DecodedInterpolationLinear      = types.DecodedInterpolationLinear
	DecodedInterpolationStep        = types.DecodedInterpolationStep
	DecodedInterpolationCubicSpline = types.DecodedInterpolationCubicSpline
)

// The five texture slots of a decoded material, in the order its Slots holds
// them.
const (
	DecodedSlotBaseColor         = types.DecodedSlotBaseColor
	DecodedSlotMetallicRoughness = types.DecodedSlotMetallicRoughness
	DecodedSlotNormal            = types.DecodedSlotNormal
	DecodedSlotOcclusion         = types.DecodedSlotOcclusion
	DecodedSlotEmissive          = types.DecodedSlotEmissive
	DecodedSlotCount             = types.DecodedSlotCount
)

// DecodedNoImage is the image index of a decoded material slot the file could
// not name a picture for.
const DecodedNoImage = types.DecodedNoImage
