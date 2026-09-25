package canvas

import (
	"github.com/dvoyni/cog/bundles/canvas/internal"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

// NewLookup builds an empty Lookup resource with canvas's default atlas sizes.
// The Canvas plugin creates its own from its configuration; this constructor
// lets tests and embedders build a Lookup to drive a LookupAccess directly.
func NewLookup() *Lookup { return internal.NewLookup() }

// NewLookupAccess builds a scoped facade. Call it inside a handler that holds
// the *Lookup write lock and the storage.FileSystem read lock; never store the
// result.
func NewLookupAccess(k kernel.Kernel, lookup *Lookup, filesystem storage.FileSystem) LookupAccess {
	return internal.NewLookupAccess(k, lookup, filesystem)
}

// NewLookupDeviceAccess builds a scoped facade over the unload verbs. Call it
// inside a handler that holds the *Lookup and *gfx.ResourceQueue write locks;
// never store the result.
func NewLookupDeviceAccess(k kernel.Kernel, lookup *Lookup, resources *gfx.ResourceQueue) LookupDeviceAccess {
	return internal.NewLookupDeviceAccess(k, lookup, resources)
}

// LayerTransform returns the scale and offset mapping a layer's world
// coordinates to logical viewport coordinates as world*scale + offset, matching
// how SetLayerTransform renders. A zero-area window yields the identity. Callers
// bake destination sub-rectangles and min/max scale caps into the window they
// pass; this reports the resulting transform.
func LayerTransform(window m.Rect, aspect AspectMode, viewport m.Vec2) (scale, offset m.Vec2) {
	return internal.LayerTransform(window, aspect, viewport)
}

// WorldToScreen maps a layer world point to logical viewport coordinates.
func WorldToScreen(window m.Rect, aspect AspectMode, viewport, world m.Vec2) m.Vec2 {
	return internal.WorldToScreen(window, aspect, viewport, world)
}

// ScreenToWorld inverts WorldToScreen so input code can hit-test in world space.
func ScreenToWorld(window m.Rect, aspect AspectMode, viewport, screen m.Vec2) m.Vec2 {
	return internal.ScreenToWorld(window, aspect, viewport, screen)
}

// DefaultKeyColor is the key colour a triangles draw gets when it names none:
// mid grey, which leaves the ramp a no-op on artwork that was not authored for
// keying. It is exported because a custom triangles material has to carry it as
// its own default - keyColor is a reserved name canvas packs into the uniform
// block, and a material that omits it keys every texel against black.
func DefaultKeyColor() m.Color { return internal.DefaultKeyColor() }

// DefaultMaterial returns the built-in sprite material: the instanced atlas
// draw every sprite, glyph, inline icon and fill reaches the screen through.
// Passing it explicitly batches identically to passing nil, because the batch
// key takes the material's fingerprint rather than the fact of naming one.
func DefaultMaterial() *gfx.MaterialDescr { return internal.DefaultMaterial() }

// DefaultTrianglesMaterial returns the built-in triangles material: sample
// canvasTexture through the key-colour ramp, times vertex colour.
func DefaultTrianglesMaterial() *gfx.MaterialDescr { return internal.DefaultTrianglesMaterial() }

// TextureMaterial returns the built-in material a texture-sourced draw uses:
// sample the texture bound to TextureSlot, multiply by vertex colour, clip. Pass
// it to DrawTriangles to get that behaviour for geometry recorded by hand.
func TextureMaterial() *gfx.MaterialDescr { return internal.TextureMaterial() }

// DefaultHaloProfile returns the profile measured off painted art - 84% of the
// halo pixels in feuds' militiaman.png sit on a plateau for the first fifth of
// the band and then fall almost exactly linearly - filled in for a caller to
// edit.
func DefaultHaloProfile() HaloProfile { return internal.DefaultHaloProfile() }

// HaloMaterialSet returns the material set that haloes a layer: name it over a
// layer with SetLayerMaterial, record the marks there, and record the same marks
// again on the layer above under no material at all.
//
//	write.SetLayerMaterial(layerHalo, canvas.HaloMaterialSet(canvas.DefaultHaloProfile()))
//	drawCluster(write, layerHalo, haloColor)
//	drawCluster(write, layerInk, inkColor)
//
// The halo material paints a soft outward band and no mark at all, so no band
// can reach anybody's ink and overlap is ordinary painter's order. It covers the
// whole sprite family - Sprite, Text glyphs, inline icons, FillRect, Line,
// StrokeRect - because all of them are sprite instances in one instanced atlas
// draw.
//
// Two layers is the idiom rather than a requirement, but is what to write: a
// draw added to the ink layer later lands in the ink run rather than becoming a
// coloured ghost.
//
// The profile arrives as the set's per-batch parameters, which is the only
// frequency it has. It cannot be named at a draw - every valued parameter
// constructor sets HasValue, and a draw's valued parameter becomes a per-sprite
// storage array while a scope's is shared - so **two reaches are two scopes**.
// That is the idiom, not a limitation. The material carries the defaults as its
// own parameters, so a hand-assembled MaterialSet{Sprite: ...} renders at reach
// 6 rather than rendering nothing.
//
// **Dedicate the layer to the marks being haloed.** Triangles and Texture stay
// nil, so a DrawTriangles or DrawTexture recorded on the halo layer paints as
// itself rather than as a band; and every sprite-family draw on it, including
// ones another subsystem adds later, becomes a band in its own colour.
//
// There is deliberately no HaloMaterial() beside DefaultMaterial(), despite the
// symmetry: a draw naming its own material takes none of its scope's parameters,
// so naming the halo at a draw would render at the material's own defaults and
// silently ignore every profile on the layer above it. A scope is the only way
// in.
func HaloMaterialSet(profile HaloProfile) MaterialSet { return internal.HaloMaterialSet(profile) }
