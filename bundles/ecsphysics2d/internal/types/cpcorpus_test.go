package types

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// The corpus. cpcases_test.go beside this file holds cp v2.4.0's frozen answers
// to every pair test and every query in it, emitted once by the harness on the
// research/port-vs-cp branch; these are the tests that drive them.
//
// This is the specification's layer A done exhaustively. Collide and the Probes
// are pure functions of values -- no history, no accumulation, no chaos -- so cp
// is an exact oracle for them and a transliteration slip in any of the six pair
// kinds is caught within one run. Layer B is the Joints' and the materials' own
// short-horizon tests; layer C never compares against cp at all.
//
// Tolerance is the specification's 1e-9 m on points and depth and 1e-9 on the
// normal's components. **There is no mismatch budget and no tolerance is
// loosened anywhere.** Every row either agrees with cp exactly or is one of the
// departures named below, and each of those is asserted as a difference -- the
// port's answer pinned, and the reason cp's is not the one to keep.

// cpTolerance is the specification's 1e-9: four orders above the float64 noise
// at the 512 m worst-case bound and six below the 0.005 m Slop.
const cpTolerance = 1e-9

// cpAmbiguous names the corpus rows where the two Shapes are placed so that
// there is no single right answer, and which therefore leave the fidelity
// corpus: the specification's ambiguity rule sorts every disagreement into a
// defect fix or a genuinely bistable case, and these are the bistable ones.
// Their answers are frozen as the *port's own* below, claiming nothing about cp.
//
// All five share one cause. Every one of them places the two Shapes so that
// their world bounding-box centres coincide exactly, where no separating
// direction is preferred over any other:
//
//   - the two circle rows are the specification's own rule, that Penetration on
//     coincident centres returns a zero normal with ok = true, because choosing
//     a direction is the caller's and a pure function takes no randomness. The
//     port still reports the right *depth* on both;
//   - the other three reach GJK, whose cold start takes its first axis from the
//     two bounding-box centres. That difference is the zero vector here, so
//     every support query answers with the same point, the simplex is
//     degenerate, and the pair comes back touching with a zero normal and a zero
//     depth. cp degenerates the same way on some of these placements -- two
//     identical boxes and two identical capsules agree with the port on a zero
//     normal -- and recovers a minimum translation on others.
//
// That last asymmetry is a finding rather than a fix: inventing an axis inside
// GJK is exactly cp's fixed (1, 0), which the specification rejects as a
// symmetry that never breaks, and Detect's seeded nudge is deliberately a
// Detect-level answer that the pure primitives do not take.
var cpAmbiguous = map[string]string{
	"circle-circle/circle.point/circle.unit/0.000,0.000,0.000,0.000,0.000,0.000":       "a point at a circle's own centre",
	"circle-circle/circle.unit/circle.unit/0.000,0.000,0.000,0.000,0.000,0.000":        "two circles sharing a centre",
	"circle-poly/circle.unit/poly.box/0.000,0.000,0.000,0.000,0.000,0.000":             "a circle concentric with a box",
	"segment-segment/segment.capsule/segment.bare/0.000,0.000,0.000,0.000,0.000,0.000": "two capsules crossing at both midpoints",
	"segment-poly/segment.bare/poly.box/0.000,0.000,0.000,0.000,0.000,0.000":           "a segment through a box's centre",
}

// ---------------------------------------------------------------------------
// Collide
// ---------------------------------------------------------------------------

