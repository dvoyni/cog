package types

import "github.com/dvoyni/cog/slots/gfx"

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

// LookupSprites reads Lookup.sprites for canvas's internal/.
func LookupSprites(v *Lookup) *Atlas { return v.sprites }

// LookupFonts reads Lookup.fonts, the glyph atlas, for canvas's internal/.
func LookupFonts(v *Lookup) *Atlas { return v.fonts }

// LookupFontStore reads Lookup.fontStore for canvas's internal/.
func LookupFontStore(v *Lookup) *FontStore { return v.fontStore }

// LookupApplyUnloads calls Lookup.applyUnloads for canvas's internal/.
func LookupApplyUnloads(v *Lookup, resources *gfx.ResourceQueue) { v.applyUnloads(resources) }

// LookupInvalidateFontsOnResize calls Lookup.invalidateFontsOnResize for
// canvas's internal/.
func LookupInvalidateFontsOnResize(v *Lookup, resources *gfx.ResourceQueue, view *gfx.Viewport) {
	v.invalidateFontsOnResize(resources, view)
}
