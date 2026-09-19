package types

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// JointKind is which of cp's ten constraints a Joint is, and it names which of
// the seven parameter slots mean anything. A kind contradicting its parameters
// cannot be spelled, because the slots are unexported and every one of the ten
// constructors fills its own.
//
// Ten Component types were put and rejected: a simple motor would be 64 bytes
// instead of 120 and an app could Query one kind directly, but it would cost
// ten Stores, ten Queries inside Solve, ten lock entries, and a Joint's kind
// becoming a structural change instead of a Component write. Shape already made
// this trade once and the port stays consistent with it.
type JointKind uint8

const (
	// JointPin holds its two anchors at a fixed distance, which is cp's
	// PinJoint.
	JointPin JointKind = iota
	// JointSlide holds its two anchors within a range of distances, which is
	// cp's SlideJoint. Inside the range it holds nothing.
	JointSlide
	// JointPivot holds its two anchors at the same point, which is cp's
	// PivotJoint: the hinge two limbs turn about.
	JointPivot
	// JointGroove holds A's anchor on a line segment carried by B, which is
	// cp's GrooveJoint.
	JointGroove
	// JointSpring pushes its two anchors towards a rest distance and settles by
	// its Absorption, which is cp's DampedSpring. It is a Spring: it only
	// pushes, and anything else acting on the two Bodies can win against it.
	JointSpring
	// JointRotarySpring pushes the two Bodies towards a rest Angle between
	// them, which is cp's DampedRotarySpring.
	JointRotarySpring
	// JointRotaryLimit holds the Angle between the two Bodies within a range,
	// which is cp's RotaryLimitJoint.
	JointRotaryLimit
	// JointRatchet lets the Angle between the two Bodies turn one way and
	// clicks over in steps, which is cp's RatchetJoint.
	JointRatchet
	// JointGear holds the two Bodies turning in step at a ratio, which is cp's
	// GearJoint.
	JointGear
	// JointMotor turns the two Bodies against each other at a rate, which is
	// cp's SimpleMotor.
	JointMotor
)

// DefaultJointErrorBias is the share of positional error a Joint leaves after
// one second, re-spelled as a rate per second: byte for byte cp's own
// errorBias, whose stored 0.9⁶⁰ is the same quantity written as a share.
//
// It is spelled here rather than read out of the settings struct because a cog
// constructor is a free function over values with no engine state to reach, and
// the three per-Joint solver constants have to have a value the moment a Joint
// is built. The settings struct records the same defaults; nothing at solve
// time substitutes them, because a Joint carries its own.
const DefaultJointErrorBias = 6.32

// The seven parameter slots, by the kinds that use them. Nothing but the
// constructors and the accessors below reads these names, and they are what
// keeps the union at 56 bytes rather than 64: groove's normal is derived at
// gather instead of stored, which is 8 bytes off every Joint in the world.
const (
	// slots 0 and 1: A's anchor, local to A's centre of gravity — and, for the
	// groove, the one anchor, which the groove carried by B holds.
	jointAnchorAX, jointAnchorAY = 0, 1
	// slots 2 and 3: B's anchor — and, for the groove, the groove's first end,
	// local to B.
	jointAnchorBX, jointAnchorBY = 2, 3
	// slots 4 and 5: the groove's second end, local to B.
	jointGrooveEndX, jointGrooveEndY = 4, 5
	// slot 4: the pin's distance, the slide's and the rotary limit's minimum,
	// the Spring's rest length, the rotary Spring's rest Angle, the ratchet's
	// Angle.
	jointFirst = 4
	// slot 5: the slide's and the rotary limit's maximum, either Spring's
	// stiffness, the ratchet's and the gear's phase.
	jointSecond = 5
	// slot 6: either Spring's Absorption, the ratchet's step, the gear's ratio,
	// the motor's rate.
	jointThird = 6
)

