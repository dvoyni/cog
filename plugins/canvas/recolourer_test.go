package canvas_test

import (
	"image"
	"image/color"
)

// recolourer is a converter painting each texel of a frame its colour, keeping
// its alpha; holding a slice, it is not comparable.
type recolourer struct {
	colours []color.NRGBA
}

func (c recolourer) Convert(frame image.Image) image.Image {
	recoloured := image.NewNRGBA(frame.Bounds())
	for y := frame.Bounds().Min.Y; y < frame.Bounds().Max.Y; y++ {
		for x := frame.Bounds().Min.X; x < frame.Bounds().Max.X; x++ {
			_, _, _, alpha := frame.At(x, y).RGBA()
			colour := c.colours[0]
			colour.A = uint8(alpha >> 8)
			recoloured.SetNRGBA(x, y, colour)
		}
	}
	return recoloured
}
