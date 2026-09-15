// Package queryindex is PROTOTYPE code for cog#345: what a Sweep and a
// SweepAll cost per call on a static and a body index, and which internal
// structure holds that cost down. Throwaway; it never merges. See README.md.
package queryindex

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

type shapeKind uint8

const (
	kindCircle shapeKind = iota
	kindBox
	kindSegment
)

// Shape is the vocabulary of cog#287: a circle (a point is radius 0), an
// axis-aligned box, or a segment centred on its position.
type Shape struct {
	kind   shapeKind
	radius float32
	half   m.Vec2
}

func Circle(r float32) Shape    { return Shape{kind: kindCircle, radius: r} }
func Box(half m.Vec2) Shape     { return Shape{kind: kindBox, half: half} }
func Segment(half m.Vec2) Shape { return Shape{kind: kindSegment, half: half} }
func (s Shape) extent() m.Vec2 {
	if s.kind == kindCircle {
		return m.Vec2{X: s.radius, Y: s.radius}
	}
	return m.Vec2{X: abs32(s.half.X), Y: abs32(s.half.Y)}
}

// Hit is cog#306's hit: a fraction T of from→to, a point on the hit shape's
// surface, and a unit normal facing the sweeper.
type Hit struct {
	Entity ecs.Entity
	T      float32
	Point  m.Vec2
	Normal m.Vec2
}

// ray is a sweep prepared once per query, so candidates share its sqrt.
type ray struct {
	p0, d  m.Vec2
	r      float32
	length float32 // |d|
	u      m.Vec2  // d / |d|, or zero
	inv    m.Vec2  // 1/d per axis, ±Inf where zero
}

func makeRay(from, to m.Vec2, r float32) ray {
	d := to.Sub(from)
	l := sqrt32(d.X*d.X + d.Y*d.Y)
	var u m.Vec2
	if l > 0 {
		u = m.Vec2{X: d.X / l, Y: d.Y / l}
	}
	return ray{p0: from, d: d, r: r, length: l, u: u, inv: m.Vec2{X: 1 / d.X, Y: 1 / d.Y}}
}

// SweepShape is the pair primitive: a circle of radius swept from→to against
// one shape standing at at.
func SweepShape(from, to m.Vec2, radius float32, shape Shape, at m.Vec2) (Hit, bool) {
	ry := makeRay(from, to, radius)
	return sweep(&ry, shape, at)
}

func sweep(ry *ray, s Shape, at m.Vec2) (Hit, bool) {
	countTest()
	switch s.kind {
	case kindCircle:
		return sweepCircle(ry, at, s.radius)
	case kindBox:
		return sweepBox(ry, at, s.half)
	default:
		return sweepSegment(ry, at, s.half)
	}
}

// fallbackNormal is the reverse of the motion, or +X when there is none.
func (ry *ray) fallbackNormal() m.Vec2 {
	if ry.length > 0 {
		return m.Vec2{X: -ry.u.X, Y: -ry.u.Y}
	}
	return m.Vec2{X: 1}
}

// sweepCircle: a circle of ry.r against a circle of rs at c. Growth is exact:
// a ray against a circle of radius r+rs.
func sweepCircle(ry *ray, c m.Vec2, rs float32) (Hit, bool) {
	R := ry.r + rs
	if R == 0 {
		return Hit{}, false // a point never hits a point
	}
	mv := ry.p0.Sub(c)
	mm := mv.X*mv.X + mv.Y*mv.Y
	if mm < R*R { // starts overlapping
		n := ry.fallbackNormal()
		if mm > 0 {
			l := sqrt32(mm)
			n = m.Vec2{X: mv.X / l, Y: mv.Y / l}
		}
		return Hit{T: 0, Normal: n, Point: m.Vec2{X: c.X + n.X*rs, Y: c.Y + n.Y*rs}}, true
	}
	if ry.length == 0 {
		return Hit{}, false
	}
	b := mv.X*ry.u.X + mv.Y*ry.u.Y
	if b >= 0 {
		return Hit{}, false // moving away
	}
	// perpendicular distance, well conditioned for long rays
	px, py := mv.X-ry.u.X*b, mv.Y-ry.u.Y*b
	h2 := R*R - (px*px + py*py)
	if h2 < 0 {
		return Hit{}, false
	}
	dist := -b - sqrt32(h2)
	if dist > ry.length {
		return Hit{}, false
	}
	if dist < 0 {
		dist = 0
	}
	t := dist / ry.length
	q := m.Vec2{X: ry.p0.X + ry.d.X*t, Y: ry.p0.Y + ry.d.Y*t}
	n := m.Vec2{X: (q.X - c.X) / R, Y: (q.Y - c.Y) / R}
	return Hit{T: t, Normal: n, Point: m.Vec2{X: c.X + n.X*rs, Y: c.Y + n.Y*rs}}, true
}