func TestTheWholeCollisionCorpusAgreesWithChipmunkCaseByCase(t *testing.T) {
	if len(cpCollideCases) == 0 {
		t.Fatal("the collision corpus is empty")
	}

	kinds := map[string]int{}
	touching, shifted := 0, 0
	for _, want := range cpCollideCases {
		kinds[want.kind]++
		if want.ok {
			touching++
		}
		name := cpCaseName(want)
		if _, ambiguous := cpAmbiguous[name]; ambiguous {
			continue
		}
		if cpRoundedCirclePoly(want) && want.ok {
			shifted++
		}
		t.Run(name, func(t *testing.T) {
			got, ok := cpRunCollide(t, want)
			cpCompare(t, want, got, ok)
		})
	}

	// Three guards on the corpus itself. A corpus that had quietly stopped
	// covering something would pass every case left in it, which is the failure
	// mode a generated table has and a hand-written test does not.
	for _, kind := range []string{
		"circle-circle", "circle-segment", "segment-segment",
		"circle-poly", "segment-poly", "poly-poly",
	} {
		if kinds[kind] < 100 {
			t.Errorf("the corpus holds %d %s cases, want the specification's hundred or so",
				kinds[kind], kind)
		}
	}
	if touching < len(cpCollideCases)/3 {
		t.Errorf("only %d of %d corpus cases touch at all, which is too few to be testing a manifold",
			touching, len(cpCollideCases))
	}
	if shifted == 0 {
		t.Error("no case in the corpus reaches a rounded Polygon against a circle, " +
			"so the radius sign of defect 1 is not being asserted at all")
	}
}

// TestTheWholeCollisionCorpusReadsTheSameEitherWayRound drives every case
// through the nine-arm switch backwards as well as forwards.
//
// The corpus is written in cp's own kind order, which is the order cp.Collide
// sorts into and therefore the sense its normal is reported in. So this is the
// port's own claim rather than cp's: a caller may pass its two Shapes either way
// round and always reads a normal from its own first Shape towards its second.
//
// It is asserted to the bit rather than within a tolerance, because the three
// arms that turn an answer round negate and swap what the arm they called
// returned rather than recomputing it. The three kinds that are their own pair
// -- segment against segment and Polygon against Polygon -- take no such arm:
// the same code runs with the two arguments the other way about, and GJK's own
// walk decides which of a two-point manifold comes first. So the normal is
// compared exactly and the points as a set, which is the same convention
// TestTheNineArmSwitchReadsTheSameEitherWayRound already uses.
func TestTheWholeCollisionCorpusReadsTheSameEitherWayRound(t *testing.T) {
	for _, want := range cpCollideCases {
		t.Run(cpCaseName(want), func(t *testing.T) {
			shapeA, vertsA := cpPortShape(t, cpShapes[want.a])
			shapeB, vertsB := cpPortShape(t, cpShapes[want.b])
			transformA := NewTransformRigid(m.Vec2d{X: want.pose[0], Y: want.pose[1]}, want.pose[2])
			transformB := NewTransformRigid(m.Vec2d{X: want.pose[3], Y: want.pose[4]}, want.pose[5])

			forward, okForward := cpCollideAt(shapeA, transformA, vertsA, shapeB, transformB, vertsB)
			backward, okBackward := cpCollideAt(shapeB, transformB, vertsB, shapeA, transformA, vertsA)

			if okForward != okBackward {
				t.Fatalf("the pair touches %v one way round and %v the other", okForward, okBackward)
			}
			if !okForward {
				return
			}
			if forward.count != backward.count {
				t.Fatalf("the pair has %d points one way round and %d the other",
					forward.count, backward.count)
			}
			if forward.normal.Negate() != backward.normal {
				t.Errorf("the normal reversed is %v one way round and %v the other",
					forward.normal.Negate(), backward.normal)
			}
			// The two points of a manifold come out in the order the two support
			// edges were handed to cp's ContactPoints, which the swap reverses,
			// so the pair of points is compared as a set rather than in order.
			for i := range forward.count {
				matched := false
				for j := range backward.count {
					if forward.points[i].p1 == backward.points[j].p2 &&
						forward.points[i].p2 == backward.points[j].p1 &&
						forward.points[i].depth == backward.points[j].depth {
						matched = true
					}
				}
				if !matched {
					t.Errorf("point %d, %+v, has no counterpart the other way round",
						i, forward.points[i])
				}
			}
		})
	}
}

