package canvas_test

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
)

// placedSprite is a sprite a pixel test draws and the transform, size and
// pivot it draws it with, which place the points of its frame.
type placedSprite struct {
	name      string
	image     canvas.Image
	frame     uint32
	transform m.Mat2x3
	size      m.Vec2
	pivot     canvas.Pivot
}

// findFramePoint answers where the centre of the texel x, y of s's frame,
// of frameSize texels, lies in the world, its size an axis of zero keeping
// the frame's aspect, the frame's for none.
func (s placedSprite) findFramePoint(frameSize m.Vec2, x, y int) m.Vec2 {
	size := s.size
	switch {
	case size == (m.Vec2{}):
		size = frameSize
	case size.Y == 0:
		size.Y = size.X * frameSize.Y / frameSize.X
	case size.X == 0:
		size.X = size.Y * frameSize.X / frameSize.Y
	}
	scale := size.Div(frameSize)
	pivotPoint := s.pivot.Fraction.AddS(0.5).Mul(size).Add(s.pivot.Texels.Mul(scale))
	own := m.Vec2{X: float32(x) + 0.5, Y: float32(y) + 0.5}.Mul(scale).Sub(pivotPoint)
	return s.transform.TransformPoint(own)
}

// draw records s into list, its frame's texels read at their nearest.
func (s placedSprite) draw(list canvas.CommandList) {
	list.DrawSprite(canvas.Sprite{Image: s.image, Frame: s.frame, Transform: s.transform, Size: s.size, Filter: canvas.FilterNearest})
}
