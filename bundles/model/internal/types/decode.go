package types

import (
	"io/fs"

	"github.com/dvoyni/cog/bundles/model/internal/types/gltf"
	qgltf "github.com/qmuntal/gltf"
)

// The decoder's output. The decoder declares it, because it imports nothing of
// model's; model names it here so the root can.
type (
	DecodedModel         = gltf.Model
	DecodedGeometry      = gltf.Geometry
	DecodedMorphTarget   = gltf.MorphTarget
	DecodedPrimitive     = gltf.Primitive
	DecodedScene         = gltf.Scene
	DecodedSceneNode     = gltf.SceneNode
	DecodedMaterial      = gltf.Material
	DecodedSlot          = gltf.Slot
	DecodedAlphaMode     = gltf.AlphaMode
	DecodedImage         = gltf.Image
	DecodedLight         = gltf.Light
	DecodedJoint         = gltf.Joint
	DecodedSkin          = gltf.Skin
	DecodedNode          = gltf.Node
	DecodedClip          = gltf.Clip
	DecodedClipNode      = gltf.ClipNode
	DecodedClipWeights   = gltf.ClipWeights
	DecodedCurve         = gltf.Curve
	DecodedWeightCurve   = gltf.WeightCurve
	DecodedInterpolation = gltf.Interpolation
)

const (
	DecodedAlphaOpaque = gltf.AlphaOpaque
	DecodedAlphaMask   = gltf.AlphaMask
	DecodedAlphaBlend  = gltf.AlphaBlend

	DecodedInterpolationLinear      = gltf.InterpolationLinear
	DecodedInterpolationStep        = gltf.InterpolationStep
	DecodedInterpolationCubicSpline = gltf.InterpolationCubicSpline

	DecodedSlotBaseColor         = gltf.SlotBaseColor
	DecodedSlotMetallicRoughness = gltf.SlotMetallicRoughness
	DecodedSlotNormal            = gltf.SlotNormal
	DecodedSlotOcclusion         = gltf.SlotOcclusion
	DecodedSlotEmissive          = gltf.SlotEmissive
	DecodedSlotCount             = gltf.SlotCount

	DecodedNoImage = gltf.NoImage
)

// DecodeModel parses and decodes one glTF or GLB file's bytes.
func DecodeModel(data []byte, modelPath string, fsys fs.FS) (*DecodedModel, error) {
	return gltf.Decode(data, modelPath, fsys)
}

// DecodeDocument decodes one parsed glTF document.
func DecodeDocument(doc *qgltf.Document, modelPath string) (*DecodedModel, error) {
	return gltf.DecodeDocument(doc, modelPath)
}