// TestTheAmbiguousPlacementsAreFrozenAsThePortsOwnAnswer pins what the port says
// at the five placements that left the fidelity corpus, and asserts that each of
// them really is a placement cp answers differently -- so an entry that stopped
// being a departure fails here rather than quietly excusing a case that agrees.
//
// These numbers claim nothing whatever about cp. They are here so that the
// port's answer at a placement with no single right answer cannot change without
// somebody saying so.
func TestTheAmbiguousPlacementsAreFrozenAsThePortsOwnAnswer(t *testing.T) {
	frozen := map[string]struct {
		normal [2]float64
		count  int
		points [2][5]float64
	}{
		// The two circle rows keep the right depth and withhold only the
		// direction, which is the specification's rule for coincident centres.
		"circle-circle/circle.point/circle.unit/0.000,0.000,0.000,0.000,0.000,0.000": {
			normal: [2]float64{0, 0}, count: 1,
			points: [2][5]float64{{0, 0, 0, 0, 0.5}},
		},
		"circle-circle/circle.unit/circle.unit/0.000,0.000,0.000,0.000,0.000,0.000": {
			normal: [2]float64{0, 0}, count: 1,
			points: [2][5]float64{{0, 0, 0, 0, 1}},
		},
		// The three GJK rows lose the depth as well, the cold-start axis being
		// the zero vector. cp answers (0, 1) at a depth of 1 for the first and
		// (0, 1) at 0.5 for the third; neither is a direction the port is
		// entitled to invent, and the lost depth is this ticket's finding.
		"circle-poly/circle.unit/poly.box/0.000,0.000,0.000,0.000,0.000,0.000": {
			normal: [2]float64{0, 0}, count: 1,
			points: [2][5]float64{{0, 0, 0.5, -0.5, 0}},
		},
		"segment-segment/segment.capsule/segment.bare/0.000,0.000,0.000,0.000,0.000,0.000": {
			normal: [2]float64{0, 0}, count: 2,
			points: [2][5]float64{{0.75, -0.25, 1, 0, 0}, {0.75, -0.25, 1, 0, 0}},
		},
		"segment-poly/segment.bare/poly.box/0.000,0.000,0.000,0.000,0.000,0.000": {
			normal: [2]float64{0, 0}, count: 2,
			points: [2][5]float64{{1, 0, 0.5, -0.5, 0}, {1, 0, 0.5, -0.5, 0}},
		},
	}

	seen := map[string]bool{}
	for _, want := range cpCollideCases {
		name := cpCaseName(want)
		if _, ambiguous := cpAmbiguous[name]; !ambiguous {
			continue
		}
		seen[name] = true
		pin, ok := frozen[name]
		if !ok {
			t.Errorf("%s is named ambiguous but its answer is not frozen", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			got, ok := cpRunCollide(t, want)
			if !ok {
				t.Fatal("the pair does not touch at all")
			}
			if got.count != pin.count {
				t.Fatalf("the manifold has %d points where the port's frozen answer has %d",
					got.count, pin.count)
			}
			cpNearVec(t, "normal", got.normal, pin.normal)
			for i := range got.count {
				cpNear(t, "p1.X", got.points[i].p1.X, pin.points[i][0])
				cpNear(t, "p1.Y", got.points[i].p1.Y, pin.points[i][1])
				cpNear(t, "p2.X", got.points[i].p2.X, pin.points[i][2])
				cpNear(t, "p2.Y", got.points[i].p2.Y, pin.points[i][3])
				cpNear(t, "depth", got.points[i].depth, pin.points[i][4])
			}

			// An exclusion that agrees with cp is a stale exclusion, and a stale
			// exclusion silently shrinks the fidelity corpus.
			if cpAgrees(want, got, ok) {
				t.Errorf("%s agrees with cp and does not belong on the ambiguous list", name)
			}
		})
	}
	for name := range cpAmbiguous {
		if !seen[name] {
			t.Errorf("%s is named ambiguous but no corpus case has that name", name)
		}
	}
}

// ---------------------------------------------------------------------------
// The Probes
// ---------------------------------------------------------------------------

