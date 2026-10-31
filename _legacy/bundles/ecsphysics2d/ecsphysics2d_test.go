package ecsphysics2d

import (
	"errors"
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The root is aliases and forwarders, so what these check is the wiring: that
// each forwarder passes its arguments through in the order the caller wrote
// them, which the compiler cannot see when the types all match.

func TestTheTransformForwardersPassTheirArgumentsThrough(t *testing.T) {
	if got, want := NewTransform(1, 2, 3, 4, 5, 6), (Transform{A: 1, C: 2, TX: 3, B: 4, D: 5, TY: 6}); got != want {
		t.Fatalf("NewTransform = %v, want %v", got, want)
	}
	if got := NewTransformIdentity().Point(m.Vec2d{X: 3, Y: 4}); got != (m.Vec2d{X: 3, Y: 4}) {
		t.Fatalf("the identity moved a point: %v", got)
	}
	if got, want := NewTransformTranslate(m.Vec2d{X: 1, Y: -1}).Point(m.Vec2d{}), (m.Vec2d{X: 1, Y: -1}); got != want {
		t.Fatalf("NewTransformTranslate = %v, want %v", got, want)
	}
	if got, want := NewTransformScale(2, 3).Point(m.Vec2d{X: 1, Y: 1}), (m.Vec2d{X: 2, Y: 3}); got != want {
		t.Fatalf("NewTransformScale = %v, want %v", got, want)
	}

	if got, want := NewTransformRotate(math.Pi/2).Point(m.Vec2d{X: 1}), (m.Vec2d{Y: 1}); !vecNear(got, want) {
		t.Fatalf("NewTransformRotate = %v, want %v", got, want)
	}

	rigid := NewTransformRigid(m.Vec2d{X: 3, Y: 4}, math.Pi/2)
	if got, want := rigid.Point(m.Vec2d{X: 1}), (m.Vec2d{X: 3, Y: 5}); !vecNear(got, want) {
		t.Fatalf("NewTransformRigid = %v, want %v", got, want)
	}
	point := m.Vec2d{X: 7, Y: -2}
	if got := NewTransformRigidInverse(rigid).Point(rigid.Point(point)); !vecNear(got, point) {
		t.Fatalf("NewTransformRigidInverse of NewTransformRigid = %v, want %v", got, point)
	}
}

func TestTheBBForwardersPassTheirArgumentsThrough(t *testing.T) {
	if got, want := NewBB(-1, -2, 3, 4), (BB{L: -1, B: -2, R: 3, T: 4}); got != want {
		t.Fatalf("NewBB = %v, want %v", got, want)
	}
	if got, want := NewBBForExtents(m.Vec2d{X: 1, Y: 1}, 2, 3), NewBB(-1, -2, 3, 4); got != want {
		t.Fatalf("NewBBForExtents = %v, want %v", got, want)
	}
	if got, want := NewBBForCircle(m.Vec2d{X: 1, Y: 1}, 2), NewBB(-1, -1, 3, 3); got != want {
		t.Fatalf("NewBBForCircle = %v, want %v", got, want)
	}
}

func TestTheMassForwardersPassTheirArgumentsThrough(t *testing.T) {
	square := []m.Vec2d{{X: -1, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 1}, {X: -1, Y: 1}}
	a, b := m.Vec2d{X: -1}, m.Vec2d{X: 1}

	if got, want := MomentForCircle(2, 0, 3, m.Vec2d{X: 4}), 41.0; !nearD(got, want) {
		t.Fatalf("MomentForCircle = %v, want %v", got, want)
	}
	if got, want := MomentForSegment(3, a, b, 0), 1.0; !nearD(got, want) {
		t.Fatalf("MomentForSegment = %v, want %v", got, want)
	}
	if got, want := MomentForBox(3, 2, 2), 2.0; !nearD(got, want) {
		t.Fatalf("MomentForBox = %v, want %v", got, want)
	}
	if got, want := MomentForPoly(3, square, m.Vec2d{}, 0), 2.0; !nearD(got, want) {
		t.Fatalf("MomentForPoly = %v, want %v", got, want)
	}
	if got, want := AreaForCircle(1, 2), 3*math.Pi; !nearD(got, want) {
		t.Fatalf("AreaForCircle = %v, want %v", got, want)
	}
	if got, want := AreaForSegment(a, b, 0.5), 0.25*math.Pi+2.0; !nearD(got, want) {
		t.Fatalf("AreaForSegment = %v, want %v", got, want)
	}
	if got, want := AreaForPoly(square, 0), 4.0; !nearD(got, want) {
		t.Fatalf("AreaForPoly = %v, want %v", got, want)
	}

	centroid, ok := CentroidForPoly(square)
	if !ok || !vecNear(centroid, m.Vec2d{}) {
		t.Fatalf("CentroidForPoly = %v, %v, want the origin", centroid, ok)
	}
	if _, ok := CentroidForPoly(nil); ok {
		t.Fatal("CentroidForPoly accepted an empty outline")
	}
}

func TestTheDynamicForwarderPassesItsArgumentsThrough(t *testing.T) {
	body, err := NewDynamic(2, 8, 15, 0.3)
	if err != nil {
		t.Fatalf("NewDynamic(2, 8, 15, 0.3): %v", err)
	}
	if got, want := body.Mass(), 2.0; got != want {
		t.Errorf("Mass = %v, want %v", got, want)
	}
	if got, want := body.Moment(), 8.0; got != want {
		t.Errorf("Moment = %v, want %v", got, want)
	}
	if got, want := body.Damping, 15.0; got != want {
		t.Errorf("Damping = %v, want %v", got, want)
	}
	if got, want := body.AngularDamping, 0.3; got != want {
		t.Errorf("AngularDamping = %v, want %v", got, want)
	}

	var refusal ErrBadMass
	if _, err := NewDynamic(0, 8, 0, 0); !errors.As(err, &refusal) {
		t.Errorf("NewDynamic with a mass of 0 returned %v, want an ErrBadMass", err)
	}
}

func TestTheShapeAndQueryForwardersPassTheirArgumentsThrough(t *testing.T) {
	circle := NewCircleShape(1, m.Vec2d{X: 2, Y: 3})
	if circle.Kind != ShapeCircle || circle.Radius != 1 || circle.Offset() != (m.Vec2d{X: 2, Y: 3}) {
		t.Fatalf("NewCircleShape = %+v", circle)
	}
	segment := NewSegmentShape(m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0.5)
	if segment.Kind != ShapeSegment || segment.A() != (m.Vec2d{X: -1}) || segment.B() != (m.Vec2d{X: 1}) {
		t.Fatalf("NewSegmentShape = %+v", segment)
	}
	if circle.CollisionBits != CollisionBitsAll || CollisionBitsNone != 0 {
		t.Fatalf("the collision constants came through as %#x and %#x", circle.CollisionBits, CollisionBitsNone)
	}

	// from, to, radius, then the Shape and where it is: the Hit is 0.4 of the
	// way along and its normal faces back down the Probe.
	hit, ok := ProbeShape(m.Vec2d{}, m.Vec2d{X: 10}, 0, NewCircleShape(1, m.Vec2d{}), m.Vec2d{X: 5}, 0, nil)
	if !ok || !nearD(hit.T, 0.4) || !vecNear(hit.Normal, m.Vec2d{X: -1}) {
		t.Fatalf("ProbeShape = %+v, %v, want a Hit at T 0.4 facing (-1, 0)", hit, ok)
	}

	// The mover first, from, to and its angle, then the target and where it is:
	// a unit box leads by 0.5, so it meets the circle 0.35 of the way along.
	hit, ok = ProbeShapeWith(NewBoxShape(1, 1, 0), m.Vec2d{}, m.Vec2d{X: 10}, 0, nil,
		NewCircleShape(1, m.Vec2d{}), m.Vec2d{X: 5}, 0, nil)
	if !ok || !nearD(hit.T, 0.35) || !vecNear(hit.Normal, m.Vec2d{X: -1}) {
		t.Fatalf("ProbeShapeWith = %+v, %v, want a Hit at T 0.35 facing (-1, 0)", hit, ok)
	}

	normal, depth, ok := Penetration(
		NewCircleShape(1, m.Vec2d{}), m.Vec2d{}, 0, nil,
		NewCircleShape(1, m.Vec2d{}), m.Vec2d{X: 1.5}, 0, nil)
	if !ok || !vecNear(normal, m.Vec2d{X: 1}) || !nearD(depth, 0.5) {
		t.Fatalf("Penetration = %v, %v, %v, want (1, 0), 0.5 and true", normal, depth, ok)
	}

	if got, want := ClosestPoint(m.Vec2d{X: 5}, NewCircleShape(1, m.Vec2d{}), m.Vec2d{}, 0, nil), (m.Vec2d{X: 1}); !vecNear(got, want) {
		t.Fatalf("ClosestPoint = %v, want %v", got, want)
	}
}

func TestTheIndexForwardersBuildAnIndexTheQueriesReach(t *testing.T) {
	static := NewStaticIndex(2)
	body := NewBodyIndex(0) // 0 takes the documented default of 2 m

	wall := ecs.Entity(1)
	mover := ecs.Entity(2)
	static.Insert(wall, NewSegmentShape(m.Vec2d{Y: -2}, m.Vec2d{Y: 2}, 0), m.Vec2d{X: 5}, 0, nil)
	body.Insert(mover, NewCircleShape(0.5, m.Vec2d{}), m.Vec2d{X: 3}, 0, nil)

	// Which index a query asks is the caller's choice, and the two are apart.
	hit, ok := static.Probe(m.Vec2d{}, m.Vec2d{X: 10}, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if !ok || hit.Entity != wall {
		t.Fatalf("the static index Probed %+v, %v, want the wall", hit, ok)
	}
	hit, ok = body.Probe(m.Vec2d{}, m.Vec2d{X: 10}, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if !ok || hit.Entity != mover {
		t.Fatalf("the Body index Probed %+v, %v, want the mover", hit, ok)
	}

	hits := static.ProbeAll(nil, m.Vec2d{}, m.Vec2d{X: 10}, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if len(hits) != 1 || hits[0].Entity != wall {
		t.Fatalf("ProbeAll = %+v, want the wall alone", hits)
	}

	crate := NewBoxShape(1, 1, 0)
	hit, ok = body.ProbeWith(crate, m.Vec2d{}, m.Vec2d{X: 10}, 0, nil, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if !ok || hit.Entity != mover {
		t.Fatalf("the Body index Probed a crate to %+v, %v, want the mover", hit, ok)
	}
	hits = static.ProbeAllWith(hits[:0], crate, m.Vec2d{}, m.Vec2d{X: 10}, 0, nil,
		CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if len(hits) != 1 || hits[0].Entity != wall {
		t.Fatalf("ProbeAllWith = %+v, want the wall alone", hits)
	}

	touching := body.Overlap(nil, NewCircleShape(1, m.Vec2d{}), m.Vec2d{X: 3.2}, 0, nil,
		CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
	if len(touching) != 1 || touching[0] != mover {
		t.Fatalf("Overlap = %v, want the mover alone", touching)
	}
	if excluded := body.Overlap(nil, NewCircleShape(1, m.Vec2d{}), m.Vec2d{X: 3.2}, 0, nil,
		CollisionBitsAll, CollisionBitsAll, mover); len(excluded) != 0 {
		t.Fatalf("Overlap with the mover excluded = %v, want nothing", excluded)
	}

	body.Clear()
	if _, ok := body.Probe(m.Vec2d{}, m.Vec2d{X: 10}, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity); ok {
		t.Error("a cleared index still Probed something")
	}
	static.Remove(wall)
	if _, ok := static.Probe(m.Vec2d{}, m.Vec2d{X: 10}, 0, CollisionBitsAll, CollisionBitsAll, ecs.NoEntity); ok {
		t.Error("a removed Entity was still Probed")
	}
}

func TestTheContactAliasCarriesItsMethodsAndItsPhases(t *testing.T) {
	// The root declares no Contact forwarder — an app never builds one, the
	// plugin does — so what there is to check is that the alias brings the
	// methods and the marks with it, which is the whole of a filter's and a
	// reacting System's surface.
	first, second := ecs.Entity(11), ecs.Entity(12)
	var entry Contact
	entry.A, entry.B = first, second
	entry.Count = 1
	entry.Normal = m.Vec2d{Y: 1}
	entry.Points[0].NormalImpulse = 5

	if got := entry.Other(first); got != second {
		t.Fatalf("Other(A) = %v, want B", got)
	}
	if got := entry.NormalFor(second); !vecNear(got, m.Vec2d{Y: -1}) {
		t.Fatalf("NormalFor(B) = %v, want the Normal negated", got)
	}
	if got := entry.TotalImpulse(); !vecNear(got, m.Vec2d{Y: 5}) {
		t.Fatalf("TotalImpulse = %v, want the Normal scaled by the Impulse", got)
	}
	// PreStep's scratch is unexported, so what the root can reach is the guard:
	// an entry the solver never PreStepped has no mass at its points, and an
	// Ended entry keeping its points is what makes that reachable at all.
	if got := entry.TotalKE(); got != 0 {
		t.Fatalf("TotalKE on an entry the solver never reached = %v, want none", got)
	}

	if entry.Dropped() || entry.Ignored() {
		t.Fatal("a fresh entry arrives marked")
	}
	entry.Drop()
	entry.Ignore()
	if !entry.Dropped() || !entry.Ignored() {
		t.Fatal("the marks did not stick")
	}

	var ended Contact
	ended.Phase = PhaseEnded
	ended.Drop()
	if ended.Dropped() {
		t.Fatal("an Ended entry was dropped; there is nothing left to take out of")
	}
	var fresh Contact
	if fresh.Phase != PhaseBegan {
		t.Fatalf("the zero Contact reads as %v, want Began", fresh.Phase)
	}
}

func nearD(got, want float64) bool   { return math.Abs(got-want) <= 1e-12 }
func vecNear(got, want m.Vec2d) bool { return nearD(got.X, want.X) && nearD(got.Y, want.Y) }
