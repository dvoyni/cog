package types

import (
	"github.com/dvoyni/cog/bundles/model"
)

// MeshTemporary is the source of a frame-local mesh, which the recording mints
// and the flush resolves. model knows only its own durable meshes, so scene
// declares its source past them.
const MeshTemporary = model.MeshDurable + 1

// TemporaryMeshID is the bit a temporary mesh's public id carries. The two
// sources allocate independent dense ranges, and the sort key is one uint32 of
// meshID, so without a bit to tell them apart a temporary mesh and a durable
// one would collide in it and batch as though they were the same geometry.
// MeshRef.ID sets it on every ref whose source is past model's own.
const TemporaryMeshID uint32 = 1 << 31