func TestEveryProbeInTheCorpusAgreesWithChipmunk(t *testing.T) {
	if len(cpProbeCases) == 0 {
		t.Fatal("the probe corpus is empty")
	}
	inside := 0
	for i, want := range cpProbeCases {
		if cpProbeStartsOnOrInside(t, want) {
			// cp's Shape.SegmentQuery answers a Probe that starts within its own
			// radius of the Shape from a branch of its own, which reports T = 0
			// and then leaves Point at the ray's far end and derives Normal from
			// a point query that may itself be a NaN. The port's rule is the
			// specification's and is asserted in the test below instead.
			inside++
			continue
		}
		t.Run(cpProbeName(i, want), func(t *testing.T) {
			shape, verts := cpPortShape(t, cpShapes[want.shape])
			hit, ok := ProbeShape(
				m.Vec2d{X: want.ray[0], Y: want.ray[1]},
				m.Vec2d{X: want.ray[2], Y: want.ray[3]},
				want.ray[4], shape,
				m.Vec2d{X: want.pose[0], Y: want.pose[1]}, want.pose[2], verts,
			)
			if ok != want.ok {
				t.Fatalf("the Probe hits %v where cp says %v", ok, want.ok)
			}
			if !ok {
				return
			}
			cpNear(t, "T", hit.T, want.t)
			cpNearVec(t, "point", hit.Point, want.point)
			cpNearVec(t, "normal", hit.Normal, want.normal)
		})
	}
	if inside == 0 {
		t.Error("no Probe in the corpus starts inside its Shape, so the one place " +
			"the port and cp part company on a Probe is not being exercised")
	}
}

// TestAProbeThatStartsInsideFollowsThePortsRuleRatherThanChipmunks asserts the
// departure the rows above are held out of.
//
// The specification's rule is that a Probe starting overlapped reports a Hit at
// T = 0 with the normal from the nearest surface point, because ignoring what a
// Probe starts inside would let a projectile leave a wall it spawned in and
// would pass spawn validation silently. cp reports T = 0 too, but it never sets
// Point in that branch -- it is left at the ray's far end, which is not on the
// Shape at all -- and it takes Normal from a point query that divides by a
// distance it has not checked, so on a circle's own centre it is a NaN.
//
// The port's answer is therefore asserted against the port's own point query
// rather than against cp: the Hit is at the start, the Point is the nearest
// point on the surface, and the Normal is finite and of unit length.
func TestAProbeThatStartsInsideFollowsThePortsRuleRatherThanChipmunks(t *testing.T) {
	checked := 0
	for i, want := range cpProbeCases {
		if !cpProbeStartsOnOrInside(t, want) {
			continue
		}
		checked++
		t.Run(cpProbeName(i, want), func(t *testing.T) {
			shape, verts := cpPortShape(t, cpShapes[want.shape])
			from := m.Vec2d{X: want.ray[0], Y: want.ray[1]}
			at := m.Vec2d{X: want.pose[0], Y: want.pose[1]}
			hit, ok := ProbeShape(from, m.Vec2d{X: want.ray[2], Y: want.ray[3]},
				want.ray[4], shape, at, want.pose[2], verts)

			// The Probe that starts on the surface rather than within it. The
			// port's overlap test is strict, as its collisions are, so a Probe
			// exactly touching a surface has not started inside it and a point
			// never Hits a point; cp's test is not strict and reports a Hit
			// there. The port then runs its ordinary sweep from a point on the
			// surface, so what is asserted is the invariant that survives the
			// noise: if it Hits at all it Hits at once, and where it started.
			_, distance, _ := cpPointQuery(t, want.shape, want.pose, from)
			if distance >= want.ray[4] {
				if !ok {
					return
				}
				if hit.T > cpTolerance {
					t.Errorf("a Probe starting %v from the surface Hits at T = %v, want the start",
						distance, hit.T)
				}
				if hit.Point.Sub(from).Length() > cpTolerance {
					t.Errorf("a Probe starting at %v Hits at %v, want where it started", from, hit.Point)
				}
				return
			}

			if !ok {
				t.Fatal("a Probe that starts overlapping reports no Hit")
			}
			if hit.T != 0 {
				t.Errorf("a Probe that starts overlapping reports T = %v, want 0", hit.T)
			}
			surface := ClosestPoint(from, shape, at, want.pose[2], verts)
			if hit.Point != surface {
				t.Errorf("the Hit is at %v where the nearest surface point is %v", hit.Point, surface)
			}
			if math.IsNaN(hit.Normal.X) || math.IsNaN(hit.Normal.Y) {
				t.Fatalf("the normal is %v", hit.Normal)
			}
			if length := hit.Normal.Length(); math.Abs(length-1) > cpTolerance {
				t.Errorf("the normal %v is %v long, want a unit vector", hit.Normal, length)
			}
		})
	}
	if checked == 0 {
		t.Fatal("no Probe in the corpus starts inside its Shape")
	}
}

