package canvas

// Crop is the fraction of an entry's rectangle left out from each side,
// zero drawing all of it.
type Crop struct {
	Left, Top, Right, Bottom float32
}

// findRect answers the part of the unit square the crop leaves: its left,
// top, right and bottom edges, a side cropped past the one facing it
// meeting it, so it leaves nothing.
func (c Crop) findRect() [4]float32 {
	left := c.Left
	top := c.Top
	return [4]float32{left, top, max(1-c.Right, left), max(1-c.Bottom, top)}
}
