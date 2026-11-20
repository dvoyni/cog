package canvas

import "github.com/dvoyni/cog/libs/m"

// Insets are distances in from each edge of a rectangle.
type Insets struct {
	Left, Top, Right, Bottom float32
}

// findSliceEdges answers the edges of the centre cell a sliced sprite cuts
// its frame into by i, the frame frameSize texels and the sprite's square
// squareSize in its own space, its lengths, its corners texelScale of it
// per texel:
// within the frame and within the unit square, each its left, top, right
// and bottom edges as fractions. Insets meeting past the frame, or corners
// past the square, shrink, keeping their ratio, until they meet; a square
// of no length along an axis, which draws nothing, has no corners along it.
func (i Insets) findSliceEdges(frameSize, squareSize m.Vec2, texelScale float32) (frameEdges, squareEdges [4]float32) {
	leading := [2]float32{max(i.Left, 0), max(i.Top, 0)}
	trailing := [2]float32{max(i.Right, 0), max(i.Bottom, 0)}
	frameLengths := [2]float32{frameSize.X, frameSize.Y}
	squareLengths := [2]float32{squareSize.X, squareSize.Y}
	for axis := range 2 {
		if squareLengths[axis] <= 0 {
			frameEdges[axis+2] = 1
			squareEdges[axis+2] = 1
			continue
		}
		frameLeading, frameTrailing := fitInsets(leading[axis], trailing[axis], frameLengths[axis])
		frameEdges[axis] = frameLeading / frameLengths[axis]
		frameEdges[axis+2] = 1 - frameTrailing/frameLengths[axis]
		squareLeading, squareTrailing := fitInsets(frameLeading*texelScale, frameTrailing*texelScale, squareLengths[axis])
		squareEdges[axis] = squareLeading / squareLengths[axis]
		squareEdges[axis+2] = 1 - squareTrailing/squareLengths[axis]
	}
	return frameEdges, squareEdges
}

// fitInsets answers leading and trailing, insets of a length, shrunk,
// keeping their ratio, until they meet where they would overlap.
func fitInsets(leading, trailing, length float32) (float32, float32) {
	if total := leading + trailing; total > length {
		return leading * length / total, trailing * length / total
	}
	return leading, trailing
}