func TestEveryPointQueryInTheCorpusAgreesWithChipmunk(t *testing.T) {
	if len(cpPointCases) == 0 {
		t.Fatal("the point-query corpus is empty")
	}
	centres, onSegments := 0, 0
	for i, want := range cpPointCases {
		switch {
		case math.IsNaN(want.point[0]) || math.IsNaN(want.point[1]):
			// Defect 6: a point exactly on a circle's centre divides by a
			// distance of zero, which C guards and jakecoffman/cp does not. The
			// port's answer is asserted in the finiteness test below.
			centres++
			continue
		case cpOnSegmentCentreLine(t, want):
			// The Segment's own defect, asserted as a difference below.
			onSegments++
			continue
		}
		t.Run(cpPointName(i, want), func(t *testing.T) {
			shape, verts := cpPortShape(t, cpShapes[want.shape])
			p := m.Vec2d{X: want.p[0], Y: want.p[1]}
			at := m.Vec2d{X: want.pose[0], Y: want.pose[1]}

			// ClosestPoint is the exported primitive and reports the point
			// alone; the distance and the gradient behind it are compared too,
			// this file being in the package that computes them.
			point := ClosestPoint(p, shape, at, want.pose[2], verts)
			cpNearVec(t, "point", point, want.point)

			inner, distance, gradient := cpPointQuery(t, want.shape, want.pose, p)
			if inner != point {
				t.Errorf("ClosestPoint says %v where the query behind it says %v", point, inner)
			}
			cpNear(t, "distance", distance, want.distance)
			cpNearVec(t, "gradient", gradient, want.gradient)
		})
	}
	if centres == 0 {
		t.Error("no point query in the corpus lands on a circle's own centre, " +
			"so defect 6's divide is not being exercised")
	}
	if onSegments == 0 {
		t.Error("no point query in the corpus lands on a rotated Segment's centre line")
	}
}

// TestAPointOnACircleCentreAnswersWithANumberWhereChipmunkAnswersWithANaN is
// defect 6 asserted as a difference. C tests d > 0 before dividing by it and
// jakecoffman/cp does not, so cp's nearest surface point on a circle's own
// centre is a NaN in both components. The port keeps C's guard and answers with
// the point the circle's own fallback direction picks.
func TestAPointOnACircleCentreAnswersWithANumberWhereChipmunkAnswersWithANaN(t *testing.T) {
	found := 0
	for i, want := range cpPointCases {
		if !math.IsNaN(want.point[0]) && !math.IsNaN(want.point[1]) {
			continue
		}
		found++
		t.Run(cpPointName(i, want), func(t *testing.T) {
			p := m.Vec2d{X: want.p[0], Y: want.p[1]}
			point, distance, gradient := cpPointQuery(t, want.shape, want.pose, p)
			for name, v := range map[string]float64{
				"point.X": point.X, "point.Y": point.Y, "distance": distance,
				"gradient.X": gradient.X, "gradient.Y": gradient.Y,
			} {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Errorf("%s is %v where cp gives a NaN; the port is meant to give a number", name, v)
				}
			}
			// The distance is still cp's own -- the divide is the only thing
			// that differs -- and a centre is one radius inside the surface.
			cpNear(t, "distance", distance, want.distance)
			if offset := point.Sub(p).Length(); math.Abs(offset-cpShapes[want.shape].radius) > cpTolerance {
				t.Errorf("the surface point is %v from the centre, want the radius %v",
					offset, cpShapes[want.shape].radius)
			}
		})
	}
	if found == 0 {
		t.Fatal("no point query in the corpus lands on a circle's own centre")
	}
}

