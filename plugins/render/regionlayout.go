package render

import "github.com/dvoyni/cog/plugins/gfx"

// regionLayout is how a region of a texture lies in a buffer's bytes for a
// copy between them: gfx's texture and the region's origin in it, its size
// in texels, each of texelSize bytes, and its rows, each rowSize bytes long
// and starting bytesPerRow bytes, a multiple of 256, after the one before,
// so they take bytesPerRow times height less one, plus rowSize, bytes.
type regionLayout struct {
	origin                          gfx.ImageCopyTexture
	width, height                   uint32
	texelSize, rowSize, bytesPerRow uint64
}

// makeCopy answers gfx's copy of the region from offset in a buffer.
func (l regionLayout) makeCopy(offset uint64) gfx.BufferTextureCopy {
	return gfx.BufferTextureCopy{
		BufferLayout: gfx.ImageDataLayout{Offset: offset, BytesPerRow: uint32(l.bytesPerRow), RowsPerImage: l.height},
		TextureBase:  l.origin,
		Size:         gfx.Extent3D{Width: l.width, Height: l.height, DepthOrArrayLayers: 1},
	}
}
