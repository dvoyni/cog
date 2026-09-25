package internal

import (
	"errors"
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

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
	if got, want := body.Damping, 15.0; got != want {
		t.Errorf("Damping = %v, want %v", got, want)
	}
	if got, want := body.AngularDamping, 0.3; got != want {
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
	IntegrateVelocity(&body, &velocity, &force, m.Vec2d{}, solverStep)
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
	IntegrateVelocity(&body, &velocity, &force, m.Vec2d{}, solverStep)
	if velocity != (Velocity{}) {
		t.Errorf("the zero Dynamic moved to %+v under a force of 1e9", velocity)
	}
}
