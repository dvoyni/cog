package types

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

var (
	origin = m.Vec2d{}
	// unitCircle is a circle of radius 1 about its Body's Position.
	unitCircle = NewCircleShape(1, m.Vec2d{})
	// upright is a vertical segment two metres long with no thickness, which is
	// what static geometry is made of.
	upright = NewSegmentShape(m.Vec2d{Y: -1}, m.Vec2d{Y: 1}, 0)
)

func TestAProbeReportsTheFractionTheSurfaceAndANormalFacingTheProber(t *testing.T) {
	hit, ok := ProbeShape(origin, m.Vec2d{X: 10}, 0, unitCircle, m.Vec2d{X: 5}, 0, nil)
	if !ok {
		t.Fatal("a Probe straight through a circle met nothing")
	}
	if !nearF(hit.T, 0.4) {
		t.Errorf("T = %v, want 0.4", hit.T)
	}
	if want := (m.Vec2d{X: 4}); !vecNear(hit.Point, want) {
		t.Errorf("Point = %v, want %v on the circle's surface", hit.Point, want)
	}
	if want := (m.Vec2d{X: -1}); !vecNear(hit.Normal, want) {
		t.Errorf("Normal = %v, want %v, facing the Prober", hit.Normal, want)
	}

	// Snap-back is from.Lerp(to, T), which is what T carrying no unit buys.
	if want := (m.Vec2d{X: 4}); !vecNear(origin.Lerp(m.Vec2d{X: 10}, hit.T), want) {
		t.Errorf("the snap-back point is %v, want %v", origin.Lerp(m.Vec2d{X: 10}, hit.T), want)
	}

	if _, ok := ProbeShape(origin, m.Vec2d{X: 3}, 0, unitCircle, m.Vec2d{X: 5}, 0, nil); ok {
		t.Error("a Probe stopping short of a circle still met it")
	}
}

func TestAProbeAgainstASegmentMeetsWhicheverSideItCameFrom(t *testing.T) {
	left, ok := ProbeShape(origin, m.Vec2d{X: 10}, 0, upright, m.Vec2d{X: 5}, 0, nil)
	if !ok {
		t.Fatal("a Probe across a segment met nothing")
	}
	if !nearF(left.T, 0.5) || !vecNear(left.Point, m.Vec2d{X: 5}) || !vecNear(left.Normal, m.Vec2d{X: -1}) {
		t.Errorf("from the left the Hit is %+v, want T 0.5 at (5, 0) facing (-1, 0)", left)
	}

	right, ok := ProbeShape(m.Vec2d{X: 10}, origin, 0, upright, m.Vec2d{X: 5}, 0, nil)
	if !ok {
		t.Fatal("a Probe back across the segment met nothing")
	}
	if !vecNear(right.Normal, m.Vec2d{X: 1}) {
		t.Errorf("from the right the normal is %v, want (1, 0)", right.Normal)
	}

	// A Probe passing beyond the segment's end sweeps its end cap, which is
	// what a rounded segment has and a bare one has not.
	if _, ok := ProbeShape(m.Vec2d{X: 5, Y: 2}, m.Vec2d{X: 5, Y: 5}, 0, upright, origin, 0, nil); ok {
		t.Error("a Probe past a bare segment's end still met it")
	}
	capsule := NewSegmentShape(m.Vec2d{Y: -1}, m.Vec2d{Y: 1}, 0.5)
	hit, ok := ProbeShape(m.Vec2d{X: 2, Y: 1.4}, m.Vec2d{X: -2, Y: 1.4}, 0, capsule, origin, 0, nil)
	if !ok {
		t.Fatal("a Probe across a capsule's end cap met nothing")
	}
	if hit.T <= 0 || hit.T >= 1 {
		t.Errorf("the end-cap Hit is at T = %v, want it inside the Probe", hit.T)
	}
}

