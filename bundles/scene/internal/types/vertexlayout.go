package types

import "github.com/dvoyni/cog/bundles/model"

// The authoring vertex and the storage layout it packs into are model's: the
// vertex is what model's geometry generators build, and its VertexLayout method
// reports that layout. scene still packs, so it names them here under the names
// its packers and tests have always used.
type (
	Vertex       = model.Vertex
	VertexLayout = model.VertexLayout
)

const (
	storagePosition = model.StoragePosition
	storageNormal   = model.StorageNormal
	storageTangent  = model.StorageTangent
	storageUV0      = model.StorageUV0
	storageUV1      = model.StorageUV1
	storageColor    = model.StorageColor
	StorageStride   = model.StorageStride

	storageJoints        = model.StorageJoints
	storageWeights       = model.StorageWeights
	StorageSkinnedStride = model.StorageSkinnedStride

	StandardVertexAttrs = model.StandardVertexAttrs
)
