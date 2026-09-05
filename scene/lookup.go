package scene

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
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
	// its position in it plus one, so id 0 stays "no mesh".
	meshes []meshRecord
	// unitBox is scene's own cube, baked on first use.
	unitBox MeshRef
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
	l.bundled = bundledPbr(pbrDefaults{
		white:      bake(1, 1, gfx.FormatRGBA8, []byte{0xff, 0xff, 0xff, 0xff}),
		flatNormal: bake(1, 1, gfx.FormatRGBA8, []byte{0x80, 0x80, 0xff, 0xff}),
	})
	return l.bundled
}