// Joint is one of cp's ten constraints as a single kind-discriminated value of
// 120 bytes, carried by an Entity of its own.
//
// It is a separate Entity rather than a Component on one of the Bodies because
// a Body can carry several Joints and a Component is one per Entity. This is
// the first place the Reference vocabulary is load-bearing: References arrive
// with Joints, between Bodies, never between a Body and its Shape.
//
//	| A, B References                              | 16 |
//	| kind, CollideBodies and padding              |  8 |
//	| MaxForce, ErrorBias, MaxBias                 | 24 |
//	| the parameter union, widest at the Spring    | 56 |
//	| the accumulated Impulse                      | 16 |
//	|                                              |120 |
//
// cp's a and b are this port's B and A, as they are everywhere else in the
// solver: cp's apply_impulses adds the impulse to its second Body, so the party
// the Impulse is applied to is A — which is what makes Impulse read the way
// Contact.TotalImpulse reads. Every sign in the ported arithmetic follows from
// that one substitution and nothing else changes. The one place it is visible
// from outside is the groove, whose line is carried by B and whose anchor is on
// A, where cp has them the other way round.
//
// The parameters are unexported because they carry an invariant with Kind, as
// Shape's vertices do; everything else is a plain field, so the three solver
// constants and the collision flag are data the app writes. A Joint written as
// a bare literal is a pin Joint of zero length with no positional correction
// and its two Bodies passing through each other — the same trap a bare Shape
// literal is, and stated here rather than checked.
//
// Only two things cross a tick: the accumulated Impulse, which warm starting
// spends, and the ratchet's Angle, which cp mutates as the ratchet clicks over.
// Every one of cp's PreStep scratch fields — r1, r2, k, nMass, the bias, the
// groove normal, the clamp, targetVrn and vCoef — is frame-local in the dense
// Joint row Solve throws away, because a Joint Component is app-owned data the
// app writes and reads, where a Contact entry is solver-owned data inside a
// Resource nothing else writes.
type Joint struct {
	// A and B are the two Bodies the Joint holds. A Reference that resolves to
	// nothing — the Body was despawned — makes Solve skip the Joint and zero
	// its Impulse; the plugin does not despawn the Joint, structural change
	// during the step being forbidden, and a dangling Reference is the app's to
	// clean up. Impulse reading 0 is how an app notices. cp asserts here.
	A, B ecs.Entity

	params [7]float64

	// MaxForce is the ceiling on the force the Joint may apply, in newtons, and
	// is used as MaxForce·dt: an Impulse ceiling. It is how a breakable or a
	// limited Joint is expressed. Its default is an infinity, which is cp's.
	MaxForce float64
	// ErrorBias is the rate per second at which the Joint pushes out the
	// positional error it finds, which is cp's errorBias re-spelled the way the
	// collision bias is. Its default is DefaultJointErrorBias.
	ErrorBias float64
	// MaxBias is the ceiling on that correction, in metres per second — radians
	// per second for the five angular kinds — and is how cp makes a Joint soft.
	// Its default is an infinity, which is cp's.
	MaxBias float64

	impulse m.Vec2d

	// Kind is which of the ten constraints this Joint is.
	Kind JointKind
	// CollideBodies says whether the two Bodies still collide with each other.
	// False is what makes a jointed figure of limbs possible: Index puts the
	// pair into JointedPairs and Detect never creates the Contact and never
	// reports it, which is cp's own semantics, its QueryReject meaning no
	// arbiter and therefore no Begin.
	CollideBodies bool

	_ [6]byte
}

// newJoint is the Constraint every kind starts from: cp's NewConstraint, with
// its three defaults re-spelled and its collideBodies default kept.
func newJoint(kind JointKind, a, b ecs.Entity) Joint {
	return Joint{
		A:             a,
		B:             b,
		MaxForce:      math.Inf(1),
		ErrorBias:     DefaultJointErrorBias,
		MaxBias:       math.Inf(1),
		Kind:          kind,
		CollideBodies: true,
	}
}

// NewPinJoint holds A's anchor and B's anchor at a fixed distance, which is
// cp's NewPinJoint. Both anchors are local to their Body's centre of gravity.
//
// cp's constructor reads both Bodies' transforms to work the distance out;
// a cog constructor is a free function over values with no Store to reach, so
// PinDistance is the pure helper beside this one and the app passes what it
// returns. At a distance of 0 cp warns that a pin is unstable and a pivot is
// what is wanted.
func NewPinJoint(a, b ecs.Entity, anchorA, anchorB m.Vec2d, distance float64) Joint {
	joint := newJoint(JointPin, a, b)
	joint.setAnchors(anchorA, anchorB)
	joint.params[jointFirst] = distance
	return joint
}

// PinDistance is the distance cp's NewPinJoint reads off the two Bodies: the
// distance between the two anchors as they stand in the world. The app has all
// six arguments in hand at spawn time.
func PinDistance(
	posA m.Vec2d, angleA float64, anchorA m.Vec2d,
	posB m.Vec2d, angleB float64, anchorB m.Vec2d,
) float64 {
	worldA := NewTransformRigid(posA, angleA).Point(anchorA)
	worldB := NewTransformRigid(posB, angleB).Point(anchorB)
	return worldA.Sub(worldB).Length()
}

