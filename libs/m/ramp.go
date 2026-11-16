package m

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Ramp is a piecewise-linear curve through its points, read with Evaluate. The
// points are expected in order of X, left to right; nothing sorts them, and a
// lookup takes the first pair that fits, walking from the left.
//
// Two points sharing an X make a step: the curve jumps there. On either side of
// the step it follows the segment on that side, and at the step itself it takes
// the midpoint of the two sides. Before the first point the curve holds the
// first point's Y, and after the last point the last point's Y.
//
// It crosses JSON as an array of [x, y] pairs, such as
// [[0.2,1],[0.5,0.2],[0.5,0.6],[0.8,1]] - which is 1 up to 0.2, falls to 0.2
// at 0.5, steps up to 0.6, rises to 1 at 0.8 and stays there, and reads 0.4 at
// 0.5 itself.
type Ramp []Vec2

// Evaluate is the curve's value at x: the midpoint of the curve as it arrives at
// x from the left and as it leaves x to the right. Everywhere but a step the two
// are the same, and the midpoint is simply the value there. An empty Ramp is 0
// everywhere, and a single point is a constant.
func (r Ramp) Evaluate(x float32) float32 {
	if len(r) == 0 {
		return 0
	}
	return (r.fromLeft(x) + r.fromRight(x)) / 2
}

// fromLeft is the value arriving at x from the left: the first segment that
// ends at or past x and starts before it. With none, x is before the curve
// starts or after it ends, and the nearer end holds.
func (r Ramp) fromLeft(x float32) float32 {
	for index := 1; index < len(r); index++ {
		from, to := r[index-1], r[index]
		if from.X < x && x <= to.X {
			return rampSegment(from, to, x)
		}
	}
	if x <= r[0].X {
		return r[0].Y
	}
	return r[len(r)-1].Y
}

// fromRight is the value leaving x to the right: the first segment that starts
// at or before x and ends past it. With none, the nearer end holds.
func (r Ramp) fromRight(x float32) float32 {
	for index := 1; index < len(r); index++ {
		from, to := r[index-1], r[index]
		if from.X <= x && x < to.X {
			return rampSegment(from, to, x)
		}
	}
	if x < r[0].X {
		return r[0].Y
	}
	return r[len(r)-1].Y
}

// rampSegment is the segment from from to to at x. Its ends are returned as
// they are rather than lerped to, so a point on the curve reads back exactly.
func rampSegment(from, to Vec2, x float32) float32 {
	switch {
	case x <= from.X:
		return from.Y
	case x >= to.X:
		return to.Y
	}
	return Lerp(from.Y, to.Y, (x-from.X)/(to.X-from.X))
}

// MarshalJSON writes the points as an array of [x, y] pairs, [] when there are
// none.
func (r Ramp) MarshalJSON() ([]byte, error) {
	pairs := make([][2]float32, len(r))
	for index, point := range r {
		pairs[index] = [2]float32{point.X, point.Y}
	}
	return json.Marshal(pairs)
}

// UnmarshalJSON reads an array of [x, y] pairs, and null as no points. A pair
// with more or fewer than two numbers is an error rather than a point padded or
// cut down to fit.
func (r *Ramp) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*r = nil
		return nil
	}
	var pairs [][]float32
	if err := json.Unmarshal(data, &pairs); err != nil {
		return err
	}
	ramp := make(Ramp, len(pairs))
	for index, pair := range pairs {
		if len(pair) != 2 {
			return fmt.Errorf("ramp point %d has %d numbers, want [x, y]", index, len(pair))
		}
		ramp[index] = Vec2{X: pair[0], Y: pair[1]}
	}
	*r = ramp
	return nil
}
