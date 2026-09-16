package types

import (
	"errors"
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// The step every acceptance number below is quoted at. cog's step is fixed, so
// a per-tick figure and a per-second one are the same claim said two ways.
const step = 1.0 / 60

func TestNewDynamicStoresTheInversesOfWhatItIsGiven(t *testing.T) {
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
	if got, want := body.Damping(), 15.0; got != want {
		t.Errorf("Damping = %v, want %v", got, want)
	}
	if got, want := body.AngularDamping(), 0.3; got != want {
		t.Errorf("AngularDamping = %v, want %v", got, want)
	}
}

// The rejections are what replaces cp's exact comparison of a mass against
// INFINITY: nothing that would store an infinite or a NaN inverse is ever
// stored, so the +Inf·0 = NaN cp's Joints hit cannot arise from a Body.
func TestNewDynamicRejectsEveryMassThatIsNotPositiveAndFinite(t *testing.T) {
	for _, mass := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		body, err := NewDynamic(mass, 8, 0, 0)
		var refusal ErrBadMass
		if !errors.As(err, &refusal) {
			t.Errorf("NewDynamic with mass %v returned %v, want an ErrBadMass", mass, err)
		}
		if body != (Dynamic{}) {
			t.Errorf("NewDynamic with mass %v returned %+v, want the zero Dynamic", mass, body)
		}
	}
}

// An infinite Moment of inertia is the one infinity that is legal: it is cp's
// own spelling of a Body that does not turn, which is why there is no
// FixedRotation Tag to say it a second time.
func TestAnInfiniteMomentIsLegalAndEveryOtherBadMomentIsNot(t *testing.T) {
	body, err := NewDynamic(2, math.Inf(1), 0, 0)
	if err != nil {
		t.Fatalf("NewDynamic with an infinite moment: %v", err)
	}
	if got := body.Moment(); !math.IsInf(got, 1) {
		t.Errorf("Moment = %v, want +Inf", got)
	}

	velocity, force := Velocity{}, Force{Torque: 1000}
	IntegrateVelocity(&body, &velocity, &force, step)
	if velocity.Angular != 0 {
		t.Errorf("a Body with an infinite moment turned at %v under a torque of 1000", velocity.Angular)
	}

	for _, moment := range []float64{0, -1, math.NaN(), math.Inf(-1)} {
		var refusal ErrBadMoment
		if _, err := NewDynamic(2, moment, 0, 0); !errors.As(err, &refusal) {
			t.Errorf("NewDynamic with moment %v returned %v, want an ErrBadMoment", moment, err)
		}
	}
}

// Damping is a rate per second and the step raises e to minus it, so a negative
// rate amplifies velocity every tick until it is an infinity. Refusing it here
// is the finiteness invariant held at the only place a rate is written.
func TestADampingRateIsFiniteAndNotNegative(t *testing.T) {
	for _, rate := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		var refusal ErrBadDamping
		if _, err := NewDynamic(2, 8, rate, 0); !errors.As(err, &refusal) || refusal.Angular {
			t.Errorf("NewDynamic with damping %v returned %v, want a linear ErrBadDamping", rate, err)
		}
		if _, err := NewDynamic(2, 8, 0, rate); !errors.As(err, &refusal) || !refusal.Angular {
			t.Errorf("NewDynamic with angular damping %v returned %v, want an angular ErrBadDamping", rate, err)
		}
	}
	if _, err := NewDynamic(2, 8, 0, 0); err != nil {
		t.Errorf("a damping rate of zero was refused: %v", err)
	}
}