// TestARotatedSegmentsPointQueryGradientIsChipmunksLocalNormalTurned is a defect
// in jakecoffman/cp that is not on the specification's record, found by this
// corpus and asserted here as a difference.
//
// cp's Segment keeps two normals: n, the one the constructor derives in the
// Body's own frame, and tn, the one CacheData writes each time the Shape is
// placed. Segment.PointQuery takes its closest point from the *transformed*
// endpoints ta and tb, and then, when the query point lies on the centre line
// and the gradient cannot be derived by dividing, falls back to `seg.n` --
// segment.go:106, the local normal, in an answer that is otherwise entirely in
// world space. Every sibling reads tn: SegmentQuery at segment.go:111 and the
// three collision arms at collision.go:129, :267 and :272.
//
// So a rotated Segment hands back a gradient in the Body's frame. The port keeps
// its world normal in the world cache and has nothing local to reach for, so its
// answer is cp's turned by the Shape's own angle -- which is what this asserts,
// exactly, rather than merely noting that the two differ.
func TestARotatedSegmentsPointQueryGradientIsChipmunksLocalNormalTurned(t *testing.T) {
	found := 0
	for i, want := range cpPointCases {
		if !cpOnSegmentCentreLine(t, want) {
			continue
		}
		found++
		t.Run(cpPointName(i, want), func(t *testing.T) {
			p := m.Vec2d{X: want.p[0], Y: want.p[1]}
			point, distance, gradient := cpPointQuery(t, want.shape, want.pose, p)

			// Everything but the gradient is cp's own: the fallback is the only
			// thing the local normal reaches.
			cpNearVec(t, "point", point, want.point)
			cpNear(t, "distance", distance, want.distance)

			turned := cpVec(want.gradient).Rotate(m.ForAngle(want.pose[2]))
			cpNear(t, "gradient.X", gradient.X, turned.X)
			cpNear(t, "gradient.Y", gradient.Y, turned.Y)

			if want.pose[2] != 0 && gradient == cpVec(want.gradient) {
				t.Errorf("the gradient %v is cp's untouched, so the Segment was not rotated after all", gradient)
			}
		})
	}
	if found == 0 {
		t.Fatal("no point query in the corpus lands on a rotated Segment's centre line")
	}
}

// ---------------------------------------------------------------------------
// Running one case
// ---------------------------------------------------------------------------

// cpPortShape is the corpus's Shape as the port builds it. Both engines are
// handed the same outline and both hull it, so neither this file nor the harness
// decides the winding.
func cpPortShape(t testing.TB, s cpShape) (Shape, []m.Vec2d) {
	t.Helper()
	switch s.kind {
	case "circle":
		return NewCircleShape(s.radius, cpVec(s.verts[0])), nil
	case "segment":
		return NewSegmentShape(cpVec(s.verts[0]), cpVec(s.verts[1]), s.radius), nil
	case "box":
		box := NewBB(s.verts[0][0], s.verts[0][1], s.verts[1][0], s.verts[1][1])
		return NewBoxShapeFor(box, s.radius), nil
	}
	outline := make([]m.Vec2d, len(s.verts))
	for i, v := range s.verts {
		outline[i] = cpVec(v)
	}
	shape, polygon, err := NewPolygonShape(outline, s.radius)
	if err != nil {
		t.Fatalf("the corpus outline %v is not a Polygon: %v", s.verts, err)
	}
	return shape, PolygonVerts(nil, shape, polygon)
}

// cpRunCollide places the case's two Shapes and runs the narrowphase cold.
func cpRunCollide(t testing.TB, want cpCollideCase) (touching, bool) {
	t.Helper()
	shapeA, vertsA := cpPortShape(t, cpShapes[want.a])
	shapeB, vertsB := cpPortShape(t, cpShapes[want.b])
	return cpCollideAt(
		shapeA, NewTransformRigid(m.Vec2d{X: want.pose[0], Y: want.pose[1]}, want.pose[2]), vertsA,
		shapeB, NewTransformRigid(m.Vec2d{X: want.pose[3], Y: want.pose[4]}, want.pose[5]), vertsB,
	)
}

