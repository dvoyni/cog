package types

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/slots/gfx"
)

// The friend functions: what canvas's internal/ reads from, or does to, a
// public type's unexported state. Only packages under bundles/canvas can import
// this package, so these are not public API. Each is a field read or a direct
// call, so the flush pays nothing for going through one.

// OpQueueLayers reads OpQueue.ops for canvas's internal/: every layer the queue
// has ever recorded, keyed by layer. A layer that recorded nothing this tick is
// still present with everything zeroed, because reset keeps the keys.
func OpQueueLayers(v *OpQueue) map[Layer]LayerOps { return v.ops }

// OpQueueDefaults points at OpQueue.defaults for canvas's internal/, so the
// fingerprints the queue-wide set takes lazily are memoised for the whole frame.
func OpQueueDefaults(v *OpQueue) *ScopeMaterials { return &v.defaults }

// OpQueueLayout reads one of OpQueue.layouts for canvas's internal/.
func OpQueueLayout(v *OpQueue, layoutID int) []gfx.VertexAttr { return v.layouts[layoutID] }

// OpQueueReset calls OpQueue.reset for canvas's internal/.
func OpQueueReset(v *OpQueue) { v.reset() }

// OpQueueInspect calls OpQueue.inspectOp for canvas's internal/.
func OpQueueInspect(v *OpQueue, layerID Layer, op *DrawOp) Op { return v.inspectOp(layerID, op) }

// LookupResolveSprite returns the atlas entry for a sprite path for canvas's
// internal/, decoding and packing it on first use. The empty path is the white
// texel solid fills draw with.
//
// A zero entry is a sprite that did not load - a missing file, an image that
// would not decode, or one the packer refused - and the draw that asked for it
// draws nothing. It is cached as it is, so nothing is re-opened on a later
// frame; the failure was reported when it happened.
func LookupResolveSprite(v *Lookup, k kernel.Kernel, path string, fsys fs.FS, resources *gfx.ResourceQueue) AtlasEntry {
	return v.sprites.Get(k, spriteDescr(path), fsys,
		spriteUser{packer: v.spritePacker, resources: resources})
}

// LookupResolveStandalone returns the full-image texture a tiled sprite samples
// with repeat addressing, for canvas's internal/, decoding and baking it on
// first use.
func LookupResolveStandalone(v *Lookup, k kernel.Kernel, path string, fsys fs.FS, resources *gfx.ResourceQueue) StandaloneEntry {
	return v.tiled.Get(k, assets.Descr[tiledDescrParams]{Name: path}, fsys, resources)
}

// LookupFontStore reads Lookup.fontStore for canvas's internal/.
func LookupFontStore(v *Lookup) *FontStore { return v.fontStore }

// LookupInvalidateFontsOnResize calls Lookup.invalidateFontsOnResize for
// canvas's internal/.
func LookupInvalidateFontsOnResize(v *Lookup, resources *gfx.ResourceQueue, view *gfx.Viewport) {
	v.invalidateFontsOnResize(resources, view)
}
