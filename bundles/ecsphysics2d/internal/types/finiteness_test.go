package types

import (
	"fmt"
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// The finiteness invariant, fuzzed over the pair and query surface:
//
//	No input produces a NaN or an infinity in a Component the plugin writes.
//
// The spec states it as a constraint and not only as a test, so the sweep below
// is over inputs an app can actually build — every Shape here comes out of an
// exported constructor — rather than over arbitrary bit patterns.
//
// Two things make this more than a NaN hunt.
//
// First, it generates the cases cp's own sweep never reaches. The corpus in
// cpcases_test.go moves Shapes over a continuum of poses, and a continuum never
// lands exactly on anything: the Polygon point query's divide at poly.go:101 and
// the Polygon segment query's an − bn at poly.go:129 both need a query exactly on
// an edge or exactly parallel to a face plane, and neither was reached by a
// single corpus row. Here every placement is drawn from a lattice of exact
// binary fractions at an angle of zero, so an axis-aligned face lies on an exact
// coordinate and a probe along that coordinate is exactly parallel to it. The
// guards C has and jakecoffman/cp drops are what those rows run through.
//
// Second, a NaN is not the only bad answer. A zero normal with a zero depth is
// finite, and it is the port reporting a pair as touching with nothing to
// separate it — the answer is useless rather than wrong-looking, and a fuzz that
// only asks math.IsNaN sails past it. So every such answer is classified here
// and has to have a cause the sweep can name; one with no cause fails the test.
//
// The sweep is deterministic and bounded: splitmix64 from the seed below, a
// fixed number of draws, no wall clock and no global rand. A flaky test is worse
// than no test.
const finitenessSeed = 0x9E3779B97F4A7C15

// finitenessDraws is how many cases each of the three sweeps takes. Bounded, and
// the whole test is well under a second.
const finitenessDraws = 6000

// splitmix64 is the generator, written out rather than taken from math/rand so
// that the sequence is this file's own and cannot move under a standard library
// change. The repo imports math/rand nowhere else.
type splitmix64 uint64

func (r *splitmix64) next() uint64 {
	*r += 0x9E3779B97F4A7C15
	z := uint64(*r)
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func (r *splitmix64) intn(n int) int { return int(r.next() % uint64(n)) }

// lattice is the placement grid: quarter metres, every one of them exact in
// float64, so two Shapes placed on it share face planes and corner coordinates
// exactly rather than to within a rounding. That is the whole point of a lattice
// here — the corpus's continuum of poses is what leaves the exact cases unvisited.
func (r *splitmix64) lattice() m.Vec2d {
	return m.Vec2d{
		X: float64(r.intn(17)-8) * 0.25,
		Y: float64(r.intn(17)-8) * 0.25,
	}
}

// finitenessShape is one Shape of the catalogue with whatever Polygon Component
// vertices its kind reads, and a name a failure can quote.
type finitenessShape struct {
	name  string
	shape Shape
	verts []m.Vec2d
}

// finitenessCatalogue is every Shape the sweep draws from. The degenerate
// members are the point of it: a circle with no radius, a segment whose two
// endpoints are the same point — which NewSegmentShape builds without complaint
// — a quad with no area and a quad with no height, each of which collapses some
// denominator downstream.
func finitenessCatalogue(t testing.TB) []finitenessShape {
	t.Helper()

	corner := m.Vec2d{X: 0.5, Y: 0.5}
	triangle, triangleVerts, err := NewPolygonShape(
		[]m.Vec2d{{X: -0.5, Y: -0.5}, {X: 0.5, Y: -0.5}, {Y: 0.5}}, 0)
	if err != nil {
		t.Fatalf("hulling the triangle: %v", err)
	}
	hexagon, hexagonVerts, err := NewPolygonShape([]m.Vec2d{
		{X: 0.5}, {X: 0.25, Y: 0.433}, {X: -0.25, Y: 0.433},
		{X: -0.5}, {X: -0.25, Y: -0.433}, {X: 0.25, Y: -0.433},
	}, 0.125)
	if err != nil {
		t.Fatalf("hulling the hexagon: %v", err)
	}

	return []finitenessShape{
		{name: "a circle with no radius", shape: NewCircleShape(0, m.Vec2d{})},
		{name: "a circle", shape: NewCircleShape(0.5, m.Vec2d{})},
		{name: "a circle offset from its Body", shape: NewCircleShape(0.25, m.Vec2d{X: 0.5})},
		{name: "a zero-length segment", shape: NewSegmentShape(corner, corner, 0)},
		{name: "a zero-length capsule", shape: NewSegmentShape(corner, corner, 0.25)},
		{name: "a segment", shape: NewSegmentShape(m.Vec2d{X: -0.5}, m.Vec2d{X: 0.5}, 0)},
		{name: "a capsule", shape: NewSegmentShape(m.Vec2d{X: -0.5}, m.Vec2d{X: 0.5}, 0.25)},
		{name: "a chained segment", shape: NewSegmentShapeWithNeighbours(
			m.Vec2d{X: -1}, m.Vec2d{X: -0.5}, m.Vec2d{X: 0.5}, m.Vec2d{X: 1}, 0)},
		{name: "a quad with no area", shape: NewBoxShapeFor(NewBB(0, 0, 0, 0), 0)},
		{name: "a quad with no height", shape: NewBoxShapeFor(NewBB(-0.5, 0, 0.5, 0), 0)},
		{name: "a box", shape: NewBoxShape(1, 1, 0)},
		{name: "a rounded box", shape: NewBoxShape(1, 1, 0.25)},
		{name: "a triangle", shape: triangle, verts: PolygonVerts(nil, triangle, triangleVerts)},
		{name: "a rounded hexagon", shape: hexagon, verts: PolygonVerts(nil, hexagon, hexagonVerts)},
	}
}

// finitenessWorld is the world cache and the bounding box of one placed Shape,
// built exactly as an index entry's is. The box centre is what GJK's cold start
// takes its axis from, which is why the sweep keeps it.
func finitenessWorld(s finitenessShape, at m.Vec2d, angle float64) ([]m.Vec2d, BB) {
	world := make([]m.Vec2d, worldLenFor(s.shape, s.verts))
	used, box := cacheWorldAt(s.shape, NewTransformRigid(at, angle), s.verts, world)
	return world[:used], box
}

func TestTheFinitenessInvariantHoldsOverASweepOfDegenerateInputs(t *testing.T) {
	catalogue := finitenessCatalogue(t)
	rng := splitmix64(finitenessSeed)

	// The angles the sweep draws from. Zero dominates on purpose: a Shape turned
	// by anything else has no exactly axis-aligned face — cos(pi/2) is 6.1e-17
	// and not 0 — and the exact-edge and exact-parallel rows are the ones the cp
	// corpus never generated.
	angles := []float64{0, 0, 0, 0, 0, 0, math.Pi / 2, math.Pi, -math.Pi / 2, 0.7}

	var (
		pairs, probes, points int
		touching              int
		hits                  int
		// The two kinds of directionless answer, which are the whole of the
		// characterisation this sweep exists to make.
		//
		// seeded is one of the two closed forms — circle against circle, circle
		// against segment — reporting no direction because the caller gave it no
		// seed. Penetration is pure and hands in none; Detect hands in c.nudged.
		// The depth survives: it is the summed radius, and it is right.
		seeded int
		// unseeded is one of the four GJK arms, which take no seed at all. Their
		// cold start is axis := centreA.Sub(centreB).Perp() and a degenerate
		// simplex takes closestTo's d <= 0 arm with a normal Normalize guarded to
		// zero, so the depth is lost along with the direction.
		unseeded int
		// coincident and collinear split the unseeded ones by what degenerated
		// the simplex: exactly coincident world bounding box centres, which is a
		// zero cold-start axis, or a Minkowski difference with no area for the
		// axis to have found.
		coincident, collinear int
		// ungated counts the pairs whose bounding boxes do not intersect at all,
		// and ungatedTouching how many of those the narrowphase nonetheless
		// reports as touching. Neither is asserted on, because no engine path can
		// reach them: Detect and Overlap both test box intersection before the
		// narrowphase, and only the bare Penetration has no such gate. The count
		// is logged because it is the one measurement of that answer there is.
		ungated, ungatedTouching int
		// depthKept is the failure: a GJK arm that lost the direction but reported
		// a depth anyway, which would push two Bodies along a normal of nothing.
		depthKept []string
		// asymmetric counts the pairs the broadphase admits that read differently
		// either way round with a real depth behind the disagreement.
		asymmetric []string
		// exactlyOnAnEdge and exactlyParallel count the rows that reach the two
		// guards the cp corpus never reached.
		exactlyOnAnEdge, exactlyParallel int
	)

	finite := func(what string, values ...float64) {
		t.Helper()
		for _, v := range values {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatalf("%s is %v, and the finiteness invariant forbids it", what, v)
			}
		}
	}

	// The pair sweep: Penetration both ways round over every drawn placement.
	for range finitenessDraws {
		a := catalogue[rng.intn(len(catalogue))]
		b := catalogue[rng.intn(len(catalogue))]
		angleA := angles[rng.intn(len(angles))]
		angleB := angles[rng.intn(len(angles))]
		atA := rng.lattice()
		atB := atA
		// A third of the draws place the two Shapes on the same point exactly,
		// which is the only way to reach a zero GJK axis at all; the rest are
		// drawn apart on the same lattice.
		if rng.intn(3) != 0 {
			atB = rng.lattice()
		}

		_, boxA := finitenessWorld(a, atA, angleA)
		_, boxB := finitenessWorld(b, atB, angleB)

		normal, depth, ok := Penetration(
			a.shape, atA, angleA, a.verts,
			b.shape, atB, angleB, b.verts)
		finite(fmt.Sprintf("the normal of %s against %s", a.name, b.name), normal.X, normal.Y)
		finite(fmt.Sprintf("the depth of %s against %s", a.name, b.name), depth)

		// The same pair the other way round, which is the nine-arm switch's other
		// three arms and the flip that follows them.
		back, backDepth, backOK := Penetration(
			b.shape, atB, angleB, b.verts,
			a.shape, atA, angleA, a.verts)
		finite(fmt.Sprintf("the reversed normal of %s against %s", a.name, b.name), back.X, back.Y)
		finite(fmt.Sprintf("the reversed depth of %s against %s", a.name, b.name), backDepth)

		pairs++

		// The broadphase gate is what separates an answer the engine can act on
		// from one only a direct Penetration call ever sees. Detect and Overlap
		// both refuse a pair whose boxes do not intersect before any narrowphase
		// arm runs, so everything below the gate is recorded and nothing below it
		// is asserted.
		if !boxA.Intersects(boxB) {
			ungated++
			if ok {
				ungatedTouching++
			}
			continue
		}

		if ok != backOK {
			// An exactly tangent pair is where the two sides may legitimately
			// part: the end-cap rejection reads one Shape's neighbour tangents
			// against a normal that is the other's negated, and at a depth of
			// exactly zero the two readings can differ. Only a disagreement with
			// a real overlap behind it is a failure; the corpus's own
			// either-way-round test pins every pose that is not on a lattice.
			deeper := depth
			if backOK {
				deeper = backDepth
			}
			if deeper != 0 {
				asymmetric = append(asymmetric, fmt.Sprintf(
					"%s at %v angle %v against %s at %v angle %v: %v one way, %v the other, depth %v",
					a.name, atA, angleA, b.name, atB, angleB, ok, backOK, deeper))
			}
			continue
		}
		if !ok {
			continue
		}
		touching++
		if normal != (m.Vec2d{}) {
			// An exactly tangent pair has a real normal and a depth of zero,
			// which is the right answer and not a degenerate one.
			continue
		}

		// A pair reported as touching with no direction at all, and the whole of
		// the split this sweep is here to draw: the two closed forms take a seed
		// and keep the depth, the four GJK arms take none and lose it.
		closedForm := (a.shape.Kind == ShapeCircle && b.shape.Kind == ShapeCircle) ||
			(a.shape.Kind == ShapeCircle && b.shape.Kind == ShapeSegment) ||
			(a.shape.Kind == ShapeSegment && b.shape.Kind == ShapeCircle)
		if closedForm {
			seeded++
			continue
		}

		unseeded++
		if boxA.Centre() == boxB.Centre() {
			coincident++
		} else {
			collinear++
		}
		if depth != 0 {
			depthKept = append(depthKept, fmt.Sprintf(
				"%s at %v angle %v against %s at %v angle %v — depth %v with no normal at all",
				a.name, atA, angleA, b.name, atB, angleB, depth))
		}
	}

	// The Probe sweep. Every Probe runs along the lattice, so one aimed down a
	// row or a column is exactly parallel to an axis-aligned face plane, which is
	// the an − bn divide the corpus never reached.
	for range finitenessDraws {
		s := catalogue[rng.intn(len(catalogue))]
		angle := angles[rng.intn(len(angles))]
		at := rng.lattice()
		from, to := rng.lattice(), rng.lattice()
		radius := []float64{0, 0, 0.25, 0.3}[rng.intn(4)]

		// A quarter of the Probes are aimed straight down a lattice row or
		// column from the Shape's own placement, which is what puts the ray on a
		// face plane rather than merely near one.
		if rng.intn(4) == 0 {
			from = m.Vec2d{X: at.X - 2, Y: at.Y + 0.5}
			to = m.Vec2d{X: at.X + 2, Y: at.Y + 0.5}
			exactlyParallel++
		}

		hit, ok := ProbeShape(from, to, radius, s.shape, at, angle, s.verts)
		finite(fmt.Sprintf("the T of a Probe against %s", s.name), hit.T)
		finite(fmt.Sprintf("the Point of a Probe against %s", s.name), hit.Point.X, hit.Point.Y)
		finite(fmt.Sprintf("the Normal of a Probe against %s", s.name), hit.Normal.X, hit.Normal.Y)
		probes++
		if ok {
			hits++
		}
	}

	// The point-query sweep, which is ClosestPoint and the gradient behind it. A
	// point drawn on the lattice lands exactly on an edge of an axis-aligned box
	// often enough to be the poly.go:101 divide's only coverage.
	for range finitenessDraws {
		s := catalogue[rng.intn(len(catalogue))]
		angle := angles[rng.intn(len(angles))]
		at := rng.lattice()
		p := rng.lattice()

		// Half of them are placed on the Shape's own edge coordinate, which is
		// where the distance is exactly zero and the unguarded divide is 0/0.
		if rng.intn(2) == 0 {
			p = m.Vec2d{X: at.X + 0.5, Y: at.Y}
			exactlyOnAnEdge++
		}

		world, _ := finitenessWorld(s, at, angle)
		point, distance, gradient := pointQueryWorld(p, s.shape, world)
		finite(fmt.Sprintf("the surface point on %s", s.name), point.X, point.Y)
		finite(fmt.Sprintf("the distance to %s", s.name), distance)
		finite(fmt.Sprintf("the gradient on %s", s.name), gradient.X, gradient.Y)

		closest := ClosestPoint(p, s.shape, at, angle, s.verts)
		finite(fmt.Sprintf("ClosestPoint on %s", s.name), closest.X, closest.Y)
		points++
	}

	t.Logf("seed %#x: %d pairs, of which %d had boxes the broadphase gate refuses (%d of those "+
		"reported touching anyway, which no engine path can see); %d touching through the gate, "+
		"%d of them with no direction at all — %d from a closed form given no seed, which keeps "+
		"its depth, and %d from a GJK arm, which does not (%d on exactly coincident bounding box "+
		"centres, %d on a Minkowski difference with no area); %d Probes (%d Hits, %d aimed exactly "+
		"along a face plane); %d point queries (%d exactly on an edge coordinate)",
		uint64(finitenessSeed), pairs, ungated, ungatedTouching,
		touching, seeded+unseeded, seeded, unseeded, coincident, collinear,
		probes, hits, exactlyParallel, points, exactlyOnAnEdge)

	if len(depthKept) > 0 {
		t.Errorf("%d pairs report a depth with no normal at all, which a solver would spend "+
			"along a direction of nothing; the first is %s", len(depthKept), depthKept[0])
	}
	if len(asymmetric) > 0 {
		t.Errorf("%d pairs the broadphase admits read differently either way round with a real "+
			"overlap behind it; the first is %s", len(asymmetric), asymmetric[0])
	}

	// The emptiness guards. A sweep that never reached a degenerate case would
	// pass every assertion above while measuring nothing, which is the failure
	// mode two rounds of the allocation line already walked into.
	if touching == 0 {
		t.Error("no drawn pair touched at all, so the sweep exercised no narrowphase arm")
	}
	if hits == 0 {
		t.Error("no drawn Probe Hit anything, so the sweep exercised no Probe arm")
	}
	if coincident == 0 {
		t.Error("the sweep never placed two Shapes on exactly coincident bounding box centres, " +
			"so it says nothing about GJK's zero cold-start axis")
	}
	if seeded == 0 {
		t.Error("the sweep never reached a closed form with no seed, so it draws no contrast " +
			"between an arm that takes one and an arm that does not")
	}
	if exactlyParallel == 0 || exactlyOnAnEdge == 0 {
		t.Error("the sweep generated no exactly-parallel Probe or no exactly-on-an-edge point, " +
			"which are the two guards the cp corpus never reached")
	}
}

// TestACircleAgainstAZeroLengthSegmentAnswersWithANumberWhereChipmunkAnswersWithANaN
// pins the decision this ticket was handed: whether the port guards the fourth
// site of defect 6 or stays faithful to cp.
//
// It guards it. NewSegmentShape(p, p, r) builds a zero-length segment without
// complaint, cp's closestT is then clamp01(0/0), and every number downstream —
// the Normal, both surface Points and the Depth — is a NaN written straight into
// the Contact list and from there into a Velocity. The spec states the finiteness
// invariant as a constraint on the port and not only as a test, so a constructor
// an app can reach that produces a NaN Component is the constraint broken.
//
// It is the same defect, the same guard and the same constant as the three sites
// the porting index does name — ClosestT, the circle point query's divide by d,
// and the Polygon segment query's an − bn — and the port already restores C's
// guard at all three. This is the fourth, which the index simply did not
// enumerate; cp and C share it, so it is a departure under the fourth heading,
// a defect in cp.
//
// Guarded, a zero-length segment is the circle it geometrically is.
func TestACircleAgainstAZeroLengthSegmentAnswersWithANumberWhereChipmunkAnswersWithANaN(t *testing.T) {
	at := m.Vec2d{X: 1, Y: 1}
	segment := NewSegmentShape(at, at, 0.2)
	circle := NewCircleShape(0.5, m.Vec2d{})

	// Apart along +X: the summed radius is 0.7 and the centres are 0.1 apart, so
	// the pair is 0.6 deep along the axis between them. This is the case that is
	// not about coincidence at all — any circle whatever against a zero-length
	// segment took the NaN.
	normal, depth, ok := Penetration(circle, m.Vec2d{X: 1.1, Y: 1}, 0, nil, segment, m.Vec2d{}, 0, nil)
	if !ok {
		t.Fatal("a circle overlapping a zero-length segment does not touch it")
	}
	// The normal points from the circle towards the segment, which is cp's own
	// sense, and the segment's point is the way of −X from the circle's centre.
	wantNormal(t, normal, -1, 0)
	wantNear(t, "Depth", depth, 0.6)

	// The zero-length segment answers exactly as the circle it is: same radius,
	// same place, same numbers.
	asCircle := NewCircleShape(0.2, at)
	circleNormal, circleDepth, circleOK := Penetration(
		circle, m.Vec2d{X: 1.1, Y: 1}, 0, nil, asCircle, m.Vec2d{}, 0, nil)
	if !circleOK || circleNormal != normal || circleDepth != depth {
		t.Errorf("the zero-length segment gives %v/%v where the circle it is gives %v/%v",
			normal, depth, circleNormal, circleDepth)
	}

	// On exactly coincident centres the answer is the port's stated rule for a
	// coincident pair, not a NaN: no direction invented, and the full summed
	// radius to separate by. Penetration is pure and takes no randomness; the
	// seeded direction is Detect's.
	normal, depth, ok = Penetration(circle, at, 0, nil, segment, m.Vec2d{}, 0, nil)
	if !ok {
		t.Fatal("a circle on a zero-length segment's own point does not touch it")
	}
	if normal != (m.Vec2d{}) {
		t.Errorf("the coincident normal is %v, want no direction at all", normal)
	}
	wantNear(t, "Depth", depth, 0.7)

	// The point query behind ClosestPoint took the same divide through
	// closestPointOnSegment, and answered with a NaN surface point.
	point := ClosestPoint(m.Vec2d{X: 2, Y: 2}, segment, m.Vec2d{}, 0, nil)
	wantPoint(t, "ClosestPoint", point, 1+0.2/math.Sqrt2, 1+0.2/math.Sqrt2)
}

// TestTwoShapesWhoseBoundingBoxCentresCoincideExactlyReportNoDirectionAndNoDepth
// freezes the GJK cold-start gap as the port's own answer. It is a
// characterisation and not an endorsement: whether to guard it is a human
// decision this ticket was told not to take, and the numbers below are what that
// decision would be taken against.
//
// gjk()'s cold start is axis := centreA.Sub(centreB).Perp() over the two world
// bounding box centres. When they coincide exactly the axis is the zero vector,
// both support queries return the same vertex, the simplex collapses to a single
// Minkowski point, and closestTo takes its d <= 0 arm with a normal that
// Normalize guarded to zero — so the pair is reported as touching with no
// direction and, worse, with a depth of zero when the real overlap is a whole
// Shape wide.
//
// Detect's seeded nudge does not reach here. Collide passes c.nudged into
// collideWorld as coincident and collideCircles is the only arm that consumes it,
// so the symmetry-breaking built for two coincident circles has no counterpart
// for a circle concentric with a box.
func TestTwoShapesWhoseBoundingBoxCentresCoincideExactlyReportNoDirectionAndNoDepth(t *testing.T) {
	box := NewBoxShape(1, 1, 0)
	circle := NewCircleShape(0.5, m.Vec2d{})
	capsule := NewSegmentShape(m.Vec2d{X: -0.5}, m.Vec2d{X: 0.5}, 0.2)
	through := NewSegmentShape(m.Vec2d{X: -2}, m.Vec2d{X: 2}, 0)
	triangle, triangleVerts, err := NewPolygonShape(
		[]m.Vec2d{{X: -0.5, Y: -0.5}, {X: 0.5, Y: -0.5}, {Y: 0.5}}, 0)
	if err != nil {
		t.Fatalf("hulling the triangle: %v", err)
	}

	triangleShape := PolygonVerts(nil, triangle, triangleVerts)

	for _, c := range []struct {
		name           string
		a, b           Shape
		vertsA, vertsB []m.Vec2d
		degenerate     bool
		wantDepth      float64
		chipmunkNote   string
	}{
		{
			name: "two identical boxes", a: box, b: box,
			degenerate:   true,
			chipmunkNote: "cp degenerates identically here",
		},
		{
			name: "two identical capsules", a: capsule, b: capsule,
			degenerate:   true,
			chipmunkNote: "cp degenerates identically here",
		},
		{
			name: "two identical triangles", a: triangle, b: triangle,
			vertsA: triangleShape, vertsB: triangleShape,
			degenerate:   true,
			chipmunkNote: "cp degenerates identically here",
		},
		{
			name: "a circle concentric with a box", a: circle, b: box,
			degenerate:   true,
			chipmunkNote: "cp recovers with a normal of (0, 1) and a depth of 1",
		},
		{
			name: "a segment through a box's centre", a: through, b: box,
			degenerate:   true,
			chipmunkNote: "cp recovers with a normal of (0, 1) and a depth of 0.5",
		},
		{
			// The one pair kind the seed does reach, and the contrast that makes
			// the gap a gap: two coincident circles take a closed form, and that
			// closed form has a seeded direction parameter the four GJK arms do
			// not.
			name: "two coincident circles", a: circle, b: circle,
			wantDepth:    1,
			chipmunkNote: "cp invents a fixed (1, 0) that never breaks the symmetry",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			normal, depth, ok := Penetration(c.a, m.Vec2d{}, 0, c.vertsA, c.b, m.Vec2d{}, 0, c.vertsB)
			if !ok {
				t.Fatalf("%s does not touch at all", c.name)
			}
			if normal != (m.Vec2d{}) {
				t.Errorf("%s reports a normal of %v, want none — %s", c.name, normal, c.chipmunkNote)
			}
			if c.degenerate {
				// The depth is the half that is not ambiguous and is lost anyway:
				// two identical boxes overlap completely and the port says zero.
				if depth != 0 {
					t.Errorf("%s reports a depth of %v, want the degenerate zero this pins — %s",
						c.name, depth, c.chipmunkNote)
				}
				return
			}
			wantNear(t, "Depth", depth, c.wantDepth)
		})
	}
}
