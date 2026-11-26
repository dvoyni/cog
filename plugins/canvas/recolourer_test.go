package canvas_test

import (
	"image"
	"image/color"

	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/render"
)

// recolourer is a converter painting each texel of a frame its colour, keeping
// its alpha; holding a slice, it is not comparable.
type recolourer struct {
	colours []color.NRGBA
}

func (c recolourer) Format(space canvas.ColorSpace) render.TextureFormat {
	return findRGBA8Format(space)
}

func (c recolourer) Convert(frame *image.NRGBA, space canvas.ColorSpace) ([]byte, image.Rectangle) {
	recoloured := image.NewNRGBA(frame.Rect)
	for y := frame.Rect.Min.Y; y < frame.Rect.Max.Y; y++ {
		for x := frame.Rect.Min.X; x < frame.Rect.Max.X; x++ {
			colour := c.colours[0]
			colour.A = frame.NRGBAAt(x, y).A
			recoloured.SetNRGBA(x, y, colour)
		}
	}
	return premultiplyTexels(recoloured, recoloured.Rect, space), recoloured.Rect
}