// A setter changes one thing and leaves the rest of the Body alone, and a
// refused one writes nothing at all — so a caller that ignores the error keeps
// the Body it had rather than half of the one it asked for.
func TestASetterChangesOneFieldAndARefusedOneChangesNone(t *testing.T) {
	body, err := NewDynamic(2, 8, 15, 0.3)
	if err != nil {
		t.Fatalf("NewDynamic: %v", err)
	}

	if err := body.SetMass(4); err != nil {
		t.Fatalf("SetMass(4): %v", err)
	}
	if got, want := body.Mass(), 4.0; got != want {
		t.Errorf("Mass = %v, want %v", got, want)
	}
	if got, want := body.Moment(), 8.0; got != want {
		t.Errorf("SetMass moved the moment to %v, want %v", got, want)
	}

	if err := body.SetMoment(16); err != nil {
		t.Fatalf("SetMoment(16): %v", err)
	}
	if err := body.SetDamping(1); err != nil {
		t.Fatalf("SetDamping(1): %v", err)
	}
	if err := body.SetAngularDamping(2); err != nil {
		t.Fatalf("SetAngularDamping(2): %v", err)
	}
	before := body

	var badMass ErrBadMass
	if err := body.SetMass(0); !errors.As(err, &badMass) || body != before {
		t.Errorf("SetMass(0) returned %v and left %+v, want an ErrBadMass and %+v", err, body, before)
	}
	var badMoment ErrBadMoment
	if err := body.SetMoment(-1); !errors.As(err, &badMoment) || body != before {
		t.Errorf("SetMoment(-1) returned %v and left %+v, want an ErrBadMoment and %+v", err, body, before)
	}
	var badDamping ErrBadDamping
	if err := body.SetDamping(math.NaN()); !errors.As(err, &badDamping) || body != before {
		t.Errorf("SetDamping(NaN) returned %v and left %+v, want an ErrBadDamping and %+v", err, body, before)
	}
	if err := body.SetAngularDamping(-2); !errors.As(err, &badDamping) || body != before {
		t.Errorf("SetAngularDamping(-2) returned %v and left %+v, want an ErrBadDamping and %+v", err, body, before)
	}
}

// The zero Dynamic is not a second spelling of Kinematic and nothing checks for
// it: it is infinite mass and an infinite moment, so it goes nowhere under any
// Force, which is what makes forgetting the constructor visible rather than
// quietly wrong.
func TestTheZeroDynamicIsHarmless(t *testing.T) {
	body := Dynamic{}
	if got := body.Mass(); !math.IsInf(got, 1) {
		t.Errorf("the zero Dynamic's Mass is %v, want +Inf", got)
	}
	if got := body.Moment(); !math.IsInf(got, 1) {
		t.Errorf("the zero Dynamic's Moment is %v, want +Inf", got)
	}

	velocity := Velocity{}
	force := Force{Force: m.Vec2d{X: 1e9, Y: -1e9}, Torque: 1e9}
	IntegrateVelocity(&body, &velocity, &force, step)
	if velocity != (Velocity{}) {
		t.Errorf("the zero Dynamic moved to %+v under a force of 1e9", velocity)
	}
}

// Solve clears the Force and the Torque once it has spent them, which is what
// makes a Force a thing said once per tick rather than a setting that stays on.
func TestIntegratingAVelocitySpendsTheForceAndClearsIt(t *testing.T) {
	body, err := NewDynamic(2, 8, 0, 0)
	if err != nil {
		t.Fatalf("NewDynamic: %v", err)
	}
	velocity := Velocity{}
	force := Force{Force: m.Vec2d{X: 10, Y: -4}, Torque: 16}

	IntegrateVelocity(&body, &velocity, &force, step)

	// v = F·h/m and w = T·h/I, undamped: 10·(1/60)/2 and 16·(1/60)/8.
	if want := (m.Vec2d{X: 10 * step / 2, Y: -4 * step / 2}); !vecNear(velocity.Linear, want) {
		t.Errorf("Linear = %v, want %v", velocity.Linear, want)
	}
	if want := 16 * step / 8; !nearF(velocity.Angular, want) {
		t.Errorf("Angular = %v, want %v", velocity.Angular, want)
	}
	if force != (Force{}) {
		t.Errorf("the Force was left at %+v, want it cleared", force)
	}
}