// NewSlideJoint holds A's anchor and B's anchor within a range of distances,
// which is cp's NewSlideJoint. Inside the range the Joint holds nothing at all
// and its Impulse is zero.
func NewSlideJoint(a, b ecs.Entity, anchorA, anchorB m.Vec2d, minimum, maximum float64) Joint {
	joint := newJoint(JointSlide, a, b)
	joint.setAnchors(anchorA, anchorB)
	joint.params[jointFirst], joint.params[jointSecond] = minimum, maximum
	return joint
}

// NewPivotJoint holds A's anchor and B's anchor at the same point, which is
// cp's NewPivotJoint2 — the hinge a jointed figure of limbs turns about.
//
// cp's other constructor takes one world pivot and calls WorldToLocal on both
// Bodies. PivotAnchors is the pure helper that does that split over values.
func NewPivotJoint(a, b ecs.Entity, anchorA, anchorB m.Vec2d) Joint {
	joint := newJoint(JointPivot, a, b)
	joint.setAnchors(anchorA, anchorB)
	return joint
}

// PivotAnchors splits one world pivot into the two local anchors cp's
// NewPivotJoint derives with WorldToLocal, which a cog constructor cannot do
// for itself.
func PivotAnchors(
	posA m.Vec2d, angleA float64,
	posB m.Vec2d, angleB float64,
	worldPivot m.Vec2d,
) (anchorA, anchorB m.Vec2d) {
	anchorA = NewTransformRigidInverse(NewTransformRigid(posA, angleA)).Point(worldPivot)
	anchorB = NewTransformRigidInverse(NewTransformRigid(posB, angleB)).Point(worldPivot)
	return anchorA, anchorB
}

// NewGrooveJoint holds A's anchor on the line segment from start to end carried
// by B, which is cp's NewGrooveJoint.
//
// cp carries the groove on its first Body and the anchor on its second; this
// port's A is cp's b, so the groove is B's and the anchor is A's. It is the one
// place the substitution is visible from outside the solver.
//
// The groove's normal is not stored. cp caches it in its constructor and never
// changes it, so the port derives it at gather for one normalize a tick, which
// is what keeps the parameter union at 56 bytes rather than 64.
func NewGrooveJoint(a, b ecs.Entity, anchorA, start, end m.Vec2d) Joint {
	joint := newJoint(JointGroove, a, b)
	joint.params[jointAnchorAX], joint.params[jointAnchorAY] = anchorA.X, anchorA.Y
	joint.params[jointAnchorBX], joint.params[jointAnchorBY] = start.X, start.Y
	joint.params[jointGrooveEndX], joint.params[jointGrooveEndY] = end.X, end.Y
	return joint
}

// NewSpringJoint pushes A's anchor and B's anchor towards a rest length,
// harder the further they are from it, settling by its Absorption. It is cp's
// NewDampedSpring, and it is the one kind of Joint that holds nothing: it only
// pushes, so anything else acting on the two Bodies can win against it.
//
// stiffness is in newtons per metre and absorption in newton-seconds per metre.
// Absorption is named apart from a Body's Damping, a rate in 1/s, and from a
// Contact's Friction, a ratio: three different things.
//
// cp's SpringForceFunc is not ported — a Component holds no pointers,
// transitively, enforced at registration — so this is cp's linear law only,
// (RestLength − dist)·Stiffness. An app wanting a nonlinear Spring writes Force
// from its own System, which is nearly the same computation. The two differ in
// one way, recorded rather than corrected: cp applies the Spring as an Impulse
// inside PreStep, before velocity integration, so it is multiplied by the
// Body's Damping in the same tick, where a Force write is added after that
// multiply. At 15 /s and 60 Hz the Spring loses 22% of its Impulse in the tick
// it is applied. That is cp's behaviour and the port inherits it.
func NewSpringJoint(
	a, b ecs.Entity, anchorA, anchorB m.Vec2d,
	restLength, stiffness, absorption float64,
) Joint {
	joint := newJoint(JointSpring, a, b)
	joint.setAnchors(anchorA, anchorB)
	joint.params[jointFirst] = restLength
	joint.params[jointSecond] = stiffness
	joint.params[jointThird] = absorption
	return joint
}

