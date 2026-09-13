package canvas

import (
	"github.com/dvoyni/cog/bundles/canvas/internal"
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/libs/m"
)

// The four reserved canvas parameter names: the names canvas consumes itself and
// never forwards to a material.
//
// TextureSlot and SamplerSlot are the texture and sampler a draw samples; bind
// them through a material or through DrawTriangles params, and the built-in
// triangle shader samples them with raw uv. TintSlot and KeyColorSlot are fields
// of the sprite instance record, which canvas reads out of a draw's parameters
// by name and packs in, so a custom sprite shader reads them from the shared
// VertexOut rather than from a uniform and may not reclaim either name.
//
// All four are constants rather than string literals scattered through the
// flush, because a reserved name spelled in four places is reserved only by
// coincidence.
const (
	TextureSlot  = internal.TextureSlot
	SamplerSlot  = internal.SamplerSlot
	TintSlot     = internal.TintSlot
	KeyColorSlot = internal.KeyColorSlot
)

// SpriteInstance is one per-instance record the sprite shader reads from its
// storage buffer. Field order and size must match the shader's SpriteInstance
// (6 vec4, 96 bytes, no padding), so a []SpriteInstance uploads directly as the
// instance buffer for the instanced draw.
//
// The record is frozen. A custom sprite material may replace both entry points
// and append members to the uniform block, but it may not change this: the Go
// struct is hand-mirrored against the WGSL one and uploaded by direct
// reinterpretation, so a divergence is a silent misread rather than a compile
// error. TestSpriteInstanceMatchesTheShaderRecord is what catches it.
type SpriteInstance struct {
	Transform0 m.Vec4 // position.xy, size.xy
	Transform1 m.Vec4 // origin.xy, sine, cosine
	Frame      m.Vec4 // uv rect (x0, y0, x1, y1)
	Tint       m.Vec4
	Misc       m.Vec4 // atlasLayer, unused, unused, unused
	KeyColor   m.Vec4
}

// DefaultKeyColor is the key colour a triangles draw gets when it names none:
// mid grey, which leaves the ramp a no-op on artwork that was not authored for
// keying. It is exported because a custom triangles material has to carry it as
// its own default - keyColor is a reserved name canvas packs into the uniform
// block, and a material that omits it keys every texel against black.
func DefaultKeyColor() m.Color { return internal.DefaultKeyColor }

// DefaultMaterial returns the built-in sprite material: the instanced atlas
// draw every sprite, glyph, inline icon and fill reaches the screen through.
// Passing it explicitly batches identically to passing nil, because the batch
// key takes the material's fingerprint rather than the fact of naming one.
func DefaultMaterial() *gfx.MaterialDescr { return &internal.DefaultSpriteMaterial }

// DefaultTrianglesMaterial returns the built-in triangles material: sample
// canvasTexture through the key-colour ramp, times vertex colour.
func DefaultTrianglesMaterial() *gfx.MaterialDescr { return &internal.DefaultTrianglesMaterial }

// TextureMaterial returns the built-in material a texture-sourced draw uses:
// sample the texture bound to TextureSlot, multiply by vertex colour, clip. Pass
// it to DrawTriangles to get that behaviour for geometry recorded by hand.
func TextureMaterial() *gfx.MaterialDescr { return &internal.DefaultTextureMaterial }