// Previous and PreviousAngle are written at the top of the position update, so
// within a tick they name where the Body was when the tick began and a render
// copy lerps between the two without a seam at ±π.
func TestIntegratingAPositionRecordsWhereTheBodyWas(t *testing.T) {
	position := Position{Current: m.Vec2d{X: 1, Y: 2}, Angle: 0.5}
	velocity := Velocity{Linear: m.Vec2d{X: 6, Y: -12}, Angular: 3}

	IntegratePosition(&position, &velocity, step)

	if want := (m.Vec2d{X: 1, Y: 2}); position.Previous != want {
		t.Errorf("Previous = %v, want %v", position.Previous, want)
	}
	if want := 0.5; position.PreviousAngle != want {
		t.Errorf("PreviousAngle = %v, want %v", position.PreviousAngle, want)
	}
	if want := (m.Vec2d{X: 1 + 6*step, Y: 2 - 12*step}); !vecNear(position.Current, want) {
		t.Errorf("Current = %v, want %v", position.Current, want)
	}
	if want := 0.5 + 3*step; !nearF(position.Angle, want) {
		t.Errorf("Angle = %v, want %v", position.Angle, want)
	}
}

// The Angle counts on from every earlier turn rather than wrapping into one
// revolution, which is what lets interpolation be a plain lerp.
func TestTheAngleIsNeverWrapped(t *testing.T) {
	position := Position{}
	velocity := Velocity{Angular: 4 * math.Pi}

	for range 60 {
		IntegratePosition(&position, &velocity, step)
	}

	if want := 4 * math.Pi; !nearF(position.Angle, want) {
		t.Errorf("Angle after two turns = %v, want %v and not a wrapped %v",
			position.Angle, want, math.Mod(want, 2*math.Pi))
	}
}

// Damping is exponential in the rate, which is exact and stable at any step: a
// naive v − rate·v·h goes negative once rate·h > 1, and Box2D's 1/(1 + rate·h)
// is cheaper but departs from cp.
func TestDampingIsExponentialInTheRatePerSecond(t *testing.T) {
	body, err := NewDynamic(2, 8, 15, 30)
	if err != nil {
		t.Fatalf("NewDynamic: %v", err)
	}
	velocity := Velocity{Linear: m.Vec2d{X: 4}, Angular: 2}
	force := Force{}

	IntegrateVelocity(&body, &velocity, &force, step)

	if want := 4 * math.Exp(-15*step); !nearF(velocity.Linear.X, want) {
		t.Errorf("Linear.X = %v, want %v", velocity.Linear.X, want)
	}
	if want := 2 * math.Exp(-30*step); !nearF(velocity.Angular, want) {
		t.Errorf("Angular = %v, want %v", velocity.Angular, want)
	}
}