// sweepBox: a circle against an axis-aligned box, which is a ray against the
// box rounded by the circle's radius.
func sweepBox(ry *ray, c, h m.Vec2) (Hit, bool) {
	r := ry.r
	mv := ry.p0.Sub(c)
	dx, dy := abs32(mv.X)-h.X, abs32(mv.Y)-h.Y
	if dx < 0 && dy < 0 { // centre inside the box: least penetration
		var n m.Vec2
		var p m.Vec2
		if dx > dy {
			n.X = signOr(mv.X, -ry.d.X)
			p = m.Vec2{X: c.X + n.X*h.X, Y: ry.p0.Y}
		} else {
			n.Y = signOr(mv.Y, -ry.d.Y)
			p = m.Vec2{X: ry.p0.X, Y: c.Y + n.Y*h.Y}
		}
		return Hit{T: 0, Normal: n, Point: p}, true
	}
	if r > 0 {
		qx, qy := max32(dx, 0), max32(dy, 0)
		if qq := qx*qx + qy*qy; qq < r*r { // inside the rounded rim
			l := sqrt32(qq)
			n := m.Vec2{X: qx / l * sign(mv.X), Y: qy / l * sign(mv.Y)}
			p := m.Vec2{X: c.X + clamp32(mv.X, -h.X, h.X), Y: c.Y + clamp32(mv.Y, -h.Y, h.Y)}
			return Hit{T: 0, Normal: n, Point: p}, true
		}
	}
	if ry.length == 0 {
		return Hit{}, false
	}
	// slab test against the box grown by r with square corners
	H := m.Vec2{X: h.X + r, Y: h.Y + r}
	tx1, tx2 := (-H.X-mv.X)*ry.inv.X, (H.X-mv.X)*ry.inv.X
	ty1, ty2 := (-H.Y-mv.Y)*ry.inv.Y, (H.Y-mv.Y)*ry.inv.Y
	if ry.d.X == 0 {
		if abs32(mv.X) > H.X {
			return Hit{}, false
		}
		tx1, tx2 = float32(math.Inf(-1)), float32(math.Inf(1))
	}
	if ry.d.Y == 0 {
		if abs32(mv.Y) > H.Y {
			return Hit{}, false
		}
		ty1, ty2 = float32(math.Inf(-1)), float32(math.Inf(1))
	}
	if tx1 > tx2 {
		tx1, tx2 = tx2, tx1
	}
	if ty1 > ty2 {
		ty1, ty2 = ty2, ty1
	}
	enter, exit := max32(tx1, ty1), min32(tx2, ty2)
	if enter > exit || exit < 0 || enter > 1 {
		return Hit{}, false
	}
	te := max32(enter, 0)
	q := m.Vec2{X: mv.X + ry.d.X*te, Y: mv.Y + ry.d.Y*te} // relative to c
	cornerX, cornerY := abs32(q.X) > h.X, abs32(q.Y) > h.Y
	if r == 0 || !(cornerX && cornerY) {
		if enter < 0 { // cannot start inside the grown box outside a corner here
			return Hit{}, false
		}
		var n, p m.Vec2
		if tx1 > ty1 {
			n.X = -sign(ry.d.X)
			p = m.Vec2{X: c.X + n.X*h.X, Y: c.Y + clamp32(q.Y, -h.Y, h.Y)}
		} else {
			n.Y = -sign(ry.d.Y)
			p = m.Vec2{X: c.X + clamp32(q.X, -h.X, h.X), Y: c.Y + n.Y*h.Y}
		}
		return Hit{T: enter, Normal: n, Point: p}, true
	}
	// entered through a corner square: only the corner arcs can be hit
	best := Hit{T: 2}
	found := false
	for _, k := range [4]m.Vec2{{X: -h.X, Y: -h.Y}, {X: h.X, Y: -h.Y}, {X: -h.X, Y: h.Y}, {X: h.X, Y: h.Y}} {
		if hit, ok := sweepCircle(ry, c.Add(k), 0); ok && hit.T < best.T {
			best, found = hit, true
		}
	}
	return best, found
}

