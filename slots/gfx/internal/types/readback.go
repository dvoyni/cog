package types

import "image"

// Capture is one completed readback: either the mapped bytes or the reason
// there are none. Pixels carries the GPU's own row padding, which BytesPerRow
// describes and Image removes; a backend never sees an image.Image.
//
// One struct carries success and failure so that a caller cannot handle one and
// forget the other, which is how a capture that never arrives becomes a hang
// somewhere far away.
type Capture struct {
	Pixels        []byte
	Width, Height int
	Format        TextureFormat
	BytesPerRow   int
	Err           error
}

// Image un-strides the captured bytes into an image. It reports nil for a
// failed capture and for any format that is not 8-bit RGBA, which is every
// format a backend is allowed to hand back.
//
// image.NRGBA rather than image.RGBA: FormatRGBA8 is straight-alpha, and
// image.RGBA is premultiplied, so the obvious type would misread every
// translucent pixel. No colour conversion happens here in either case - an
// sRGB frame buffer's bytes are already what a PNG wants.
func (c Capture) Image() image.Image {
	if c.Err != nil || !captureFormatSupported(c.Format) {
		return nil
	}
	if c.Width <= 0 || c.Height <= 0 {
		return nil
	}
	row := c.Width * 4
	if c.BytesPerRow < row || len(c.Pixels) < (c.Height-1)*c.BytesPerRow+row {
		return nil
	}
	picture := image.NewNRGBA(image.Rect(0, 0, c.Width, c.Height))
	for y := range c.Height {
		src := y * c.BytesPerRow
		dst := y * picture.Stride
		copy(picture.Pix[dst:dst+row], c.Pixels[src:src+row])
	}
	return picture
}

// captureFormatSupported reports whether a format is the 8-bit RGBA a capture
// can be an image of. Depth is not: it is a float field needing a range to be
// legible, which is a visualization question rather than a readback one.
func captureFormatSupported(format TextureFormat) bool {
	switch format.Resolve() {
	case FormatRGBA8, FormatRGBA8Srgb:
		return true
	default:
		return false
	}
}
