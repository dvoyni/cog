package canvas_test

import (
	"image"
	"sync/atomic"
)

// trimmer is a converter cutting a frame to the bounds of its texels of any
// alpha, a SubImage, which keeps the frame's coordinates, counting each
// frame it converts in convertedCount.
type trimmer struct {
	convertedCount *atomic.Int32
}

func (c trimmer) Convert(frame image.Image) image.Image {
	c.convertedCount.Add(1)
	bounds := image.Rectangle{}
	for y := frame.Bounds().Min.Y; y < frame.Bounds().Max.Y; y++ {
		for x := frame.Bounds().Min.X; x < frame.Bounds().Max.X; x++ {
			if _, _, _, alpha := frame.At(x, y).RGBA(); alpha > 0 {
				bounds = bounds.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return frame.(interface {
		SubImage(image.Rectangle) image.Image
	}).SubImage(bounds)
}
