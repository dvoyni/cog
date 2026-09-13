package internal

import (
	"unsafe"

	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/libs/m"
)

// DefaultKeyColor is what a draw that names no key colour gets: the mid-grey
// that makes the shader's ramp reproduce the grey ramp the artist painted,
// leaving an unkeyed sprite looking like its own artwork rather than like a
// player colour nobody chose.
//
// It is sRGB 0.5 rather than linear 0.5 because 0.5 is what the artist's
// colour picker said, and the ramp puts keyColor exactly at that intensity.
// This is the one canvas constant where the two spaces differ visibly, which
// is why it is named once and referenced everywhere else. It is a variable only
// because m.Color has no constant form; nothing assigns it.
var DefaultKeyColor = m.NewColorSrgb(0.5, 0.5, 0.5, 1)

// The four reserved canvas parameter names: the names canvas consumes itself and
// never forwards to a material. The contract root re-exports them; see
// canvas.TextureSlot.
const (
	TextureSlot  = "canvasTexture"
	SamplerSlot  = "canvasSampler"
	TintSlot     = "tint"
	KeyColorSlot = "keyColor"
)

// DefaultTrianglesMaterial samples canvasTexture; untextured draws bind no
// texture param, so the backend's built-in white texture is used (texture id 0).
// It must NOT carry an inline TextureWithBytes default: that would re-bake a
// temporary texture on every draw.
var DefaultTrianglesMaterial = gfx.MaterialWithState(
	gfx.ShaderWithResource(TrianglesShaderPath),
	gpu.StateOverlay2D,
	gfx.SamplerParam(SamplerSlot, gpu.SamplerDesc{}),
	gfx.ColorParam(KeyColorSlot, DefaultKeyColor),
)

// DefaultTextureMaterial samples an arbitrary gfx texture as it is. It is a
// second built-in rather than a parameter on the triangle one because the
// difference is the shader: triangles.wgsl runs the key-colour ramp over every
// texel, which is right for artwork and silent damage to a rendered image, and
// no key colour turns the ramp off. Like the triangle material it carries no
// inline texture default, because that would re-bake a temporary on every draw.
var DefaultTextureMaterial = gfx.MaterialWithState(
	gfx.ShaderWithResource(TextureShaderPath),
	gpu.StateOverlay2D,
	gfx.SamplerParam(SamplerSlot, gpu.SamplerDesc{}),
)

// DefaultSpriteMaterial draws many sprites, glyphs and fills in one instanced
// call: per-instance data comes from the "instances" storage buffer, and the
// texture, sampler and shared uniforms are bound per draw. It is the only sprite
// material canvas has, and a lone sprite is its one-instance case.
var DefaultSpriteMaterial = gfx.MaterialWithState(
	gfx.ShaderWithResource(SpriteShaderPath),
	gpu.StateOverlay2D,
)

// The three built-ins indexed by family, with their fingerprints taken once. A
// draw that names no material of its own adopts one of these as its batch key,
// so computing the hash per draw would be per-draw work for a value that cannot
// change.
var (
	builtinMaterials = [3]*gfx.MaterialDescr{
		FamilySprite:    &DefaultSpriteMaterial,
		FamilyTriangles: &DefaultTrianglesMaterial,
		FamilyTexture:   &DefaultTextureMaterial,
	}
	builtinFingerprints = [3]uint64{
		FamilySprite:    DefaultSpriteMaterial.Fingerprint(),
		FamilyTriangles: DefaultTrianglesMaterial.Fingerprint(),
		FamilyTexture:   DefaultTextureMaterial.Fingerprint(),
	}
)

var triangleVertexLayout = [...]gfx.VertexAttr{
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Position)), gpu.Float32x2),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Color)), gpu.Float32x4),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.UV)), gpu.Float32x2),
}
