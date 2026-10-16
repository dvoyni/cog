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
// batch key. That second half is load-bearing: tint arrives as a draw
// parameter and are consumed into the instance record, so keying on them would
// split a batch whose draws differ only in tint - the exact merge the instanced
// path exists to make.
func reservedName(name string) bool {
	switch name {
	case TextureSlot, SamplerSlot, TintSlot:
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

// tintNeutral is the middle of the tint ramp, canvasTintNeutral in tint.wgsl:
// #6D6D6D, the grey the art paints its player-colour masks in, in the linear
// light the ramp multiplies in. It is the tint channel that leaves a texel as it was
// painted.
const tintNeutral = 0.15292619

// defaultTint is what a draw that names no tint gets: neutral, so a sprite
// draws as its own artwork, at full alpha. It is a variable only because
// m.Color has no constant form; nothing assigns it.
var defaultTint = m.Color{R: tintNeutral, G: tintNeutral, B: tintNeutral, A: 1}

// DefaultTint returns the tint a draw that names none gets; see
// canvas.DefaultTint.
func DefaultTint() m.Color { return defaultTint }

// TintFor returns the tint that draws white art in color: color's rgb scaled
// onto the ramp by the neutral, its alpha as it is. See canvas.TintFor.
func TintFor(color m.Color) m.Color {
	return m.Color{R: color.R * tintNeutral, G: color.G * tintNeutral, B: color.B * tintNeutral, A: color.A}
}

// tintColor is TintFor inverted: the colour white art takes under tint, which
// is what a sprite routed through the triangles family wears as its vertex
// colour, where the colour is a plain multiply rather than the ramp.
func tintColor(tint m.Color) m.Color {
	return m.Color{R: tint.R / tintNeutral, G: tint.G / tintNeutral, B: tint.B / tintNeutral, A: tint.A}
}

// The three reserved canvas parameter names: the names canvas consumes itself
// and never forwards to a material. The root re-exports them; see
// canvas.TextureSlot.
const (
	TextureSlot = "canvasTexture"
	SamplerSlot = "canvasSampler"
	TintSlot    = "tint"
)

// defaultTrianglesMaterial samples canvasTexture; untextured draws bind no
// texture param, so the backend's built-in white texture is used (texture id 0).
// It must NOT carry an inline TextureWithBytes default: that would re-bake a
// temporary texture on every draw.
var defaultTrianglesMaterial = MaterialWithState(
	gfx.ShaderWithResource(TrianglesShaderPath),
	gfx.StateOverlay2D(),
	gfx.ShaderParameterSampler(SamplerSlot, gfx.SamplerDesc{}),
)

// defaultTextureMaterial samples an arbitrary gfx texture as it is. It is a
// second built-in rather than the triangle one so that a scope can name a
// material over texture-sourced draws alone. Like the triangle material it
// carries no inline texture default, because that would re-bake a temporary on
// every draw.
var defaultTextureMaterial = MaterialWithState(
	gfx.ShaderWithResource(TextureShaderPath),
	gfx.StateOverlay2D(),
	gfx.ShaderParameterSampler(SamplerSlot, gfx.SamplerDesc{}),
)

// defaultSpriteMaterial draws many sprites, glyphs and fills in one instanced
// call: per-instance data comes from the "instances" storage buffer, and the
// texture, sampler and shared uniforms are bound per draw. It is the only sprite
// material canvas has, and a lone sprite is its one-instance case.
var defaultSpriteMaterial = MaterialWithState(
	gfx.ShaderWithResource(SpriteShaderPath),
	gfx.StateOverlay2D(),
)

// DefaultMaterial returns the built-in sprite material; see
// canvas.DefaultMaterial.
func DefaultMaterial() *Material { return &defaultSpriteMaterial }

// DefaultTrianglesMaterial returns the built-in triangles material; see
// canvas.DefaultTrianglesMaterial.
func DefaultTrianglesMaterial() *Material { return &defaultTrianglesMaterial }

// TextureMaterial returns the built-in texture material; see
// canvas.TextureMaterial.
func TextureMaterial() *Material { return &defaultTextureMaterial }

// The three built-ins indexed by family, with their fingerprints taken once. A
// draw that names no material of its own adopts one of these as its batch key,
// so computing the hash per draw would be per-draw work for a value that cannot
// change.
var (
	builtinMaterials = [3]*Material{
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

var triangleVertexLayout = [...]gfx.VertexAttribute{
	{Offset: int(unsafe.Offsetof(Vertex{}.Position)), Type: gfx.Float32x2},
	{Offset: int(unsafe.Offsetof(Vertex{}.Color)), Type: gfx.Float32x4},
	{Offset: int(unsafe.Offsetof(Vertex{}.UV)), Type: gfx.Float32x2},
}
