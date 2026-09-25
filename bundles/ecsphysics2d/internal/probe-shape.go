package internal

import "github.com/dvoyni/cog/libs/m"

// Probing a Shape: a convex Shape moved in a straight line at a fixed angle,
// and the first touch it makes with one other placed Shape. A circle keeps
// probeWorld, which is exact against every kind; everything else is Probed by
// conservative advancement over gjk.go's distance.
//
// The distance between two convex Shapes, one of them moving in a straight
// line without turning, is a convex function of how far along the path it is,
// and its slope there is minus the path's component along the separating
// normal. So stepping the mover forward by the separation over that component
// is Newton's method on the distance, started before the root, and it never
// steps past the first touch: it lands on it exactly for two flat faces and
// closes on it quadratically for a rounded one. A path whose component along
// the normal is not closing never comes nearer, and that is a miss.

const (
	// probeShapeTolerance is how near the surface a Probed Shape's advance
	// stops, in metres. Two faces meet in one step and land well inside it;
	// only a rounded feature approaches it, from before the touch, and the T
	// it reports is short of the exact one by at most this over the path's
	// closing speed.
	probeShapeTolerance = 1e-9

	// maxProbeShapeIterations caps the advance, as maxGJKIterations caps GJK.
	// Newton closes a simple root in a handful of steps, so only a graze that
	// is tangent to the surface reaches the cap, and it is reported where the
	// advance stood, which is before the touch and never through it.
	maxProbeShapeIterations = 30
)

// ProbeShapeWith moves one Shape at a fixed angle from one Position to another
// and reports the first Hit on a second Shape placed at a position and an
// angle. It is the pair primitive behind an index's ProbeWith, over values and
// touching no engine state, and its Hit names no Entity.
//
// from and to are the mover's Position at each end, and angle is held for the
// whole path: the mover does not turn. A circle mover answers exactly as
// ProbeShape does for a circle of its radius about its placed centre.
//
// moverVerts and targetVerts are the Polygon Components' vertices and are nil
// for every kind but Poly.
func ProbeShapeWith(
	mover Shape, from, to m.Vec2d, angle float64, moverVerts []m.Vec2d,
	target Shape, at m.Vec2d, targetAngle float64, targetVerts []m.Vec2d,
) (Hit, bool) {
	var scratch [worldScratchLen]m.Vec2d
	world := worldRunFor(scratch[:], target, targetVerts)
	used, box := cacheWorldAt(target, NewTransformRigid(at, targetAngle), targetVerts, world)
	if used == 0 {
		return Hit{}, false
	}

	if mover.Kind == ShapeCircle {
		from, to := circlePath(mover, from, to, angle)
		return probeWorld(from, to, mover.Radius, target, world[:used])
	}

	var runs moverRuns
	probe, ok := newShapeProbe(&runs, mover, from, to, angle, moverVerts)
	if !ok {
		return Hit{}, false
	}
	return probe.against(target, box, world[:used])
}

// moverRuns is the stack scratch a Probed Shape's two world caches are built
// in: where it stood at from, and where the advance has moved it to.
type moverRuns [2][worldScratchLen]m.Vec2d

// shapeProbe is one Probed Shape, cached once at from and moved along its
// path by translation alone, the angle being held.
type shapeProbe struct {
	shape Shape
	// delta is the whole path, to less from.
	delta m.Vec2d
	// base is the world cache at from, and moved is the same cache translated
	// to wherever the advance stands. GJK reads the vertices alone, so only
	// they are rewritten as it moves.
	base, moved []m.Vec2d
	// points is how many leading vectors of the cache are points, as opposed
	// to the normals a segment and a Polygon keep after them.
	points int
	// box is the mover's bounding box at from, and centre its centre, which
	// is GJK's cold-start guess translated along with the mover.
	box    BB
	centre m.Vec2d
}

// newShapeProbe caches the mover at from in runs, or in a fresh run where a
// Polygon is too large for them, which is the one allocation the query surface
// has.
func newShapeProbe(
	runs *moverRuns, shape Shape, from, to m.Vec2d, angle float64, verts []m.Vec2d,
) (shapeProbe, bool) {
	base := worldRunFor(runs[0][:], shape, verts)
	moved := worldRunFor(runs[1][:], shape, verts)
	used, box := cacheWorldAt(shape, NewTransformRigid(from, angle), verts, base)
	if used == 0 {
		return shapeProbe{}, false
	}
	copy(moved, base[:used])

	return shapeProbe{
		shape:  shape,
		delta:  to.Sub(from),
		base:   base[:used],
		moved:  moved[:used],
		points: cachedPoints(shape.Kind, used),
		box:    box,
		centre: box.Centre(),
	}, true
}