// The first of the two acceptance criteria that survive from the driving game,
// asserted in SI units: a Dynamic body under a constant Force settles at a
// terminal speed, and reaches 95% of the speed that was asked for well inside
// 190 ms at a Damping of 15 /s.
//
// The terminal speed is not F/(mλ). cp damps exactly and then applies the Force
// as a plain Euler step, so what a constant Force settles at is
//
//	v* = F/(mλ) · λh/(1 − exp(−λh))
//
// which is +13.0% over F/(mλ) at 60 Hz and +27.1% at 30. cog's step is fixed, so
// that is a constant offset that disappears into tuning; the exact form was
// weighed and not taken, because it costs a divide and a λ = 0 branch per Body
// per tick to buy nothing at a fixed step.
//
// The ramp is measured against the F/(mλ) the caller asked for, which is what
// the criterion means by terminal. Reaching 95% of v* itself takes ln(20)/λ =
// 200 ms, because v* is 13% higher: that is a consequence of damping
// exponentially where the driving game multiplied by (1 − k) each tick, a rule
// under which the two terminal speeds coincide and the bound was calibrated.
func TestABodyUnderAConstantForceRampsToItsTerminalSpeed(t *testing.T) {
	const (
		rate  = 15.0 // 1/s
		mass  = 2.0  // kg
		push  = 10.0 // N
		ramp  = 0.190
		asked = push / (mass * rate) // m/s, the speed the caller asked for
	)
	body, err := NewDynamic(mass, 8, rate, 0)
	if err != nil {
		t.Fatalf("NewDynamic: %v", err)
	}

	terminal := asked * rate * step / (1 - math.Exp(-rate*step))
	velocity, reached := Velocity{}, math.Inf(1)
	for tick := 1; tick <= 600; tick++ {
		force := Force{Force: m.Vec2d{X: push}}
		IntegrateVelocity(&body, &velocity, &force, step)
		if velocity.Linear.X >= 0.95*asked && math.IsInf(reached, 1) {
			reached = float64(tick) * step
		}
	}

	t.Logf("terminal %.6f m/s against the %.6f m/s asked for (+%.1f%%), 95%% of it reached at %.0f ms",
		velocity.Linear.X, asked, 100*(terminal/asked-1), 1000*reached)

	if !nearF(velocity.Linear.X, terminal) {
		t.Errorf("the terminal speed is %v m/s, want %v m/s", velocity.Linear.X, terminal)
	}
	if got, want := terminal/asked, 1.1302; math.Abs(got-want) > 1e-4 {
		t.Errorf("the terminal speed is %.4f× the speed asked for, want %.4f×", got, want)
	}
	if reached > ramp {
		t.Errorf("95%% of %v m/s was reached at %v s, want it within %v s", asked, reached, ramp)
	}
}

// The second surviving criterion, which is what confirms Damping's units
// against a measured number: the driving game's k is per tick, so ×60 gives
// 0.3 /s for a boulder, and ln2/0.3 = 2.31 s reproduces the 2.3 s half-life it
// quoted. Decay under no Force is exactly exponential, so this one holds at any
// step rather than approximately.
func TestACrateStopsByDampingWithAHalfLifeOfLnTwoOverTheRate(t *testing.T) {
	const rate = 0.3 // 1/s
	body, err := NewDynamic(50, 100, rate, 0)
	if err != nil {
		t.Fatalf("NewDynamic: %v", err)
	}
	velocity := Velocity{Linear: m.Vec2d{X: 4}}

	half := math.Inf(1)
	for tick := 1; tick <= 600; tick++ {
		force := Force{}
		IntegrateVelocity(&body, &velocity, &force, step)
		if velocity.Linear.X <= 2 && math.IsInf(half, 1) {
			half = float64(tick) * step
		}
	}

	want := math.Ln2 / rate
	t.Logf("half-life %.4f s against ln2/%v = %.4f s", half, rate, want)
	if math.Abs(half-want) > step {
		t.Errorf("the half-life is %v s, want %v s within one step of %v s", half, want, step)
	}
}

// Nothing on the step's path reaches the heap: the two integrators take
// pointers into Component Stores and compute in registers, and m.Vec2d's
// variadic MulS does not escape its slice.
func TestTheIntegratorsAllocateNothing(t *testing.T) {
	body, err := NewDynamic(2, 8, 15, 0.3)
	if err != nil {
		t.Fatalf("NewDynamic: %v", err)
	}
	position := Position{}
	velocity := Velocity{Linear: m.Vec2d{X: 1}, Angular: 1}

	if got := testing.AllocsPerRun(1000, func() {
		force := Force{Force: m.Vec2d{X: 10}, Torque: 4}
		IntegrateVelocity(&body, &velocity, &force, step)
		IntegratePosition(&position, &velocity, step)
	}); got != 0 {
		t.Errorf("one integrated Body allocates %v objects, want 0", got)
	}
}
