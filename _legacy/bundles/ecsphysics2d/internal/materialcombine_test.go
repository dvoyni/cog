package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The material a pair is solved at: cp's two combination rules, and the ranges
// the two Shape fields carry. Nothing here needs cp as an oracle — a product is
// a product — so nothing here quotes it.

// detectPair runs one tick of detection over two circles 0.9 m apart with a
// summed radius of 1 m, inserted in the order given, and hands back the entry.
// Which of the two ends up A is the Entity order, so the same pair is reachable
// both ways round.
func detectPair(t testing.TB, first, second Shape) Contact {
	t.Helper()
	contacts := NewContacts(5)
	bodies, statics := NewBodyIndex(0), NewStaticIndex(0)
	bodies.Insert(ecs.Entity(1), first, m.Vec2d{}, 0, nil)
	bodies.Insert(ecs.Entity(2), second, m.Vec2d{X: 0.9}, 0, nil)
	Collide(contacts, bodies, statics, noJoints, 3, testSlop)
	list := contacts.All()
	if len(list) != 1 {
		t.Fatalf("the two circles gave %d entries, want 1", len(list))
	}
	return list[0]
}

// withMaterial is a circle of radius 0.5 carrying one Friction and one
// Restitution.
func solverWithMaterial(friction, restitution float64) Shape {
	shape := NewCircleShape(0.5, m.Vec2d{})
	shape.Friction, shape.Restitution = friction, restitution
	return shape
}

func TestDetectCombinesThePairsMaterialAsCpsTwoPlainProducts(t *testing.T) {
	// cp's Update: arb.u = a.u * b.u and arb.e = a.e * b.e. Both are plain
	// products, so neither depends on which party ended up A — which is what the
	// second half of this asserts by handing detection the same pair the other
	// way round.
	rough := solverWithMaterial(0.8, 0.25)
	smooth := solverWithMaterial(0.5, 0.8)

	entry := detectPair(t, rough, smooth)
	if !solverNear(entry.Friction, 0.4) {
		t.Errorf("a pair of 0.8 and 0.5 has Friction %v, want their product 0.4", entry.Friction)
	}
	if !solverNear(entry.Restitution, 0.2) {
		t.Errorf("a pair of 0.25 and 0.8 has Restitution %v, want their product 0.2", entry.Restitution)
	}

	swapped := detectPair(t, smooth, rough)
	if swapped.Friction != entry.Friction || swapped.Restitution != entry.Restitution {
		t.Errorf("the same pair the other way round is %v/%v, want %v/%v",
			swapped.Friction, swapped.Restitution, entry.Friction, entry.Restitution)
	}
}

func TestOneSlipperyShapeSlidesThePairAndOneDeadShapeAbsorbsTheBounce(t *testing.T) {
	// The whole of what the product rule means to an app: a material is a
	// veto, not an average. Sand on rubber slides, and rubber on putty does not
	// bounce.
	slippery := detectPair(t, solverWithMaterial(0, 1), solverWithMaterial(1, 1))
	if slippery.Friction != 0 {
		t.Errorf("a frictionless Shape against a gripping one has Friction %v, want 0",
			slippery.Friction)
	}
	dead := detectPair(t, solverWithMaterial(1, 0), solverWithMaterial(1, 1))
	if dead.Restitution != 0 {
		t.Errorf("a dead Shape against a lively one has Restitution %v, want 0",
			dead.Restitution)
	}
}

func TestAShapeBuiltTheNormalWayCarriesCpsOwnDefaultsOfZero(t *testing.T) {
	// Defaults 0 and 0, matching cp, so a Shape nobody wrote a material onto
	// behaves exactly as it did before either was spent — which is what the
	// slide along a wall being v ← v − (v·n)·n rests on.
	for name, shape := range map[string]Shape{
		"a bare literal":  {Kind: ShapeCircle, Radius: 0.5},
		"NewCircleShape":  NewCircleShape(0.5, m.Vec2d{}),
		"NewSegmentShape": NewSegmentShape(m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0),
	} {
		if shape.Friction != 0 || shape.Restitution != 0 {
			t.Errorf("%s carries Friction %v and Restitution %v, want cp's 0 and 0",
				name, shape.Friction, shape.Restitution)
		}
	}
	entry := detectPair(t, NewCircleShape(0.5, m.Vec2d{}), NewCircleShape(0.5, m.Vec2d{}))
	if entry.Friction != 0 || entry.Restitution != 0 {
		t.Errorf("a pair of shipped Shapes is solved at %v/%v, want 0 and 0",
			entry.Friction, entry.Restitution)
	}
}

func TestNeitherMaterialIsValidatedAnywhere(t *testing.T) {
	// The range is cp's and cp validates neither, so the fields are plain and
	// there is no constructor to reject anything at. A Restitution above 1 adds
	// energy every bounce and a Friction above 1 grips harder than the pair
	// presses; both are legal, and both reach the entry as the product.
	entry := detectPair(t, solverWithMaterial(1.5, 1.25), solverWithMaterial(2, 0.8))
	if !solverNear(entry.Friction, 3) {
		t.Errorf("a pair of 1.5 and 2 has Friction %v, want 3", entry.Friction)
	}
	if !solverNear(entry.Restitution, 1) {
		t.Errorf("a pair of 1.25 and 0.8 has Restitution %v, want 1", entry.Restitution)
	}
}