// shapeProbeBack is the Probed Shape of a placed one whose world cache and box
// are already built where its path ends: the cache moved back along delta to
// where the path starts, in runs, which must hold twice the cache. It is the
// path test of a fast solid Body, whose index entry has its cache at the end
// pose and whose Shape is held at its end angle, so the start is the end
// translated and nothing is placed twice.
func shapeProbeBack(runs []m.Vec2d, shape Shape, end []m.Vec2d, box BB, delta m.Vec2d) shapeProbe {
	used := len(end)
	base, moved := runs[:used], runs[used:2*used]
	points := cachedPoints(shape.Kind, used)
	for i := range points {
		base[i] = end[i].Sub(delta)
	}
	copy(base[points:], end[points:])
	copy(moved, base)
	start := box.Offset(delta.Negate())
	return shapeProbe{
		shape:  shape,
		delta:  delta,
		base:   base,
		moved:  moved,
		points: points,
		box:    start,
		centre: start.Centre(),
	}
}

// cachedPoints is how many leading vectors of a world cache of used vectors
// are points, as opposed to the normals a segment and a Polygon keep after
// them: every one of a circle's, a segment's two ends, and a Polygon's first
// half.
func cachedPoints(kind ShapeKind, used int) int {
	switch kind {
	case ShapeCircle:
		return used
	case ShapeSegment:
		return 2
	}
	return used / 2
}

// place moves the mover's cache to fraction t of its path.
func (probe *shapeProbe) place(t float64) m.Vec2d {
	offset := probe.delta.MulS(t)
	for i := range probe.points {
		probe.moved[i] = probe.base[i].Add(offset)
	}
	return offset
}

// against is the swept test itself: the mover advanced along its path until it
// touches the target, whose world cache and bounding box are already built.
//
// It reads as probeWorld does. A mover that starts overlapping the target
// reports T = 0 with the normal out of the overlap; the test is strict, so a
// mover that merely touches the target where it starts has not started inside
// it. Point is on the target's surface and Normal faces the mover.
func (probe *shapeProbe) against(target Shape, box BB, world []m.Vec2d) (Hit, bool) {
	if len(world) == 0 {
		return Hit{}, false
	}
	ctx := support{worldA: probe.moved, worldB: world, kindA: probe.shape.Kind, kindB: target.Kind}
	radii := probe.shape.Radius + target.Radius
	centre := box.Centre()

	t := 0.0
	var cached uint32
	for iteration := 0; ; iteration++ {
		offset := probe.place(t)
		// Two centres placed on each other have no cold-start axis, and the
		// seed is what GJK reads there. A Probe is reporting a Hit that needs a
		// normal, so it asks for one, which the pure Penetration does not.
		points := gjk(ctx, probe.centre.Add(offset), centre, m.Vec2d{Y: 1}, cached)
		cached = points.id
		separation := points.d - radii
		closing := probe.delta.Dot(points.n)

		switch {
		case iteration == 0 && separation < 0,
			separation <= probeShapeTolerance && (iteration > 0 || closing > 0),
			iteration == maxProbeShapeIterations:
			return shapeHit(t, points, target.Radius), true
		case !(closing > 0):
			return Hit{}, false
		}

		t += separation / closing
		if t > 1 {
			return Hit{}, false
		}
	}
}

// shapeHit is the Hit where the advance stopped: the target's surface point
// nearest the mover, grown by the target's rounding, and the normal turned to
// face the mover. GJK's normal points from the mover towards the target.
func shapeHit(t float64, points closestPoints, targetRadius float64) Hit {
	n := points.n
	if n == (m.Vec2d{}) {
		// cp's fallback direction, which pointQueryWorld also answers with.
		n = m.Vec2d{Y: -1}
	}
	return Hit{
		T:      t,
		Point:  points.b.Sub(n.MulS(targetRadius)),
		Normal: n.Negate(),
	}
}
