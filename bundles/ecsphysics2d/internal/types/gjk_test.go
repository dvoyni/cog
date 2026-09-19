package types

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// The two things the simplex itself has to get right, whichever arm in
// collide_test.go reached it: a degenerate simplex answers with a number, and
// the cached id is a starting guess that never changes the answer.

func TestADegenerateSimplexAnswersWithANumber(t *testing.T) {
	// The guard ClosestT carries, which jakecoffman/cp drops: two Shapes sharing
	// a centre give GJK a simplex whose points coincide, and the unguarded
	// divide is zero over zero. The finiteness invariant is that no input puts a
	// NaN into anything the plugin writes.
	box := NewBoxShape(1, 1, 0)
	point := NewCircleShape(0, m.Vec2d{})
	zeroSegment := NewSegmentShape(m.Vec2d{}, m.Vec2d{}, 0)

	// The closed forms are #428's and are not retested here; these are the four
	// pairs that reach GJK.
	for name, pair := range map[string][2]Shape{
		"a box on a box":                 {box, box},
		"a point inside a box":           {point, box},
		"a zero-length segment in a box": {zeroSegment, box},
		"two zero-length segments":       {zeroSegment, zeroSegment},
	} {
		touch, ok := collide(t, pair[0], m.Vec2d{}, pair[1], m.Vec2d{})
		if !ok {
			continue
		}
		if math.IsNaN(touch.normal.X) || math.IsNaN(touch.normal.Y) {
			t.Errorf("%s gave the normal %v", name, touch.normal)
		}
		for i := range touch.count {
			p := touch.points[i]
			if math.IsNaN(p.depth) || math.IsInf(p.depth, 0) ||
				math.IsNaN(p.p1.X) || math.IsNaN(p.p1.Y) ||
				math.IsNaN(p.p2.X) || math.IsNaN(p.p2.Y) {
				t.Errorf("%s point %d is %+v", name, i, p)
			}
		}
	}
}

func TestTheCachedSimplexWarmStartsTheNextTickAndAnswersTheSame(t *testing.T) {
	// cp's collisionId. A cached id is a starting guess and nothing more, so the
	// answer must not depend on it — including when the id belonged to the pair
	// the other way round, which the port's per-tick index slots can do and
	// cp's fixed arbiter order cannot.
	box := NewBoxShape(1, 1, 0)
	var worldA, worldB [worldScratchLen]m.Vec2d
	transformA := NewTransformRigid(m.Vec2d{}, 0)
	transformB := NewTransformRigid(m.Vec2d{X: 0.1, Y: 0.9}, 0)
	usedA, _ := cacheWorldAt(box, transformA, nil, worldA[:])
	usedB, _ := cacheWorldAt(box, transformB, nil, worldB[:])

	cold, ok := collideWorld(box, transformA, worldA[:usedA], box, transformB, worldB[:usedB], m.Vec2d{}, 0)
	if !ok {
		t.Fatal("two stacked boxes do not touch")
	}
	if cold.gjkId == 0 {
		t.Fatal("a GJK pair came back with no simplex to warm start from")
	}

	// A simplex the pair itself produced, and the same one as the other party
	// would have packed it, which is what the port's per-tick index slots can
	// hand back when the two Bodies change places in the walk.
	for name, cached := range map[string]uint32{
		"its own simplex": cold.gjkId,
		// The same two Minkowski points as the pair the other way round would
		// have packed them: the two support indices swap inside each half.
		"a mirrored simplex": (cold.gjkId&0x00FF00FF)<<8 | (cold.gjkId&0xFF00FF00)>>8,
	} {
		warm, ok := collideWorld(
			box, transformA, worldA[:usedA], box, transformB, worldB[:usedB], m.Vec2d{}, cached)
		if !ok {
			t.Fatalf("warm started from %s the pair stopped touching", name)
		}
		if warm.count != cold.count || !vecNear(warm.normal, cold.normal) {
			t.Fatalf("warm started from %s the pair is %d points along %v, want %d along %v",
				name, warm.count, warm.normal, cold.count, cold.normal)
		}
		for i := range cold.count {
			if !near(warm.points[i].depth, cold.points[i].depth) {
				t.Errorf("warm started from %s point %d is %v deep, want %v",
					name, i, warm.points[i].depth, cold.points[i].depth)
			}
		}
	}

	// An id naming vertices the Shape does not have is cp's own clamp, which
	// puts every index at 0 and hands GJK a simplex whose two points coincide.
	// cp's answer there is a poor one and so is the port's; what is required of
	// it is that it is a number, which is what ClosestT's restored CPFLOAT_MIN
	// buys. No id the port itself packs can be out of range: every byte of one
	// is a vertex index of the Shape that produced it.
	wild, ok := collideWorld(
		box, transformA, worldA[:usedA], box, transformB, worldB[:usedB], m.Vec2d{}, 0xFFFFFFFF)
	if ok {
		if math.IsNaN(wild.normal.X) || math.IsNaN(wild.normal.Y) {
			t.Errorf("an out-of-range simplex gave the normal %v", wild.normal)
		}
		for i := range wild.count {
			if math.IsNaN(wild.points[i].depth) || math.IsInf(wild.points[i].depth, 0) {
				t.Errorf("an out-of-range simplex gave point %d the depth %v",
					i, wild.points[i].depth)
			}
		}
	}
}
