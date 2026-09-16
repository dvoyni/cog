package types

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// infinity is what a miss is, as cp's INFINITY is.
const infinity = math.MaxFloat64

// BB is an axis-aligned bounding box: left, bottom, right, top.
//
// Ported from cp's BB. The fields are exported where cp's are, because a value
// type cog exposes is data-driven and these four carry no invariant between
// them; cp's own BB exports them too.
type BB struct{ L, B, R, T float64 }

// NewBB is the box with those four edges.
func NewBB(l, b, r, t float64) BB { return BB{L: l, B: b, R: r, T: t} }

// NewBBForExtents is the box centred on a point with those half sizes.
func NewBBForExtents(centre m.Vec2d, halfWidth, halfHeight float64) BB {
	return BB{
		L: centre.X - halfWidth,
		B: centre.Y - halfHeight,
		R: centre.X + halfWidth,
		T: centre.Y + halfHeight,
	}
}

// NewBBForCircle is the box around a circle at that position.
func NewBBForCircle(position m.Vec2d, radius float64) BB {
	return NewBBForExtents(position, radius, radius)
}

// Intersects reports whether the two boxes touch or overlap.
func (bb BB) Intersects(other BB) bool {
	return bb.L <= other.R && other.L <= bb.R && bb.B <= other.T && other.B <= bb.T
}

// Contains reports whether other lies wholly inside the box.
func (bb BB) Contains(other BB) bool {
	return bb.L <= other.L && bb.R >= other.R && bb.B <= other.B && bb.T >= other.T
}

// ContainsVec reports whether the box holds the point.
func (bb BB) ContainsVec(v m.Vec2d) bool {
	return bb.L <= v.X && bb.R >= v.X && bb.B <= v.Y && bb.T >= v.Y
}

// Merge is the smallest box holding both.
func (bb BB) Merge(other BB) BB {
	return BB{
		math.Min(bb.L, other.L),
		math.Min(bb.B, other.B),
		math.Max(bb.R, other.R),
		math.Max(bb.T, other.T),
	}
}

// Expand is the smallest box holding the box and the point.
func (bb BB) Expand(v m.Vec2d) BB {
	return BB{
		math.Min(bb.L, v.X),
		math.Min(bb.B, v.Y),
		math.Max(bb.R, v.X),
		math.Max(bb.T, v.Y),
	}
}

// Centre is the middle of the box.
func (bb BB) Centre() m.Vec2d {
	return m.Vec2d{X: bb.L, Y: bb.B}.Lerp(m.Vec2d{X: bb.R, Y: bb.T}, 0.5)
}

// Area is the box's area, negative if an edge pair is the wrong way round.
func (bb BB) Area() float64 { return (bb.R - bb.L) * (bb.T - bb.B) }

// Offset moves the box by a vector.
func (bb BB) Offset(v m.Vec2d) BB {
	return BB{bb.L + v.X, bb.B + v.Y, bb.R + v.X, bb.T + v.Y}
}

// SegmentQuery is how far along the segment from a to b the box is first
// entered, as a fraction in [0, 1], and infinity when the segment misses.
func (bb BB) SegmentQuery(a, b m.Vec2d) float64 {
	delta := b.Sub(a)
	minimum := -infinity
	maximum := infinity

	if delta.X == 0 {
		if a.X < bb.L || bb.R < a.X {
			return infinity
		}
	} else {
		t1 := (bb.L - a.X) / delta.X
		t2 := (bb.R - a.X) / delta.X
		minimum = math.Max(minimum, math.Min(t1, t2))
		maximum = math.Min(maximum, math.Max(t1, t2))
	}

	if delta.Y == 0 {
		if a.Y < bb.B || bb.T < a.Y {
			return infinity
		}
	} else {
		t1 := (bb.B - a.Y) / delta.Y
		t2 := (bb.T - a.Y) / delta.Y
		minimum = math.Max(minimum, math.Min(t1, t2))
		maximum = math.Min(maximum, math.Max(t1, t2))
	}

	if minimum <= maximum && 0 <= maximum && minimum <= 1.0 {
		return math.Max(minimum, 0.0)
	}
	return infinity
}

// IntersectsSegment reports whether the segment from a to b meets the box.
func (bb BB) IntersectsSegment(a, b m.Vec2d) bool { return bb.SegmentQuery(a, b) != infinity }
