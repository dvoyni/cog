package internal

import (
	"encoding/binary"
	"math"
	"unsafe"

	"github.com/dvoyni/cog/bundles/canvas"
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
	case canvas.TextureSlot, canvas.SamplerSlot, canvas.TintSlot, canvas.KeyColorSlot:
		return true
	}
	return false
}

// spriteInstanceBytes reinterprets a slice of instances as the raw bytes uploaded
// to the storage buffer. GPU targets (desktop amd64, wasm) are little-endian, so
// the in-memory layout is the wire layout — same reinterpretation OpQueue uses for
// vertices.
func spriteInstanceBytes(instances []canvas.SpriteInstance) []byte {
	if len(instances) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&instances[0])), len(instances)*int(unsafe.Sizeof(canvas.SpriteInstance{})))
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
