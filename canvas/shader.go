package canvas

import (
	"encoding/binary"
	"github.com/dvoyni/cog/m"
	"math"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
)

// defaultKeyColor is what a draw that names no key colour gets: the mid-grey
// that makes the shader's ramp reproduce the grey ramp the artist painted,
// leaving an unkeyed sprite looking like its own artwork rather than like a
// player colour nobody chose.
//
// It is sRGB 0.5 rather than linear 0.5 because 0.5 is what the artist's
// colour picker said, and the ramp puts keyColor exactly at that intensity.
// This is the one canvas constant where the two spaces differ visibly, which
// is why it is named once and referenced everywhere else.
var defaultKeyColor = m.NewColorSrgb(0.5, 0.5, 0.5, 1)

// canvasSampler is the sampler a 2D draw wants: one filter for magnification,
// minification and mip selection alike, since canvas never generates mipmaps.
func canvasSampler(u, v gfx.AddressMode, filter gfx.FilterMode) gfx.SamplerDesc {
	return gfx.SamplerDesc{AddressU: u, AddressV: v, Mag: filter, Min: filter, Mip: filter}
}

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
	TextureSlot  = "canvasTexture"
	SamplerSlot  = "canvasSampler"
	TintSlot     = "tint"
	KeyColorSlot = "keyColor"
)

// reservedName reports whether canvas consumes a parameter name itself.
//
// A reserved name never becomes a per-instance array and never enters the sprite
// batch key. That second half is load-bearing: tint and keyColor arrive as draw
// parameters and are consumed into the instance record, so keying on them would
// split a batch whose draws differ only in tint - the exact merge the instanced
// path exists to make.
func reservedName(name string) bool {
	switch name {
	case TextureSlot, SamplerSlot, TintSlot, KeyColorSlot:
		return true
	}
	return false
}

// defaultTrianglesMaterial samples canvasTexture; untextured draws bind no
// texture param, so the backend's built-in white texture is used (texture id 0).
// It must NOT carry an inline TextureWithBytes default: that would re-bake a
// temporary texture on every draw.
var defaultTrianglesMaterial = gfx.MaterialWithState(
	gfx.ShaderWithResource(trianglesShaderPath),
	gfx.StateOverlay2D,
	gfx.SamplerParam(SamplerSlot, gfx.SamplerDesc{}),
	gfx.ColorParam(KeyColorSlot, defaultKeyColor),
)

// defaultTextureMaterial samples an arbitrary gfx texture as it is. It is a
// second built-in rather than a parameter on the triangle one because the
// difference is the shader: triangles.wgsl runs the key-colour ramp over every
// texel, which is right for artwork and silent damage to a rendered image, and
// no key colour turns the ramp off. Like the triangle material it carries no
// inline texture default, because that would re-bake a temporary on every draw.
var defaultTextureMaterial = gfx.MaterialWithState(
	gfx.ShaderWithResource(textureShaderPath),
	gfx.StateOverlay2D,
	gfx.SamplerParam(SamplerSlot, gfx.SamplerDesc{}),
)

// defaultSpriteMaterial draws many sprites, glyphs and fills in one instanced
// call: per-instance data comes from the "instances" storage buffer, and the
// texture, sampler and shared uniforms are bound per draw. It is the only sprite
// material canvas has, and a lone sprite is its one-instance case.
var defaultSpriteMaterial = gfx.MaterialWithState(
	gfx.ShaderWithResource(spriteShaderPath),
	gfx.StateOverlay2D,
)

// The three built-ins indexed by family, with their fingerprints taken once. A
// draw that names no material of its own adopts one of these as its batch key,
// so computing the hash per draw would be per-draw work for a value that cannot
// change.
var (
	builtinMaterials = [3]*gfx.MaterialDescr{
		familySprite:    &defaultSpriteMaterial,
		familyTriangles: &defaultTrianglesMaterial,
		familyTexture:   &defaultTextureMaterial,
	}
	builtinFingerprints = [3]uint64{
		familySprite:    defaultSpriteMaterial.Fingerprint(),
		familyTriangles: defaultTrianglesMaterial.Fingerprint(),
		familyTexture:   defaultTextureMaterial.Fingerprint(),
	}
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

// spriteInstanceBytes reinterprets a slice of instances as the raw bytes uploaded
// to the storage buffer. GPU targets (desktop amd64, wasm) are little-endian, so
// the in-memory layout is the wire layout — same reinterpretation OpQueue uses for
// vertices.
func spriteInstanceBytes(instances []SpriteInstance) []byte {
	if len(instances) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&instances[0])), len(instances)*int(unsafe.Sizeof(SpriteInstance{})))
}

// colorVec converts a color to the vec4 the instance encoder expects.
func colorVec(c m.Color) m.Vec4 { return m.Vec4{X: c.R, Y: c.G, Z: c.B, W: c.A} }

var triangleVertexLayout = [...]gfx.VertexAttr{
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Position)), gfx.Float32x2),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Color)), gfx.Float32x4),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.UV)), gfx.Float32x2),
}

// DefaultKeyColor is the key colour a triangles draw gets when it names none:
// mid grey, which leaves the ramp a no-op on artwork that was not authored for
// keying. It is exported because a custom triangles material has to carry it as
// its own default - keyColor is a reserved name canvas packs into the uniform
// block, and a material that omits it keys every texel against black.
func DefaultKeyColor() m.Color { return defaultKeyColor }

// DefaultMaterial returns the built-in sprite material: the instanced atlas
// draw every sprite, glyph, inline icon and fill reaches the screen through.
// Passing it explicitly batches identically to passing nil, because the batch
// key takes the material's fingerprint rather than the fact of naming one.
func DefaultMaterial() *gfx.MaterialDescr { return &defaultSpriteMaterial }

func DefaultTrianglesMaterial() *gfx.MaterialDescr { return &defaultTrianglesMaterial }

// TextureMaterial returns the built-in material a texture-sourced draw uses:
// sample the texture bound to TextureSlot, multiply by vertex colour, clip. Pass
// it to DrawTriangles to get that behaviour for geometry recorded by hand.
func TextureMaterial() *gfx.MaterialDescr { return &defaultTextureMaterial }

func unitQuadBytes() (vertices, indices []byte) {
	vertices = make([]byte, 4*2*4)
	positions := [...]float32{0, 0, 1, 0, 1, 1, 0, 1}
	for i, value := range positions {
		binary.LittleEndian.PutUint32(vertices[i*4:], math.Float32bits(value))
	}
	indices = make([]byte, 6*4)
	for i, value := range [...]uint32{0, 1, 2, 0, 2, 3} {
		binary.LittleEndian.PutUint32(indices[i*4:], value)
	}
	return vertices, indices
}