// cpCollideAt is collideWorld over two placed Shapes, cold: no seeded direction
// for coincident centres and a cached simplex of 0, which is what makes the
// answer a function of the two Shapes and their poses alone.
func cpCollideAt(
	a Shape, transformA Transform, vertsA []m.Vec2d,
	b Shape, transformB Transform, vertsB []m.Vec2d,
) (touching, bool) {
	worldA := make([]m.Vec2d, worldLenFor(a, vertsA))
	worldB := make([]m.Vec2d, worldLenFor(b, vertsB))
	usedA, _ := cacheWorldAt(a, transformA, vertsA, worldA)
	usedB, _ := cacheWorldAt(b, transformB, vertsB, worldB)
	return collideWorld(a, transformA, worldA[:usedA], b, transformB, worldB[:usedB], m.Vec2d{}, 0)
}

// cpPointQuery is pointQueryWorld over a placed corpus Shape.
func cpPointQuery(t testing.TB, name string, pose [3]float64, p m.Vec2d) (m.Vec2d, float64, m.Vec2d) {
	t.Helper()
	shape, verts := cpPortShape(t, cpShapes[name])
	world := make([]m.Vec2d, worldLenFor(shape, verts))
	at := m.Vec2d{X: pose[0], Y: pose[1]}
	used, _ := cacheWorldAt(shape, NewTransformRigid(at, pose[2]), verts, world)
	return pointQueryWorld(p, shape, world[:used])
}

// cpRoundedCirclePoly says whether a case is a circle against a Polygon with a
// rounding radius, which is the one pair kind defect 1 touches.
func cpRoundedCirclePoly(want cpCollideCase) bool {
	return want.kind == "circle-poly" && cpShapes[want.b].radius > 0
}

// cpProbeStartsOnOrInside says whether the Probe begins no further outside the
// Shape than the corpus's own tolerance can resolve.
//
// Strictly inside is cp's own starts-inside branch, which it answers from rather
// than from the class's sweep. The tolerance is added because at a separation
// below 1e-9 neither engine's branch is a comparable quantity: which side of
// `distance <= radius` the start falls, and whether a zero-area surface is
// crossed at all, are settled by bits this corpus does not resolve. So the row
// cannot be a fidelity case either way, and the port's own rule is asserted
// instead.
func cpProbeStartsOnOrInside(t testing.TB, want cpProbeCase) bool {
	t.Helper()
	_, distance, _ := cpPointQuery(t, want.shape, want.pose, m.Vec2d{X: want.ray[0], Y: want.ray[1]})
	return distance <= want.ray[4]+cpTolerance
}

// cpOnSegmentCentreLine says whether the queried point lies on a Segment's
// centre line, which is cp's own branch: within MAGIC_EPSILON of it the gradient
// cannot be derived by dividing and both engines fall back to a stored normal --
// cp to the local one and the port to the world one.
func cpOnSegmentCentreLine(t testing.TB, want cpPointCase) bool {
	t.Helper()
	if cpShapes[want.shape].kind != "segment" {
		return false
	}
	_, distance, _ := cpPointQuery(t, want.shape, want.pose, m.Vec2d{X: want.p[0], Y: want.p[1]})
	return distance+cpShapes[want.shape].radius <= magicEpsilon
}

// cpCompare is the corpus's one assertion over a collision case.
func cpCompare(t testing.TB, want cpCollideCase, got touching, ok bool) {
	t.Helper()
	if ok != want.ok {
		t.Fatalf("the pair touches %v where cp says %v", ok, want.ok)
	}
	if !ok {
		return
	}
	if got.count != want.count {
		t.Fatalf("the manifold has %d points where cp has %d", got.count, want.count)
	}
	cpNearVec(t, "normal", got.normal, want.normal)

	// Defect 1, asserted as a difference rather than excused. cp's CircleToPoly
	// offsets the Polygon's surface point by +poly.r where C has -poly->r, so its
	// p2 lands exactly 2*r the wrong side of the face and the pair reads 2*r less
	// deep. The normal and the point on the circle are cp's own, untouched, which
	// is what pins the departure to the one sign and to nothing else.
	shift := 0.0
	if cpRoundedCirclePoly(want) {
		shift = 2 * cpShapes[want.b].radius
	}

	for i := range got.count {
		cpNear(t, "p1.X", got.points[i].p1.X, want.points[i][0])
		cpNear(t, "p1.Y", got.points[i].p1.Y, want.points[i][1])
		cpNear(t, "p2.X", got.points[i].p2.X, want.points[i][2]-shift*want.normal[0])
		cpNear(t, "p2.Y", got.points[i].p2.Y, want.points[i][3]-shift*want.normal[1])
		cpNear(t, "depth", got.points[i].depth, want.points[i][4]+shift)
	}
}