func TestAProbeThatStartsOverlappingReportsAHitAtTheStart(t *testing.T) {
	hit, ok := ProbeShape(m.Vec2d{X: 5.5}, m.Vec2d{X: 10}, 0, unitCircle, m.Vec2d{X: 5}, 0, nil)
	if !ok {
		t.Fatal("a Probe starting inside a circle met nothing")
	}
	if hit.T != 0 {
		t.Errorf("T = %v, want exactly 0", hit.T)
	}
	if want := (m.Vec2d{X: 6}); !vecNear(hit.Point, want) {
		t.Errorf("Point = %v, want %v, the nearest surface point", hit.Point, want)
	}
	if want := (m.Vec2d{X: 1}); !vecNear(hit.Normal, want) {
		t.Errorf("Normal = %v, want %v", hit.Normal, want)
	}

	// Touching is not overlapping, as cp's own collisions are strict, so a
	// point never Hits a point.
	if _, ok := ProbeShape(m.Vec2d{X: 6}, m.Vec2d{X: 6}, 0, unitCircle, m.Vec2d{X: 5}, 0, nil); ok {
		t.Error("a Probe resting exactly on a surface reported itself already inside")
	}
	// A point never Hits a point: a radius-0 Probe against a radius-0 circle has
	// zero area, so projectiles pass through each other unless the app gives
	// one a radius. Exact alignment is the measure-zero case cp answers with a
	// tangent, and nothing tries to fix that.
	point := NewCircleShape(0, m.Vec2d{})
	beside := m.Vec2d{X: 5, Y: 0.001}
	if _, ok := ProbeShape(origin, m.Vec2d{X: 10}, 0, point, beside, 0, nil); ok {
		t.Error("a radius-0 Probe Hit a radius-0 circle beside it")
	}
	if _, ok := ProbeShape(origin, m.Vec2d{X: 10}, 0.01, point, beside, 0, nil); !ok {
		t.Error("a Probe given a radius missed a point within it")
	}
}

func TestAProbeOnACircleCentreReturnsAPointRatherThanANaN(t *testing.T) {
	// C guards the divide by the distance and jakecoffman/cp does not, which is
	// defect 6: without the guard both coordinates of Point come back NaN.
	hit, ok := ProbeShape(m.Vec2d{X: 5}, m.Vec2d{X: 10}, 0, unitCircle, m.Vec2d{X: 5}, 0, nil)
	if !ok {
		t.Fatal("a Probe starting on a circle's centre met nothing")
	}
	if math.IsNaN(hit.Point.X) || math.IsNaN(hit.Point.Y) {
		t.Fatalf("Point = %v, want a number", hit.Point)
	}
	if want := (m.Vec2d{X: 5, Y: 1}); !vecNear(hit.Point, want) {
		t.Errorf("Point = %v, want %v from the fallback direction", hit.Point, want)
	}
	if want := (m.Vec2d{Y: 1}); !vecNear(hit.Normal, want) {
		t.Errorf("Normal = %v, want cp's fallback %v", hit.Normal, want)
	}
}

func TestAZeroLengthProbeIsLegalAndBehavesAsAnOverlap(t *testing.T) {
	inside := m.Vec2d{X: 5.25}
	hit, ok := ProbeShape(inside, inside, 0, unitCircle, m.Vec2d{X: 5}, 0, nil)
	if !ok || hit.T != 0 {
		t.Fatalf("a zero-length Probe inside a circle gave %+v, %v, want a Hit at T = 0", hit, ok)
	}
	if _, ok := ProbeShape(origin, origin, 0, unitCircle, m.Vec2d{X: 5}, 0, nil); ok {
		t.Error("a zero-length Probe well outside a circle still met it")
	}

	// A Probe with a radius is a circle, so a zero-length one of radius 2 finds
	// what a circle of radius 2 at that point would.
	if _, ok := ProbeShape(m.Vec2d{X: 3.5}, m.Vec2d{X: 3.5}, 2, unitCircle, m.Vec2d{X: 5}, 0, nil); !ok {
		t.Error("a zero-length Probe with a radius met nothing it overlaps")
	}
}

func TestAProbeGrowsTheShapeByItsOwnRadius(t *testing.T) {
	// The Probe's circle meets the surface a radius early, and Point stays on
	// the Shape itself rather than on the grown one.
	hit, ok := ProbeShape(origin, m.Vec2d{X: 10}, 0.5, unitCircle, m.Vec2d{X: 5}, 0, nil)
	if !ok {
		t.Fatal("a thick Probe met nothing")
	}
	if !nearF(hit.T, 0.35) {
		t.Errorf("T = %v, want 0.35, a radius earlier than a bare Probe's 0.4", hit.T)
	}
	if want := (m.Vec2d{X: 4}); !vecNear(hit.Point, want) {
		t.Errorf("Point = %v, want %v on the circle itself", hit.Point, want)
	}
}