// NewRotarySpringJoint pushes the two Bodies towards a rest Angle between them,
// settling by its Absorption. It is cp's NewDampedRotarySpring, with the same
// dropped force hook and the same linear law,
// (relativeAngle − RestAngle)·Stiffness, where the relative Angle is B's less
// A's. stiffness is in newton-metres per radian.
func NewRotarySpringJoint(a, b ecs.Entity, restAngle, stiffness, absorption float64) Joint {
	joint := newJoint(JointRotarySpring, a, b)
	joint.params[jointFirst] = restAngle
	joint.params[jointSecond] = stiffness
	joint.params[jointThird] = absorption
	return joint
}

// NewRotaryLimitJoint holds the Angle of A relative to B within a range of
// radians, which is cp's NewRotaryLimitJoint. Inside the range it holds nothing
// and its Impulse is zero.
func NewRotaryLimitJoint(a, b ecs.Entity, minimum, maximum float64) Joint {
	joint := newJoint(JointRotaryLimit, a, b)
	joint.params[jointFirst], joint.params[jointSecond] = minimum, maximum
	return joint
}

// NewRatchetJoint lets the Angle of A relative to B turn one way and clicks it
// over in steps of ratchet radians, offset by phase. It is cp's
// NewRatchetJoint.
//
// angle is the Angle the ratchet starts at, which cp reads off the two Bodies
// as b.a − a.a: here, A's Angle less B's. The app passes it, as it passes the
// pin's distance, because a cog constructor reads no Store.
//
// A ratchet of 0 is refused by the solver rather than by this constructor: it
// would divide by zero. The Joint is skipped and its Impulse reads 0.
func NewRatchetJoint(a, b ecs.Entity, angle, phase, ratchet float64) Joint {
	joint := newJoint(JointRatchet, a, b)
	joint.params[jointFirst] = angle
	joint.params[jointSecond] = phase
	joint.params[jointThird] = ratchet
	return joint
}

// NewGearJoint holds the two Bodies turning in step, A's Angle times the ratio
// less B's Angle held at the phase. It is cp's NewGearJoint.
//
// A ratio of 0 is refused by the solver rather than here, for the same reason
// the ratchet's step of 0 is: cp inverts the ratio and would divide by zero.
func NewGearJoint(a, b ecs.Entity, phase, ratio float64) Joint {
	joint := newJoint(JointGear, a, b)
	joint.params[jointSecond] = phase
	joint.params[jointThird] = ratio
	return joint
}

// NewMotorJoint turns the two Bodies against each other, driving A's Angular
// velocity less B's towards the negation of rate. It is cp's NewSimpleMotor,
// whose sign convention this keeps.
func NewMotorJoint(a, b ecs.Entity, rate float64) Joint {
	joint := newJoint(JointMotor, a, b)
	joint.params[jointThird] = rate
	return joint
}

func (j *Joint) setAnchors(anchorA, anchorB m.Vec2d) {
	j.params[jointAnchorAX], j.params[jointAnchorAY] = anchorA.X, anchorA.Y
	j.params[jointAnchorBX], j.params[jointAnchorBY] = anchorB.X, anchorB.Y
}

// Anchors are the two anchors, A's and B's, each local to its own Body's centre
// of gravity. They are meaningful on the pin, the slide, the pivot and the
// Spring; the groove has GrooveAnchor and Groove instead.
func (j Joint) Anchors() (anchorA, anchorB m.Vec2d) {
	return m.Vec2d{X: j.params[jointAnchorAX], Y: j.params[jointAnchorAY]},
		m.Vec2d{X: j.params[jointAnchorBX], Y: j.params[jointAnchorBY]}
}

// SetAnchors replaces both anchors.
func (j *Joint) SetAnchors(anchorA, anchorB m.Vec2d) { j.setAnchors(anchorA, anchorB) }

// GrooveAnchor is the groove Joint's one anchor, local to A.
func (j Joint) GrooveAnchor() m.Vec2d {
	return m.Vec2d{X: j.params[jointAnchorAX], Y: j.params[jointAnchorAY]}
}

// SetGrooveAnchor replaces the groove Joint's anchor.
func (j *Joint) SetGrooveAnchor(anchor m.Vec2d) {
	j.params[jointAnchorAX], j.params[jointAnchorAY] = anchor.X, anchor.Y
}

// Groove is the groove's two ends, both local to B.
func (j Joint) Groove() (start, end m.Vec2d) {
	return m.Vec2d{X: j.params[jointAnchorBX], Y: j.params[jointAnchorBY]},
		m.Vec2d{X: j.params[jointGrooveEndX], Y: j.params[jointGrooveEndY]}
}

// SetGroove replaces the groove's two ends.
func (j *Joint) SetGroove(start, end m.Vec2d) {
	j.params[jointAnchorBX], j.params[jointAnchorBY] = start.X, start.Y
	j.params[jointGrooveEndX], j.params[jointGrooveEndY] = end.X, end.Y
}

