package types

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

func TestBBConstructorsAgreeOnTheSameBox(t *testing.T) {
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

func TestBBTouchingBoxesIntersectAndDisjointOnesDoNot(t *testing.T) {
	box := NewBB(0, 0, 2, 2)

	if !box.Intersects(NewBB(1, 1, 3, 3)) {
		t.Fatal("overlapping boxes should intersect")
	}
	if !box.Intersects(NewBB(2, 0, 4, 2)) {
		t.Fatal("boxes sharing an edge should intersect")
	}
	if box.Intersects(NewBB(2.5, 0, 4, 2)) {
		t.Fatal("disjoint boxes should not intersect")
	}
	if !box.Contains(NewBB(0.5, 0.5, 1, 1)) {
		t.Fatal("an enclosed box should be contained")
	}
	if box.Contains(NewBB(0.5, 0.5, 3, 1)) {
		t.Fatal("a box sticking out should not be contained")
	}
	if !box.ContainsVec(m.Vec2d{X: 2, Y: 0}) {
		t.Fatal("a point on a corner should be contained")
	}
	if box.ContainsVec(m.Vec2d{X: 2.5, Y: 1}) {
		t.Fatal("a point outside should not be contained")
	}
}

func TestBBMergeExpandAndOffsetGrowAndMoveTheBox(t *testing.T) {
	box := NewBB(0, 0, 2, 2)

	if got, want := box.Merge(NewBB(-1, 1, 1, 5)), NewBB(-1, 0, 2, 5); got != want {
		t.Fatalf("Merge = %v, want %v", got, want)
	}
	if got, want := box.Expand(m.Vec2d{X: -3, Y: 1}), NewBB(-3, 0, 2, 2); got != want {
		t.Fatalf("Expand = %v, want %v", got, want)
	}
	if got, want := box.Offset(m.Vec2d{X: 1, Y: -1}), NewBB(1, -1, 3, 1); got != want {
		t.Fatalf("Offset = %v, want %v", got, want)
	}
	if got, want := box.Centre(), (m.Vec2d{X: 1, Y: 1}); got != want {
		t.Fatalf("Centre = %v, want %v", got, want)
	}
	if got, want := box.Area(), 4.0; got != want {
		t.Fatalf("Area = %v, want %v", got, want)
	}
}

func TestBBSegmentQueryReportsTheFractionAtWhichTheBoxIsEntered(t *testing.T) {
	box := NewBB(1, -1, 3, 1)

	if got, want := box.SegmentQuery(m.Vec2d{X: -1}, m.Vec2d{X: 3}), 0.5; got != want {
		t.Fatalf("SegmentQuery entering halfway = %v, want %v", got, want)
	}
	if got, want := box.SegmentQuery(m.Vec2d{X: 2}, m.Vec2d{X: 4}), 0.0; got != want {
		t.Fatalf("SegmentQuery starting inside = %v, want %v", got, want)
	}
	if got := box.SegmentQuery(m.Vec2d{X: -1, Y: 5}, m.Vec2d{X: 3, Y: 5}); got != math.MaxFloat64 {
		t.Fatalf("SegmentQuery missing above = %v, want infinity", got)
	}
	if got := box.SegmentQuery(m.Vec2d{X: -1}, m.Vec2d{X: 0}); got != math.MaxFloat64 {
		t.Fatalf("SegmentQuery stopping short = %v, want infinity", got)
	}

	if !box.IntersectsSegment(m.Vec2d{X: -1}, m.Vec2d{X: 3}) {
		t.Fatal("a segment crossing the box should intersect it")
	}
	if box.IntersectsSegment(m.Vec2d{X: -1, Y: 5}, m.Vec2d{X: 3, Y: 5}) {
		t.Fatal("a segment passing above should not intersect the box")
	}
}

func TestBBAllocatesNothing(t *testing.T) {
	box := NewBB(0, 0, 2, 2)
	other := NewBB(1, 1, 3, 3)
	point := m.Vec2d{X: 1, Y: 1}

	for name, call := range map[string]func(){
		"NewBB":             func() { bbSink = NewBB(0, 0, 2, 2) },
		"NewBBForExtents":   func() { bbSink = NewBBForExtents(point, 1, 2) },
		"NewBBForCircle":    func() { bbSink = NewBBForCircle(point, 1) },
		"Merge":             func() { bbSink = box.Merge(other) },
		"Expand":            func() { bbSink = box.Expand(point) },
		"Offset":            func() { bbSink = box.Offset(point) },
		"Centre":            func() { vecSink = box.Centre() },
		"Area":              func() { floatSink = box.Area() },
		"SegmentQuery":      func() { floatSink = box.SegmentQuery(point, other.Centre()) },
		"Intersects":        func() { boolSink = box.Intersects(other) },
		"Contains":          func() { boolSink = box.Contains(other) },
		"ContainsVec":       func() { boolSink = box.ContainsVec(point) },
		"IntersectsSegment": func() { boolSink = box.IntersectsSegment(point, other.Centre()) },
	} {
		if allocations := testing.AllocsPerRun(100, call); allocations != 0 {
			t.Fatalf("%s allocated %v times a call, want 0", name, allocations)
		}
	}
}
