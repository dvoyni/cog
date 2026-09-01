package canvas

import (
	"encoding/binary"
	"github.com/dvoyni/cog/m"
	"math"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
)

var defaultMaterial = gfx.MaterialWithState(
	gfx.ShaderWithResource(spriteShaderPath),
	gfx.MaterialState{Blend: gfx.BlendAlpha, DepthTest: false},
	gfx.ColorParam("tint", m.Color{R: 1, G: 1, B: 1, A: 1}),
	gfx.ColorParam("keyColor", m.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}),
)

// TextureSlot and SamplerSlot are the reserved Canvas shader parameter names for
// the texture and sampler a draw samples. Bind them through a material or through
// DrawTriangles params; the built-in triangle shader samples them with raw uv.
const (
	TextureSlot = "canvasTexture"
	SamplerSlot = "canvasSampler"
)

// defaultTrianglesMaterial samples canvasTexture; untextured draws bind no
// texture param, so the backend's built-in white texture is used (texture id 0).
// It must NOT carry an inline TextureWithBytes default: that would re-bake a
// temporary texture on every draw.
var defaultTrianglesMaterial = gfx.MaterialWithState(
	gfx.ShaderWithResource(trianglesShaderPath),
	gfx.MaterialState{Blend: gfx.BlendAlpha, DepthTest: false},
	gfx.SamplerParam(SamplerSlot, gfx.AddressClamp, gfx.FilterLinear),
	gfx.ColorParam("keyColor", m.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}),
)

// defaultSpriteBatchMaterial draws many sprites/glyphs in one instanced call:
// per-instance data comes from the "instances" storage buffer, and the texture,
// sampler, and shared uniforms are bound per draw.
var defaultSpriteBatchMaterial = gfx.MaterialWithState(
	gfx.ShaderWithResource(spriteBatchShaderPath),
	gfx.MaterialState{Blend: gfx.BlendAlpha, DepthTest: false},
)

// SpriteInstance is one per-instance record the spritebatch shader reads from its
// storage buffer. Field order and size must match spritebatch.wgsl's SpriteInstance
// (6 vec4, 96 bytes, no padding), so a []SpriteInstance uploads directly as the
// instance buffer for the instanced draw.
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

func DefaultMaterial() *gfx.MaterialDescr { return &defaultMaterial }

func DefaultTrianglesMaterial() *gfx.MaterialDescr { return &defaultTrianglesMaterial }

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
