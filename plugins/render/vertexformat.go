package render

import "github.com/dvoyni/cog/plugins/gfx"

// VertexFormat is the layout of one vertex attribute's value, each format
// gfx's of the same name: its components' count, size and type, and how the
// vertex shader reads them, as a float, normalized or not, or an integer.
type VertexFormat uint32

const (
	VertexFormatUint8x2      = VertexFormat(gfx.VertexFormatUint8x2)
	VertexFormatUint8x4      = VertexFormat(gfx.VertexFormatUint8x4)
	VertexFormatSint8x2      = VertexFormat(gfx.VertexFormatSint8x2)
	VertexFormatSint8x4      = VertexFormat(gfx.VertexFormatSint8x4)
	VertexFormatUnorm8x2     = VertexFormat(gfx.VertexFormatUnorm8x2)
	VertexFormatUnorm8x4     = VertexFormat(gfx.VertexFormatUnorm8x4)
	VertexFormatSnorm8x2     = VertexFormat(gfx.VertexFormatSnorm8x2)
	VertexFormatSnorm8x4     = VertexFormat(gfx.VertexFormatSnorm8x4)
	VertexFormatUint16x2     = VertexFormat(gfx.VertexFormatUint16x2)
	VertexFormatUint16x4     = VertexFormat(gfx.VertexFormatUint16x4)
	VertexFormatSint16x2     = VertexFormat(gfx.VertexFormatSint16x2)
	VertexFormatSint16x4     = VertexFormat(gfx.VertexFormatSint16x4)
	VertexFormatUnorm16x2    = VertexFormat(gfx.VertexFormatUnorm16x2)
	VertexFormatUnorm16x4    = VertexFormat(gfx.VertexFormatUnorm16x4)
	VertexFormatSnorm16x2    = VertexFormat(gfx.VertexFormatSnorm16x2)
	VertexFormatSnorm16x4    = VertexFormat(gfx.VertexFormatSnorm16x4)
	VertexFormatFloat16x2    = VertexFormat(gfx.VertexFormatFloat16x2)
	VertexFormatFloat16x4    = VertexFormat(gfx.VertexFormatFloat16x4)
	VertexFormatFloat32      = VertexFormat(gfx.VertexFormatFloat32)
	VertexFormatFloat32x2    = VertexFormat(gfx.VertexFormatFloat32x2)
	VertexFormatFloat32x3    = VertexFormat(gfx.VertexFormatFloat32x3)
	VertexFormatFloat32x4    = VertexFormat(gfx.VertexFormatFloat32x4)
	VertexFormatUint32       = VertexFormat(gfx.VertexFormatUint32)
	VertexFormatUint32x2     = VertexFormat(gfx.VertexFormatUint32x2)
	VertexFormatUint32x3     = VertexFormat(gfx.VertexFormatUint32x3)
	VertexFormatUint32x4     = VertexFormat(gfx.VertexFormatUint32x4)
	VertexFormatSint32       = VertexFormat(gfx.VertexFormatSint32)
	VertexFormatSint32x2     = VertexFormat(gfx.VertexFormatSint32x2)
	VertexFormatSint32x3     = VertexFormat(gfx.VertexFormatSint32x3)
	VertexFormatSint32x4     = VertexFormat(gfx.VertexFormatSint32x4)
	VertexFormatUnorm1010102 = VertexFormat(gfx.VertexFormatUnorm1010102)
)

// alignment answers the multiple a vertex attribute of format f sits at in a
// vertex, as WebGPU requires: its size, 2 bytes, for a format of two 8-bit
// components, and 4 for every other.
func (f VertexFormat) alignment() uint64 {
	switch f {
	case VertexFormatUint8x2, VertexFormatSint8x2, VertexFormatUnorm8x2, VertexFormatSnorm8x2:
		return 2
	}
	return 4
}