// sweepSegment: a circle against a segment from c-h to c+h, which is a ray
// against a capsule. A point against a segment is a crossing test; parallel
// motion never hits, because neither has area.
func sweepSegment(ry *ray, c, h m.Vec2) (Hit, bool) {
	r := ry.r
	a := c.Sub(h)
	e := m.Vec2{X: 2 * h.X, Y: 2 * h.Y}
	el := sqrt32(e.X*e.X + e.Y*e.Y)
	if el == 0 {
		return sweepCircle(ry, c, 0)
	}
	nPerp := m.Vec2{X: -e.Y / el, Y: e.X / el}
	if r > 0 {
		// starts overlapping?
		ap := ry.p0.Sub(a)
		s := clamp32((ap.X*e.X+ap.Y*e.Y)/(el*el), 0, 1)
		cp := m.Vec2{X: a.X + e.X*s, Y: a.Y + e.Y*s}
		dv := ry.p0.Sub(cp)
		if dd := dv.X*dv.X + dv.Y*dv.Y; dd < r*r {
			var n m.Vec2
			if dd > 0 {
				l := sqrt32(dd)
				n = m.Vec2{X: dv.X / l, Y: dv.Y / l}
			} else {
				n = nPerp
				if n.X*ry.d.X+n.Y*ry.d.Y > 0 {
					n = m.Vec2{X: -n.X, Y: -n.Y}
				}
			}
			return Hit{T: 0, Normal: n, Point: cp}, true
		}
	}
	if ry.length == 0 {
		return Hit{}, false
	}
	facing := nPerp
	if facing.X*ry.d.X+facing.Y*ry.d.Y > 0 {
		facing = m.Vec2{X: -facing.X, Y: -facing.Y}
	}
	best := Hit{T: 2}
	found := false
	// the flat side facing the sweeper, offset by r
	off := m.Vec2{X: a.X + facing.X*r, Y: a.Y + facing.Y*r}
	denom := cross(ry.d, e)
	if denom != 0 {
		w := off.Sub(ry.p0)
		t := cross(w, e) / denom
		s := cross(w, ry.d) / denom
		if t >= 0 && t <= 1 && s >= 0 && s <= 1 {
			best = Hit{T: t, Normal: facing, Point: m.Vec2{X: a.X + e.X*s, Y: a.Y + e.Y*s}}
			found = true
		}
	}
	if r > 0 {
		for _, k := range [2]m.Vec2{a, c.Add(h)} {
			if hit, ok := sweepCircle(ry, k, 0); ok && hit.T < best.T {
				best, found = hit, true
			}
		}
	}
	return best, found
}

// overlapCircle is strict overlap of a circle probe against a shape, the same
// strictness as the sweep's start-overlapping test.
func overlapCircle(p m.Vec2, r float32, s Shape, at m.Vec2) bool {
	mv := p.Sub(at)
	switch s.kind {
	case kindCircle:
		R := r + s.radius
		return mv.X*mv.X+mv.Y*mv.Y < R*R
	case kindBox:
		dx, dy := abs32(mv.X)-s.half.X, abs32(mv.Y)-s.half.Y
		if dx < 0 && dy < 0 {
			return true
		}
		qx, qy := max32(dx, 0), max32(dy, 0)
		return qx*qx+qy*qy < r*r
	default:
		if r == 0 {
			return false
		}
		a := at.Sub(s.half)
		e := m.Vec2{X: 2 * s.half.X, Y: 2 * s.half.Y}
		ap := p.Sub(a)
		ee := e.X*e.X + e.Y*e.Y
		t := float32(0)
		if ee > 0 {
			t = clamp32((ap.X*e.X+ap.Y*e.Y)/ee, 0, 1)
		}
		dx, dy := ap.X-e.X*t, ap.Y-e.Y*t
		return dx*dx+dy*dy < r*r
	}
}

func cross(a, b m.Vec2) float32 { return a.X*b.Y - a.Y*b.X }
func sqrt32(v float32) float32  { return float32(math.Sqrt(float64(v))) }
func cos32(v float32) float32   { return float32(math.Cos(float64(v))) }
func sin32(v float32) float32   { return float32(math.Sin(float64(v))) }
func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
func clamp32(v, lo, hi float32) float32 { return max32(lo, min32(v, hi)) }
func sign(v float32) float32 {
	if v < 0 {
		return -1
	}
	return 1
}

// signOr is the sign of v, or of fallback when v is zero, or +1.
func signOr(v, fallback float32) float32 {
	if v != 0 {
		return sign(v)
	}
	return sign(fallback)
}

// IsCircle, IsBox and IsSegment, Radius and Half let the third-party
// baselines rebuild the same geometry in their own types.
func (s Shape) IsCircle() bool  { return s.kind == kindCircle }
func (s Shape) IsBox() bool     { return s.kind == kindBox }
func (s Shape) IsSegment() bool { return s.kind == kindSegment }
func (s Shape) Radius() float32 { return s.radius }
func (s Shape) Half() m.Vec2    { return s.half }