func TestPenetrationReportsHowDeeplyTwoPlacedShapesOverlap(t *testing.T) {
	normal, depth, ok := Penetration(unitCircle, origin, 0, nil, unitCircle, m.Vec2d{X: 1.5}, 0, nil)
	if !ok {
		t.Fatal("two overlapping circles did not penetrate")
	}
	if !vecNear(normal, m.Vec2d{X: 1}) || !nearF(depth, 0.5) {
		t.Errorf("Penetration = %v, %v, want (1, 0) and 0.5", normal, depth)
	}

	if _, _, ok := Penetration(unitCircle, origin, 0, nil, unitCircle, m.Vec2d{X: 2}, 0, nil); ok {
		t.Error("two circles exactly touching penetrated")
	}

	// The circle and the segment agree whichever way round they are asked.
	forward, depthForward, ok := Penetration(unitCircle, origin, 0, nil, upright, m.Vec2d{X: 0.5}, 0, nil)
	if !ok || !vecNear(forward, m.Vec2d{X: 1}) || !nearF(depthForward, 0.5) {
		t.Fatalf("a circle against a segment gave %v, %v, %v", forward, depthForward, ok)
	}
	backward, depthBackward, ok := Penetration(upright, m.Vec2d{X: 0.5}, 0, nil, unitCircle, origin, 0, nil)
	if !ok || !vecNear(backward, forward.Negate()) || !nearF(depthBackward, depthForward) {
		t.Errorf("the segment first gave %v, %v, want the negated %v and the same depth",
			backward, depthBackward, forward)
	}
}

func TestPenetrationOnCoincidentCentresChoosesNoDirection(t *testing.T) {
	normal, depth, ok := Penetration(unitCircle, origin, 0, nil, unitCircle, origin, 0, nil)
	if !ok {
		t.Fatal("two coincident circles did not penetrate")
	}
	if normal != (m.Vec2d{}) {
		t.Errorf("Normal = %v, want the zero vector: a pure function takes no randomness", normal)
	}
	if !nearF(depth, 2) {
		t.Errorf("depth = %v, want 2", depth)
	}
}

func TestClosestPointIsOnTheSurfaceFromInsideAndOut(t *testing.T) {
	if got, want := ClosestPoint(m.Vec2d{X: 5}, unitCircle, origin, 0, nil), (m.Vec2d{X: 1}); !vecNear(got, want) {
		t.Errorf("ClosestPoint from outside = %v, want %v", got, want)
	}
	if got, want := ClosestPoint(m.Vec2d{X: 0.25}, unitCircle, origin, 0, nil), (m.Vec2d{X: 1}); !vecNear(got, want) {
		t.Errorf("ClosestPoint from inside = %v, want %v", got, want)
	}
	if got, want := ClosestPoint(m.Vec2d{X: 3, Y: 5}, upright, origin, 0, nil), (m.Vec2d{Y: 1}); !vecNear(got, want) {
		t.Errorf("ClosestPoint past a segment's end = %v, want %v", got, want)
	}

	// A rotated Shape is met where the rotation put it.
	if got, want := ClosestPoint(m.Vec2d{X: 5}, upright, origin, math.Pi/2, nil), (m.Vec2d{X: 1}); !vecNear(got, want) {
		t.Errorf("ClosestPoint on a quarter-turned segment = %v, want %v", got, want)
	}
}

func TestThePairPrimitivesAllocateNothing(t *testing.T) {
	to := m.Vec2d{X: 10}
	at := m.Vec2d{X: 5}

	for name, call := range map[string]func(){
		"ProbeShape a circle":  func() { hitSink, boolSink = ProbeShape(origin, to, 0, unitCircle, at, 0, nil) },
		"ProbeShape a segment": func() { hitSink, boolSink = ProbeShape(origin, to, 0.3, upright, at, 0, nil) },
		"ProbeShape inside":    func() { hitSink, boolSink = ProbeShape(at, to, 0, unitCircle, at, 0, nil) },
		"Penetration": func() {
			vecSink, floatSink, boolSink = Penetration(unitCircle, origin, 0, nil, upright, m.Vec2d{X: 0.5}, 0, nil)
		},
		"ClosestPoint": func() { vecSink = ClosestPoint(at, unitCircle, origin, 0, nil) },
	} {
		if allocations := testing.AllocsPerRun(100, call); allocations != 0 {
			t.Errorf("%s allocated %v times a call, want 0", name, allocations)
		}
	}
}

var hitSink Hit
