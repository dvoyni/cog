package canvas_test

import (
	"image"
	"sync/atomic"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/render"
)

// trimmer is a converter cutting a frame to the bounds of its texels of any
// alpha, which keep the frame's coordinates, counting each frame it
// converts in convertedCount.
type trimmer struct {
	convertedCount *atomic.Int32
}

func (c trimmer) Format(space canvas.ColorSpace) render.TextureFormat {
	return findRGBA8Format(space)
}

func (c trimmer) Convert(frame *image.NRGBA, space canvas.ColorSpace) ([]byte, image.Rectangle) {
	c.convertedCount.Add(1)
	bounds := image.Rectangle{}
	for y := frame.Rect.Min.Y; y < frame.Rect.Max.Y; y++ {
		for x := frame.Rect.Min.X; x < frame.Rect.Max.X; x++ {
			if frame.NRGBAAt(x, y).A > 0 {
				bounds = bounds.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return premultiplyTexels(frame, bounds, space), bounds
}

// findRGBA8Format answers space's RGBA8 format, a plain sprite atlas's.
func findRGBA8Format(space canvas.ColorSpace) render.TextureFormat {
	if space == canvas.ColorSpaceLinear {
		return render.TextureFormatRGBA8Unorm
	}
	return render.TextureFormatRGBA8UnormSrgb
}

// premultiplyTexels answers frame's texels within bounds, row by row,
// premultiplied in linear and encoded as space says, as a Converter of
// space's RGBA8 format answers them.
func premultiplyTexels(frame *image.NRGBA, bounds image.Rectangle, space canvas.ColorSpace) []byte {
	texels := make([]byte, 0, 4*bounds.Dx()*bounds.Dy())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			texel := frame.NRGBAAt(x, y)
			var red, green, blue, alpha uint8
			if space == canvas.ColorSpaceLinear {
				red = premultiplyChannel(texel.R, texel.A)
				green = premultiplyChannel(texel.G, texel.A)
				blue = premultiplyChannel(texel.B, texel.A)
				alpha = texel.A
			} else {
				red, green, blue, alpha = m.NewColorSrgb8(texel.R, texel.G, texel.B, texel.A).Premultiply().Srgb8()
			}
			texels = append(texels, red, green, blue, alpha)
		}
	}
	return texels
}

// premultiplyChannel answers a linear channel multiplied by alpha, rounded.
func premultiplyChannel(channel, alpha uint8) uint8 {
	return uint8((uint16(channel)*uint16(alpha) + 127) / 255)
}
