package canvas

import "github.com/dvoyni/cog/libs/m"

// Crop is the fraction of an entry's rectangle left out from each side,
// zero drawing all of it.
type Crop struct {
	Left, Top, Right, Bottom float32
}

// findBox answers the part of the unit square the crop leaves, a side
// cropped past the one facing it meeting it, so it leaves nothing.
func (c Crop) findBox() m.Box2 {
	left := c.Left
	top := c.Top
	return m.Box2{Min: m.Vec2{X: left, Y: top}, Max: m.Vec2{X: max(1-c.Right, left), Y: max(1-c.Bottom, top)}}
}