// Distance is the pin Joint's fixed distance in metres.
func (j Joint) Distance() float64 { return j.params[jointFirst] }

// SetDistance replaces the pin Joint's distance.
func (j *Joint) SetDistance(distance float64) { j.params[jointFirst] = distance }

// Span is the slide Joint's range of distances in metres, and the rotary limit
// Joint's range of Angles in radians.
func (j Joint) Span() (minimum, maximum float64) {
	return j.params[jointFirst], j.params[jointSecond]
}

// SetSpan replaces that range.
func (j *Joint) SetSpan(minimum, maximum float64) {
	j.params[jointFirst], j.params[jointSecond] = minimum, maximum
}

// RestLength is the Spring's rest distance in metres.
func (j Joint) RestLength() float64 { return j.params[jointFirst] }

// SetRestLength replaces the Spring's rest distance.
func (j *Joint) SetRestLength(length float64) { j.params[jointFirst] = length }

// RestAngle is the rotary Spring's rest Angle in radians.
func (j Joint) RestAngle() float64 { return j.params[jointFirst] }

// SetRestAngle replaces the rotary Spring's rest Angle.
func (j *Joint) SetRestAngle(angle float64) { j.params[jointFirst] = angle }

// Stiffness is the Spring's, in N/m, or the rotary Spring's, in N·m/rad.
func (j Joint) Stiffness() float64 { return j.params[jointSecond] }

// SetStiffness replaces the Spring's stiffness.
func (j *Joint) SetStiffness(stiffness float64) { j.params[jointSecond] = stiffness }

// Absorption is how strongly the Spring resists its two ends moving apart or
// together, in N·s/m. It is named apart from a Body's Damping and a Contact's
// Friction because it is a third thing: a force per unit of that speed.
func (j Joint) Absorption() float64 { return j.params[jointThird] }

// SetAbsorption replaces the Spring's Absorption.
func (j *Joint) SetAbsorption(absorption float64) { j.params[jointThird] = absorption }

// Angle is the ratchet's current Angle in radians, which the solver writes as
// the ratchet clicks over. It is the one parameter the plugin mutates, and it
// is public because a ratchet's position is gameplay-visible by nature.
func (j Joint) Angle() float64 { return j.params[jointFirst] }

// SetAngle replaces the ratchet's Angle.
func (j *Joint) SetAngle(angle float64) { j.params[jointFirst] = angle }

// Phase is the ratchet's and the gear's offset in radians.
func (j Joint) Phase() float64 { return j.params[jointSecond] }

// SetPhase replaces that offset.
func (j *Joint) SetPhase(phase float64) { j.params[jointSecond] = phase }

// Ratchet is the ratchet Joint's step in radians.
func (j Joint) Ratchet() float64 { return j.params[jointThird] }

// SetRatchet replaces the ratchet's step.
func (j *Joint) SetRatchet(ratchet float64) { j.params[jointThird] = ratchet }

// Ratio is the gear Joint's ratio.
func (j Joint) Ratio() float64 { return j.params[jointThird] }

// SetRatio replaces the gear's ratio.
func (j *Joint) SetRatio(ratio float64) { j.params[jointThird] = ratio }

// Rate is the motor's rate in radians per second.
func (j Joint) Rate() float64 { return j.params[jointThird] }

// SetRate replaces the motor's rate.
func (j *Joint) SetRate(rate float64) { j.params[jointThird] = rate }

// Impulse is what the Joint delivered over the tick, which is how an app
// notices a Joint under a load worth breaking. It is cp's GetImpulse, kind by
// kind: a magnitude for the eight that clamp against MaxForce, the length of
// the two-component Impulse for the pivot and the groove, and a signed number
// for the two Springs, which is cp's own.
//
// Before Solve it is the previous tick's and after Solve it is this tick's,
// exactly as a Contact's Impulses are. For the two Springs it is a readout
// only: they do not warm start — their ApplyCachedImpulse is empty — and their
// accumulated Impulse is rebuilt in PreStep every tick.
//
// A Joint the solver skipped reads 0: a Reference that resolved to nothing, a
// pair of Bodies with no mass between them, an angular kind between two Bodies
// that do not turn, or a degenerate parameter.
func (j Joint) Impulse() float64 {
	switch j.Kind {
	case JointPivot, JointGroove:
		return j.impulse.Length()
	case JointSpring, JointRotarySpring:
		return j.impulse.X
	default:
		return math.Abs(j.impulse.X)
	}
}
