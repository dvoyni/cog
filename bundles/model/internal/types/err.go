package types

import "github.com/dvoyni/cog/bundles/model/internal/types/gltf"

// The decoder declares its own reports, because it imports nothing of model's.
// They are named here so the root can re-export them under the names scene has
// always given them.
type (
	ErrModelTextureUnavailable = gltf.ErrModelTextureUnavailable
	ErrModelPrimitiveSkipped   = gltf.ErrModelPrimitiveSkipped
	ErrModelBoundsMissing      = gltf.ErrModelBoundsMissing
	ErrModelNodeDuplicated     = gltf.ErrModelNodeDuplicated
	ErrModelSkinUnbound        = gltf.ErrModelSkinUnbound
)
