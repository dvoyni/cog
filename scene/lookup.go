package scene

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/storage"
)

// Lookup is the single scene-owned persistent resource. It holds everything
// that outlives a frame — resident models, baked pose and morph buffers, the
// path-keyed texture cache, buffer-built meshes and scene's own unit meshes —
// plus the deferred unloads and bakes the flush applies at the frame boundary.
//
// It never retains a filesystem or GPU handle of its own. Query and mutate it
// only through a scoped LookupAccess.
type Lookup struct {
	config Config
	// meshes is the dense mesh table every MeshRef indexes, and a ref's id is
	// its position in it plus one, so id 0 stays "no mesh". freeMeshes are the
	// slots ReleaseMesh gave back, reissued newest first.
	meshes     []meshRecord
	freeMeshes []uint32
	// layouts interns the vertex layouts durable meshes were baked from.
	layouts layoutCache
	// staging holds the bytes BakeMesh and UpdateMesh copied out of their
	// callers, and pendingMeshes the uploads waiting on them. The arena is
	// handed to gfx wholesale at the flush and a fresh one grown after, rather
	// than reused: BakeBuffer takes the bytes without copying them, so reusing
	// the backing would rewrite an upload still in flight.
	staging       []byte
	pendingMeshes []pendingMesh
	// pendingReleases are the buffers ReleaseMesh gave up, freed at the frame
	// boundary so nothing the frame already recorded draws from a dead buffer.
	pendingReleases []gfx.BufferDescr
	// unit holds scene's own meshes - the box, sphere and plane the debug
	// vocabulary draws - each baked on first use.
	unit [shapeCount]MeshRef
	// models is the model table, keyed by the path that is a model's only cache
	// key, and textures the scene-owned texture cache every resident model's
	// materials bind out of. Neither is refcounted: nothing unloads
	// automatically, so there is nothing for a count to drive.
	models   map[string]*modelEntry
	textures map[textureKey]gfx.TextureDescr
	// defaults are the two 1x1 textures every empty PBR slot binds, baked on
	// first use. hasDefaults rather than a zero test because a baked descriptor
	// has no reserved zero value.
	defaults    pbrDefaults
	hasDefaults bool
	// nullSkin is the group 2 every draw with no animation of its own binds:
	// one identity pose row, one identity joint record and one zero morph
	// delta, baked on first use and shared by every buffer-built draw in the
	// process. Sharing it is what keeps those draws batching together instead
	// of fragmenting group 2.
	nullSkin skinBuffers
	// reported suppresses repeated reports for one model or texture path until
	// it loads successfully or is unloaded - canvas's precedent.
	reported map[string]struct{}
	// bundled is the bundled PBR material, built on first use around the two
	// default textures. It is not a package-level value because those textures
	// are baked resources: the backend may not be Ready() at startup, and a
	// texture baked then would either panic or silently not exist.
	bundled Material
}

func newLookup(config Config) *Lookup { return &Lookup{config: config} }

// NewLookup builds an empty Lookup for the given configuration. The plugin
// creates one internally; this constructor also lets tests and embedders build
// one to drive a LookupAccess directly.
func NewLookup(config Config) *Lookup { return newLookup(config) }

// LookupAccess is the scoped facade every query and mutation of a Lookup goes
// through. Acquire a *Lookup write dependency plus storage.FileSystem in a
// handler, build one with NewLookupAccess, and pass it to consumers for the
// duration of that handler. Never store the result: the handles behind it are
// valid only while the handler holds its locks.
type LookupAccess struct {
	kernel kernel.Kernel
	lookup *Lookup
	fs     storage.FileSystem
}

// NewLookupAccess builds a scoped facade. Call it inside a handler that holds
// the *Lookup write lock and the storage.FileSystem read lock.
func NewLookupAccess(k kernel.Kernel, lookup *Lookup, filesystem storage.FileSystem) LookupAccess {
	return LookupAccess{kernel: k, lookup: lookup, fs: filesystem}
}

// Valid reports whether the facade is backed by a live Lookup.
func (la LookupAccess) Valid() bool { return la.lookup != nil }

// ensureBundled builds the bundled PBR the first time something draws, baking
// the two 1x1 default textures it binds into every absent slot, and returns the
// same material forever after.
//
// 1x1 rather than larger because uploads carry no row-alignment rule and for a
// constant texel every mip level is identical, so there is nothing to generate.
// Both are linear-format: 1.0 is a fixed point of the sRGB transfer curve, so
// the white texel reads 1.0 through an sRGB slot and a linear one alike, and
// the flat normal is not a picture at all.
func (l *Lookup) ensureBundled(bake bakeTextureFunc) Material {
	if l.bundled != nil {
		return l.bundled
	}
	if !l.hasDefaults {
		l.defaults = pbrDefaults{
			white:      bake(1, 1, gfx.FormatRGBA8, []byte{0xff, 0xff, 0xff, 0xff}),
			flatNormal: bake(1, 1, gfx.FormatRGBA8, []byte{0x80, 0x80, 0xff, 0xff}),
		}
		l.hasDefaults = true
	}
	l.bundled = bundledPbr(l.defaults)
	return l.bundled
}

// ensureNullSkin bakes the one-row, one-joint, one-delta skin every draw with
// no animation of its own binds, once.
//
// It exists because a declared binding must be bound. Group 2 is declared by
// the one bundled module, which every draw goes through, and a missing entry
// is not a degraded frame: CreateBindGroup fails the entry-count rule, the
// error is swallowed, encoder.Finish()'s error is dropped, and the whole
// frame's command buffer vanishes with no error anywhere.
//
// The alternative - riding the free rest-frame path with SCENE_NOSKIN unset -
// needs no new mechanism and is correct, but it charges a procedural terrain
// mesh a per-vertex pose fetch and TRS blend for a guaranteed identity.
func (l *Lookup) ensureNullSkin(bake bakeFunc) skinBuffers {
	if l.nullSkin.bound {
		return l.nullSkin
	}
	pose := identityPose()
	joint := identitySkinJoint()
	// One zero delta record, for the same reason as the pose and the joint. A
	// draw that morphs nothing still declares sceneMorphDeltas, and an empty
	// buffer is no buffer at all: BakeBuffer returns nothing for no bytes,
	// which is the unbound binding the whole rule exists to avoid.
	var delta m.Vec4
	l.nullSkin = skinBuffers{
		poses:   bake(recordBytes(&pose)),
		joints:  bake(recordBytes(&joint)),
		morphs:  bake(recordBytes(&delta)),
		bound:   true,
		morphed: true,
	}
	return l.nullSkin
}
