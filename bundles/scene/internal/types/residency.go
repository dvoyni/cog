package types

import (
	"io/fs"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
)

// Everything that outlives a frame is model's: the Lookup with its model and
// texture caches and its mesh table, the loaded models' records, and the
// vocabulary a draw of one names. scene's recording and flush still name them,
// so they are named here under the names scene has always used.
type (
	Lookup             = model.Lookup
	LookupAccess       = model.LookupAccess
	LookupDeviceAccess = model.LookupDeviceAccess

	MeshRef = model.MeshRef

	ClipPlay    = model.ClipPlay
	SkinBuffers = model.SkinBuffers

	LightDescr = model.LightDescr

	ScenePbrRecord = model.ScenePbrRecord
)

const (
	MeshDurable = model.MeshDurable

	LightPoint = model.LightPoint
	LightSpot  = model.LightSpot
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

// NewLookup, NewLookupAccess and NewLookupDeviceAccess are model's, and scene's
// root keeps forwarding to them until the sweep rewrites its callers.
func NewLookup() *Lookup { return model.NewLookup() }

func NewLookupAccess(k kernel.Kernel, lookup *Lookup) LookupAccess {
	return model.NewLookupAccess(k, lookup)
}

func NewLookupDeviceAccess(
	k kernel.Kernel, lookup *Lookup, fsys fs.FS, resources *gfx.ResourceQueue,
) LookupDeviceAccess {
	return model.NewLookupDeviceAccess(k, lookup, fsys, resources)
}
