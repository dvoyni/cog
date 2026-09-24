package internal

import (
	"encoding/binary"
	"math"
	"unsafe"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// canvasSampler is the sampler a 2D draw wants: one filter for magnification,
// minification and mip selection alike, since canvas never generates mipmaps.
func canvasSampler(u, v gfx.AddressMode, filter gfx.FilterMode) gfx.SamplerDesc {
	return gfx.SamplerDesc{AddressU: u, AddressV: v, Mag: filter, Min: filter, Mip: filter}
}

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

// defaultKeyColor is what a draw that names no key colour gets: the mid-grey
// that makes the shader's ramp reproduce the grey ramp the artist painted,
// leaving an unkeyed sprite looking like its own artwork rather than like a
// player colour nobody chose.
//
// It is sRGB 0.5 rather than linear 0.5 because 0.5 is what the artist's
// colour picker said, and the ramp puts keyColor exactly at that intensity.
// This is the one canvas constant where the two spaces differ visibly, which
// is why it is named once and referenced everywhere else. It is a variable only
// because m.Color has no constant form; nothing assigns it.
var defaultKeyColor = m.NewColorSrgb(0.5, 0.5, 0.5, 1)

// The four reserved canvas parameter names: the names canvas consumes itself and
// never forwards to a material. The root re-exports them; see
// canvas.TextureSlot.
const (
	TextureSlot  = "canvasTexture"
	SamplerSlot  = "canvasSampler"
	TintSlot     = "tint"
	KeyColorSlot = "keyColor"
)

// defaultTrianglesMaterial samples canvasTexture; untextured draws bind no
// texture param, so the backend's built-in white texture is used (texture id 0).
// It must NOT carry an inline TextureWithBytes default: that would re-bake a
// temporary texture on every draw.
var defaultTrianglesMaterial = gfx.MaterialWithState(
	gfx.ShaderWithResource(TrianglesShaderPath),
	gfx.StateOverlay2D(),
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
	gfx.ShaderWithResource(TextureShaderPath),
	gfx.StateOverlay2D(),
	gfx.SamplerParam(SamplerSlot, gfx.SamplerDesc{}),
)

// defaultSpriteMaterial draws many sprites, glyphs and fills in one instanced
// call: per-instance data comes from the "instances" storage buffer, and the
// texture, sampler and shared uniforms are bound per draw. It is the only sprite
// material canvas has, and a lone sprite is its one-instance case.
var defaultSpriteMaterial = gfx.MaterialWithState(
	gfx.ShaderWithResource(SpriteShaderPath),
	gfx.StateOverlay2D(),
)

// DefaultKeyColor returns the key colour a draw that names none gets; see
// canvas.DefaultKeyColor.
func DefaultKeyColor() m.Color { return defaultKeyColor }

// DefaultMaterial returns the built-in sprite material; see
// canvas.DefaultMaterial.
func DefaultMaterial() *gfx.MaterialDescr { return &defaultSpriteMaterial }

// DefaultTrianglesMaterial returns the built-in triangles material; see
// canvas.DefaultTrianglesMaterial.
func DefaultTrianglesMaterial() *gfx.MaterialDescr { return &defaultTrianglesMaterial }

// TextureMaterial returns the built-in texture material; see
// canvas.TextureMaterial.
func TextureMaterial() *gfx.MaterialDescr { return &defaultTextureMaterial }

// The three built-ins indexed by family, with their fingerprints taken once. A
// draw that names no material of its own adopts one of these as its batch key,
// so computing the hash per draw would be per-draw work for a value that cannot
// change.
var (
	builtinMaterials = [3]*gfx.MaterialDescr{
		FamilySprite:    &defaultSpriteMaterial,
		FamilyTriangles: &defaultTrianglesMaterial,
		FamilyTexture:   &defaultTextureMaterial,
	}
	builtinFingerprints = [3]uint64{
		FamilySprite:    defaultSpriteMaterial.Fingerprint(),
		FamilyTriangles: defaultTrianglesMaterial.Fingerprint(),
		FamilyTexture:   defaultTextureMaterial.Fingerprint(),
	}
)

var triangleVertexLayout = [...]gfx.VertexAttr{
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Position)), gfx.Float32x2),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Color)), gfx.Float32x4),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.UV)), gfx.Float32x2),
}
