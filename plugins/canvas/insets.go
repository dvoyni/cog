package canvas

import "github.com/dvoyni/cog/libs/m"

// Insets are distances in from each edge of a rectangle.
type Insets struct {
	Left, Top, Right, Bottom float32
}

// findSliceEdges answers the centre cell a sliced sprite cuts its frame
// into by i, the frame frameSize texels and the sprite's square squareSize
// in its own space, its lengths, its corners texelScale of it per texel:
// within the frame and within the unit square, as fractions. Insets meeting
// past the frame, or corners past the square, shrink, keeping their ratio,
// until they meet; a square of no length along an axis, which draws
// nothing, has no corners along it.
func (i Insets) findSliceEdges(frameSize, squareSize m.Vec2, texelScale float32) (frameEdges, squareEdges m.Box2) {
	frameEdges.Min.X, frameEdges.Max.X, squareEdges.Min.X, squareEdges.Max.X = findSliceAxisEdges(i.Left, i.Right, frameSize.X, squareSize.X, texelScale)
	frameEdges.Min.Y, frameEdges.Max.Y, squareEdges.Min.Y, squareEdges.Max.Y = findSliceAxisEdges(i.Top, i.Bottom, frameSize.Y, squareSize.Y, texelScale)
	return frameEdges, squareEdges
}

// findSliceAxisEdges answers, along one axis, the edges of the centre cell
// leading and trailing, insets of a frame frameLength texels long, cut it
// into, within the frame and within a square squareLength long whose
// corners are texelScale of it per texel, as fractions, as findSliceEdges
// answers them.
func findSliceAxisEdges(leading, trailing, frameLength, squareLength, texelScale float32) (frameLeading, frameTrailing, squareLeading, squareTrailing float32) {
	if squareLength <= 0 {
		return 0, 1, 0, 1
	}
	frameLeadingInset, frameTrailingInset := fitInsets(max(leading, 0), max(trailing, 0), frameLength)
	squareLeadingInset, squareTrailingInset := fitInsets(frameLeadingInset*texelScale, frameTrailingInset*texelScale, squareLength)
	return frameLeadingInset / frameLength, 1 - frameTrailingInset/frameLength, squareLeadingInset / squareLength, 1 - squareTrailingInset/squareLength
}

// fitInsets answers leading and trailing, insets of a length, shrunk,
// keeping their ratio, until they meet where they would overlap.
func fitInsets(leading, trailing, length float32) (float32, float32) {
	if total := leading + trailing; total > length {
		return leading * length / total, trailing * length / total
	}
	return leading, trailing
}
