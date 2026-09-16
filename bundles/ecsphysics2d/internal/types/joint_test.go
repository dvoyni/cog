package types

import (
	"math"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// noJoints is the empty JointedPairs every Contact test hands Collide: those
// tests are about detection, and a scene with no Joint has no pair held apart.
// It is one value rather than a fresh set per call, so a benchmark's loop still
// allocates nothing.
var noJoints = NewJointedPairs()

// The two Entities every Joint built here holds. They are never resolved: this
// file is about the value, and the solver is tested through a real engine.
var (
	partyA = ecs.Entity(1)
	partyB = ecs.Entity(2)
)

// TestAJointIsOneHundredAndTwentyBytes pins the layout the specification lays
// out, because the whole argument for one kind-discriminated Component against
// ten Component types rests on it: two References, the kind and the collision
// flag, three solver constants, a 56-byte parameter union and the accumulated
// Impulse.
func TestAJointIsOneHundredAndTwentyBytes(t *testing.T) {
	if got := unsafe.Sizeof(Joint{}); got != 120 {
		t.Errorf("a Joint is %d bytes, want 120", got)
	}
	// The union is the widest kind's, which is the Spring: two anchors, a rest
	// length, a stiffness and an Absorption. Groove's normal is derived at
	// gather rather than stored, which is what keeps it at 56 rather than 64.
	if got := unsafe.Sizeof([7]float64{}); got != 56 {
		t.Errorf("the parameter union is %d bytes, want 56", got)
	}
}

// TestEveryJointConstructorFillsTheSolverDefaults pins what a Joint arrives
// with: cp's infinite MaxForce and MaxBias, the error bias re-spelled as a rate
// per second, and cp's own collideBodies of true.
func TestEveryJointConstructorFillsTheSolverDefaults(t *testing.T) {
	for _, it := range everyJointKind(t) {
		t.Run(it.name, func(t *testing.T) {
			if it.joint.Kind != it.kind {
				t.Errorf("the constructor built kind %v, want %v", it.joint.Kind, it.kind)
			}
			if it.joint.A != partyA || it.joint.B != partyB {
				t.Errorf("the References came out as %v and %v", it.joint.A, it.joint.B)
			}
			if !math.IsInf(it.joint.MaxForce, 1) {
				t.Errorf("MaxForce is %v, want an infinity", it.joint.MaxForce)
			}
			if !math.IsInf(it.joint.MaxBias, 1) {
				t.Errorf("MaxBias is %v, want an infinity", it.joint.MaxBias)
			}
			if it.joint.ErrorBias != DefaultJointErrorBias {
				t.Errorf("ErrorBias is %v, want %v", it.joint.ErrorBias, DefaultJointErrorBias)
			}
			if !it.joint.CollideBodies {
				t.Error("a Joint arrives with its two Bodies passing through each other")
			}
			if it.joint.Impulse() != 0 {
				t.Errorf("a Joint arrives carrying an Impulse of %v", it.joint.Impulse())
			}
		})
	}
}

// TestEachKindReadsBackTheParametersItWasBuiltWith is the type safety the
// kind-discriminated union gives back: per-kind constructors and per-kind
// accessors, exactly as Shape's.
func TestEachKindReadsBackTheParametersItWasBuiltWith(t *testing.T) {
	anchorA := m.Vec2d{X: 0.2, Y: -0.35}
	anchorB := m.Vec2d{X: -0.15, Y: 0.45}

	pin := NewPinJoint(partyA, partyB, anchorA, anchorB, 1.25)
	gotA, gotB := pin.Anchors()
	if gotA != anchorA || gotB != anchorB || pin.Distance() != 1.25 {
		t.Errorf("the pin read back %v, %v, %v", gotA, gotB, pin.Distance())
	}

	slide := NewSlideJoint(partyA, partyB, anchorA, anchorB, 0.4, 0.9)
	if low, high := slide.Span(); low != 0.4 || high != 0.9 {
		t.Errorf("the slide's span read back %v to %v", low, high)
	}

	pivot := NewPivotJoint(partyA, partyB, anchorA, anchorB)
	if gotA, gotB := pivot.Anchors(); gotA != anchorA || gotB != anchorB {
		t.Errorf("the pivot read back %v, %v", gotA, gotB)
	}

	start, end := m.Vec2d{X: -0.5, Y: 0.1}, m.Vec2d{X: 0.7, Y: 0.25}
	groove := NewGrooveJoint(partyA, partyB, anchorA, start, end)
	gotStart, gotEnd := groove.Groove()
	if groove.GrooveAnchor() != anchorA || gotStart != start || gotEnd != end {
		t.Errorf("the groove read back %v, %v, %v", groove.GrooveAnchor(), gotStart, gotEnd)
	}

	spring := NewSpringJoint(partyA, partyB, anchorA, anchorB, 0.8, 14, 1.7)
	if spring.RestLength() != 0.8 || spring.Stiffness() != 14 || spring.Absorption() != 1.7 {
		t.Errorf("the Spring read back %v, %v, %v",
			spring.RestLength(), spring.Stiffness(), spring.Absorption())
	}

	rotary := NewRotarySpringJoint(partyA, partyB, 0.35, 9, 1.1)
	if rotary.RestAngle() != 0.35 || rotary.Stiffness() != 9 || rotary.Absorption() != 1.1 {
		t.Errorf("the rotary Spring read back %v, %v, %v",
			rotary.RestAngle(), rotary.Stiffness(), rotary.Absorption())
	}

	limit := NewRotaryLimitJoint(partyA, partyB, -0.2, 0.3)
	if low, high := limit.Span(); low != -0.2 || high != 0.3 {
		t.Errorf("the rotary limit's span read back %v to %v", low, high)
	}

	ratchet := NewRatchetJoint(partyA, partyB, 0.65, 0.05, 0.4)
	if ratchet.Angle() != 0.65 || ratchet.Phase() != 0.05 || ratchet.Ratchet() != 0.4 {
		t.Errorf("the ratchet read back %v, %v, %v",
			ratchet.Angle(), ratchet.Phase(), ratchet.Ratchet())
	}

	gear := NewGearJoint(partyA, partyB, 0.12, 2.5)
	if gear.Phase() != 0.12 || gear.Ratio() != 2.5 {
		t.Errorf("the gear read back %v, %v", gear.Phase(), gear.Ratio())
	}

	motor := NewMotorJoint(partyA, partyB, 3.2)
	if motor.Rate() != 3.2 {
		t.Errorf("the motor read back %v", motor.Rate())
	}
}

// TestEverySetterWritesThroughTheSameSlotItsAccessorReads is what keeps a
// runtime change — a motor's rate, a slide's span — from silently landing on
// another kind's parameter.
func TestEverySetterWritesThroughTheSameSlotItsAccessorReads(t *testing.T) {
	anchorA := m.Vec2d{X: 1, Y: 2}
	anchorB := m.Vec2d{X: 3, Y: 4}

	pin := NewPinJoint(partyA, partyB, m.Vec2d{}, m.Vec2d{}, 0)
	pin.SetAnchors(anchorA, anchorB)
	pin.SetDistance(2.5)
	if gotA, gotB := pin.Anchors(); gotA != anchorA || gotB != anchorB || pin.Distance() != 2.5 {
		t.Errorf("the pin's setters wrote %v, %v, %v", gotA, gotB, pin.Distance())
	}

	groove := NewGrooveJoint(partyA, partyB, m.Vec2d{}, m.Vec2d{}, m.Vec2d{})
	groove.SetGrooveAnchor(anchorA)
	groove.SetGroove(anchorB, anchorA)
	gotStart, gotEnd := groove.Groove()
	if groove.GrooveAnchor() != anchorA || gotStart != anchorB || gotEnd != anchorA {
		t.Errorf("the groove's setters wrote %v, %v, %v", groove.GrooveAnchor(), gotStart, gotEnd)
	}

	spring := NewSpringJoint(partyA, partyB, m.Vec2d{}, m.Vec2d{}, 0, 0, 0)
	spring.SetRestLength(1.5)
	spring.SetStiffness(20)
	spring.SetAbsorption(3)
	if spring.RestLength() != 1.5 || spring.Stiffness() != 20 || spring.Absorption() != 3 {
		t.Errorf("the Spring's setters wrote %v, %v, %v",
			spring.RestLength(), spring.Stiffness(), spring.Absorption())
	}

	rotary := NewRotarySpringJoint(partyA, partyB, 0, 0, 0)
	rotary.SetRestAngle(0.75)
	if rotary.RestAngle() != 0.75 {
		t.Errorf("the rotary Spring's setter wrote %v", rotary.RestAngle())
	}

	limit := NewRotaryLimitJoint(partyA, partyB, 0, 0)
	limit.SetSpan(-1, 1)
	if low, high := limit.Span(); low != -1 || high != 1 {
		t.Errorf("the rotary limit's setter wrote %v to %v", low, high)
	}

	ratchet := NewRatchetJoint(partyA, partyB, 0, 0, 0)
	ratchet.SetAngle(0.9)
	ratchet.SetPhase(0.1)
	ratchet.SetRatchet(0.5)
	if ratchet.Angle() != 0.9 || ratchet.Phase() != 0.1 || ratchet.Ratchet() != 0.5 {
		t.Errorf("the ratchet's setters wrote %v, %v, %v",
			ratchet.Angle(), ratchet.Phase(), ratchet.Ratchet())
	}

	gear := NewGearJoint(partyA, partyB, 0, 0)
	gear.SetPhase(0.2)
	gear.SetRatio(4)
	if gear.Phase() != 0.2 || gear.Ratio() != 4 {
		t.Errorf("the gear's setters wrote %v, %v", gear.Phase(), gear.Ratio())
	}

	motor := NewMotorJoint(partyA, partyB, 0)
	motor.SetRate(-1.5)
	if motor.Rate() != -1.5 {
		t.Errorf("the motor's setter wrote %v", motor.Rate())
	}
}

// TestTheImpulseIsReadTheWayChipmunkReadsItKindByKind pins cp's own GetImpulse:
// a magnitude for the eight kinds that clamp against MaxForce, the length of
// the two-component Impulse for the pivot and the groove, and a signed number
// for the two Springs.
func TestTheImpulseIsReadTheWayChipmunkReadsItKindByKind(t *testing.T) {
	for _, it := range everyJointKind(t) {
		t.Run(it.name, func(t *testing.T) {
			joint := it.joint
			joint.impulse = m.Vec2d{X: -3, Y: 4}
			got := joint.Impulse()
			switch it.kind {
			case JointPivot, JointGroove:
				if got != 5 {
					t.Errorf("the two-component Impulse read back %v, want its length 5", got)
				}
			case JointSpring, JointRotarySpring:
				if got != -3 {
					t.Errorf("the Spring's Impulse read back %v, want the signed -3", got)
				}
			default:
				if got != 3 {
					t.Errorf("the Impulse read back %v, want the magnitude 3", got)
				}
			}
		})
	}
}

// TestJointedPairsHoldsAnUnorderedPairAndTakesADuplicateOnce is the set Index
// builds and Detect checks. It is unordered because which party is A is
// detection's choice, and it takes a duplicate because two Joints between the
// same two Bodies are an ordinary thing to spawn.
func TestJointedPairsHoldsAnUnorderedPairAndTakesADuplicateOnce(t *testing.T) {
	pairs := NewJointedPairs()
	if pairs.Len() != 0 || pairs.Has(partyA, partyB) {
		t.Fatal("a fresh set is not empty")
	}

	pairs.Add(partyA, partyB)
	if pairs.Len() != 1 {
		t.Errorf("one pair made the set %d long", pairs.Len())
	}
	if !pairs.Has(partyA, partyB) || !pairs.Has(partyB, partyA) {
		t.Error("the pair is not found both ways round")
	}

	pairs.Add(partyB, partyA)
	if pairs.Len() != 1 {
		t.Errorf("the same pair the other way round made the set %d long", pairs.Len())
	}

	if pairs.Has(partyA, ecs.Entity(99)) {
		t.Error("a pair nothing added is in the set")
	}

	pairs.Clear()
	if pairs.Len() != 0 || pairs.Has(partyA, partyB) {
		t.Error("the cleared set still holds a pair")
	}
}

// TestJointedPairsGrowsWithoutLosingAPair is the doubling the table does when
// it is half full, which a scene whose Joint count climbs walks straight into.
func TestJointedPairsGrowsWithoutLosingAPair(t *testing.T) {
	pairs := NewJointedPairs()
	const count = 500
	for i := range count {
		pairs.Add(ecs.Entity(2*i+1), ecs.Entity(2*i+2))
	}
	if pairs.Len() != count {
		t.Fatalf("the set holds %d pairs, want %d", pairs.Len(), count)
	}
	for i := range count {
		if !pairs.Has(ecs.Entity(2*i+2), ecs.Entity(2*i+1)) {
			t.Fatalf("the pair %d was lost across a growth", i)
		}
	}
}

// TestPinDistanceIsTheDistanceBetweenTheTwoAnchorsInTheWorld is the pure helper
// beside cp's constructor, which reads the two Bodies' transforms where a cog
// constructor cannot.
func TestPinDistanceIsTheDistanceBetweenTheTwoAnchorsInTheWorld(t *testing.T) {
	// Both anchors turned onto the same world point: the distance is zero.
	posA, angleA := m.Vec2d{X: 1, Y: 0}, math.Pi/2
	posB, angleB := m.Vec2d{X: 1, Y: 1}, 0.0
	anchorA := m.Vec2d{X: 1, Y: 0} // turns to (0, 1), landing on (1, 1)
	anchorB := m.Vec2d{}
	if got := PinDistance(posA, angleA, anchorA, posB, angleB, anchorB); math.Abs(got) > 1e-12 {
		t.Errorf("two anchors on the same point are %v apart", got)
	}

	// And a plain case, with neither Body turned.
	if got := PinDistance(
		m.Vec2d{}, 0, m.Vec2d{X: 0.5},
		m.Vec2d{X: 3}, 0, m.Vec2d{X: -0.5},
	); math.Abs(got-2) > 1e-12 {
		t.Errorf("the distance came out %v, want 2", got)
	}
}

// TestPivotAnchorsSplitsOneWorldPivotIntoTwoLocalAnchors is the other pure
// helper: cp calls WorldToLocal on both Bodies, and this is that over values.
func TestPivotAnchorsSplitsOneWorldPivotIntoTwoLocalAnchors(t *testing.T) {
	posA, angleA := m.Vec2d{X: 2, Y: 1}, 0.7
	posB, angleB := m.Vec2d{X: -1, Y: 4}, -1.2
	pivot := m.Vec2d{X: 0.25, Y: 2.5}

	anchorA, anchorB := PivotAnchors(posA, angleA, posB, angleB, pivot)

	// Turning each anchor back into the world lands on the pivot again.
	backA := NewTransformRigid(posA, angleA).Point(anchorA)
	backB := NewTransformRigid(posB, angleB).Point(anchorB)
	if backA.Distance(pivot) > 1e-12 {
		t.Errorf("A's anchor turns back to %v, want %v", backA, pivot)
	}
	if backB.Distance(pivot) > 1e-12 {
		t.Errorf("B's anchor turns back to %v, want %v", backB, pivot)
	}
}

// TestClampLengthIsChipmunksVectorClamp pins the one vector helper the pivot
// and the groove need that the m package does not carry, including the
// infinite ceiling every Joint arrives with.
func TestClampLengthIsChipmunksVectorClamp(t *testing.T) {
	v := m.Vec2d{X: 3, Y: 4}
	if got := clampLength(v, math.Inf(1)); got != v {
		t.Errorf("an infinite ceiling changed %v into %v", v, got)
	}
	if got := clampLength(v, 10); got != v {
		t.Errorf("a ceiling above the length changed %v into %v", v, got)
	}
	got := clampLength(v, 2.5)
	if math.Abs(got.Length()-2.5) > 1e-12 {
		t.Errorf("clamping to 2.5 gave a vector of length %v", got.Length())
	}
	if math.Abs(got.X-1.5) > 1e-12 || math.Abs(got.Y-2) > 1e-12 {
		t.Errorf("clamping to 2.5 turned the vector: %v", got)
	}
}

// TestTheEntityTableFindsEveryRowItWasGiven is the map that answers for the
// jointed Bodies detection never saw, and the one place a Joint pays a hash.
func TestTheEntityTableFindsEveryRowItWasGiven(t *testing.T) {
	var table entityTable
	table.reset()
	if _, ok := table.lookup(partyA); ok {
		t.Fatal("a fresh table found a row")
	}

	const count = 300
	for i := range count {
		table.put(ecs.Entity(i+1), int32(i+1))
	}
	for i := range count {
		at, ok := table.lookup(ecs.Entity(i + 1))
		if !ok || at != int32(i+1) {
			t.Fatalf("%d read back as %d, %v", i+1, at, ok)
		}
	}
	if _, ok := table.lookup(ecs.Entity(count + 1)); ok {
		t.Error("the table found a row nothing put there")
	}

	// Reset keeps the memory and forgets the rows, which is what makes the
	// per-tick rebuild allocate nothing.
	table.reset()
	if table.used != 0 {
		t.Errorf("the reset table still holds %d rows", table.used)
	}
	if _, ok := table.lookup(ecs.Entity(1)); ok {
		t.Error("the reset table still finds a row")
	}
}

// TestTheJointedPairSetAllocatesNothingOnceItHasGrown is the zero-allocation
// rule on the one structure Index rebuilds every tick.
func TestTheJointedPairSetAllocatesNothingOnceItHasGrown(t *testing.T) {
	pairs := NewJointedPairs()
	fill := func() {
		pairs.Clear()
		for i := range 256 {
			pairs.Add(ecs.Entity(2*i+1), ecs.Entity(2*i+2))
		}
	}
	fill()
	if got := testing.AllocsPerRun(50, fill); got != 0 {
		t.Errorf("rebuilding the set allocates %v objects a tick, want none", got)
	}
}

type jointKindCase struct {
	name  string
	kind  JointKind
	joint Joint
}

// everyJointKind is one Joint of each of the ten kinds, built through its own
// constructor, so that a test that must cover all ten cannot quietly cover
// nine.
func everyJointKind(t *testing.T) []jointKindCase {
	t.Helper()
	anchorA := m.Vec2d{X: 0.2, Y: -0.35}
	anchorB := m.Vec2d{X: -0.15, Y: 0.45}
	cases := []jointKindCase{
		{"pin", JointPin, NewPinJoint(partyA, partyB, anchorA, anchorB, 1.25)},
		{"slide", JointSlide, NewSlideJoint(partyA, partyB, anchorA, anchorB, 0.4, 0.9)},
		{"pivot", JointPivot, NewPivotJoint(partyA, partyB, anchorA, anchorB)},
		{"groove", JointGroove, NewGrooveJoint(partyA, partyB, anchorA, anchorB, anchorA)},
		{"spring", JointSpring, NewSpringJoint(partyA, partyB, anchorA, anchorB, 0.8, 14, 1.7)},
		{"rotary spring", JointRotarySpring, NewRotarySpringJoint(partyA, partyB, 0.35, 9, 1.1)},
		{"rotary limit", JointRotaryLimit, NewRotaryLimitJoint(partyA, partyB, -0.2, 0.3)},
		{"ratchet", JointRatchet, NewRatchetJoint(partyA, partyB, 0.65, 0.05, 0.4)},
		{"gear", JointGear, NewGearJoint(partyA, partyB, 0.12, 2.5)},
		{"motor", JointMotor, NewMotorJoint(partyA, partyB, 3.2)},
	}
	if len(cases) != int(JointMotor)+1 {
		t.Fatalf("there are %d kinds and this table covers %d", int(JointMotor)+1, len(cases))
	}
	return cases
}
