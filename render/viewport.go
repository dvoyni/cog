package render

import (
	"errors"
	"fmt"
)

// Viewport is the part of a pass's targets its draws map onto, in pixels
// from the targets' top left, and the range of depths they map onto, from
// MinDepth to MaxDepth, at most 0 to 1. It may reach past the targets.
type Viewport struct {
	X, Y, Width, Height, MinDepth, MaxDepth float32
}

// check answers why WebGPU refuses v on a device whose largest texture is
// maxDimension wide: a size not finite, negative or past maxDimension, a
// position not finite or reaching outside -2×maxDimension to
// 2×maxDimension-1, or a depth range outside 0 to 1 or reversed. A NaN or
// an infinity fails every range it is checked against.
func (v Viewport) check(maxDimension float32) error {
	lowest := -2 * maxDimension
	highest := 2*maxDimension - 1
	switch {
	case !(v.Width >= 0 && v.Width <= maxDimension && v.Height >= 0 && v.Height <= maxDimension):
		return fmt.Errorf("is of a size outside 0 to %v", maxDimension)
	case !(v.X >= lowest && v.Y >= lowest && v.X+v.Width <= highest && v.Y+v.Height <= highest):
		return fmt.Errorf("reaches outside %v to %v", lowest, highest)
	case !(v.MinDepth >= 0 && v.MinDepth <= v.MaxDepth && v.MaxDepth <= 1):
		return errors.New("has a depth range outside 0 to 1, or reversed")
	}
	return nil
}