// cpAgrees is cpCompare's question without its answer: whether this case would
// have passed the fidelity corpus. It is how a held-out case is checked for
// being held out for nothing, and it is written out rather than run through a
// throwaway *testing.T because cpCompare's Fatalf would take the goroutine with
// it.
func cpAgrees(want cpCollideCase, got touching, ok bool) bool {
	if ok != want.ok {
		return false
	}
	if !ok {
		return true
	}
	if got.count != want.count {
		return false
	}
	shift := 0.0
	if cpRoundedCirclePoly(want) {
		shift = 2 * cpShapes[want.b].radius
	}
	near := func(got, want float64) bool {
		return !math.IsNaN(got) && !math.IsNaN(want) && math.Abs(got-want) <= cpTolerance
	}
	if !near(got.normal.X, want.normal[0]) || !near(got.normal.Y, want.normal[1]) {
		return false
	}
	for i := range got.count {
		if !near(got.points[i].p1.X, want.points[i][0]) ||
			!near(got.points[i].p1.Y, want.points[i][1]) ||
			!near(got.points[i].p2.X, want.points[i][2]-shift*want.normal[0]) ||
			!near(got.points[i].p2.Y, want.points[i][3]-shift*want.normal[1]) ||
			!near(got.points[i].depth, want.points[i][4]+shift) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

func cpVec(v [2]float64) m.Vec2d { return m.Vec2d{X: v[0], Y: v[1]} }

// cpNear is the corpus's one comparison. A NaN on either side is a mismatch and
// never a pass: |got - want| > tolerance is false for a NaN, so a test written
// the obvious way would let every one of cp's unguarded divides through in
// silence, which is the opposite of what this corpus is for.
func cpNear(t testing.TB, name string, got, want float64) {
	t.Helper()
	switch {
	case math.IsNaN(got) && math.IsNaN(want):
		t.Errorf("%s is a NaN on both sides", name)
	case math.IsNaN(got):
		t.Errorf("%s is a NaN where cp says %v", name, want)
	case math.IsNaN(want):
		t.Errorf("%s is %v where cp says a NaN", name, got)
	case math.Abs(got-want) > cpTolerance:
		t.Errorf("%s is %v where cp says %v, off by %v", name, got, want, math.Abs(got-want))
	}
}

func cpNearVec(t testing.TB, name string, got m.Vec2d, want [2]float64) {
	t.Helper()
	cpNear(t, name+".X", got.X, want[0])
	cpNear(t, name+".Y", got.Y, want[1])
}

func cpCaseName(want cpCollideCase) string {
	return want.kind + "/" + want.a + "/" + want.b + "/" + cpPose(want.pose[:])
}

func cpProbeName(i int, want cpProbeCase) string {
	return want.shape + "/" + cpPose(want.pose[:]) + "/" + cpIndex(i)
}

func cpPointName(i int, want cpPointCase) string {
	return want.shape + "/" + cpPose(want.pose[:]) + "/" + cpIndex(i)
}

// cpPose names a placement in a subtest name, so a failure says which one it was
// without the reader counting rows.
func cpPose(pose []float64) string {
	out := ""
	for i, v := range pose {
		if i > 0 {
			out += ","
		}
		out += string(cpAppendShort(nil, v))
	}
	return out
}

func cpAppendShort(dst []byte, v float64) []byte {
	if math.Signbit(v) {
		dst = append(dst, '-')
		v = -v
	}
	whole := int(v)
	dst = cpAppendInt(dst, whole)
	dst = append(dst, '.')
	frac := int(math.Round((v - float64(whole)) * 1000))
	return append(dst, byte('0'+frac/100%10), byte('0'+frac/10%10), byte('0'+frac%10))
}

func cpAppendInt(dst []byte, v int) []byte {
	if v >= 10 {
		dst = cpAppendInt(dst, v/10)
	}
	return append(dst, byte('0'+v%10))
}

func cpIndex(i int) string { return string(cpAppendInt(nil, i)) }
