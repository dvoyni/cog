package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// The equations are pinned here, once, against W3C's published numbers, and no
// game's test ever re-asserts them.
//
// Everything compares with a tolerance. sound promises run-to-run determinism
// on one build and explicitly not bit-equality across architectures, because
// math.Acos and math.Cos carry no such guarantee, so an exact float comparison
// here would be a test that passes on the machine it was written on.
const tolerance = 1e-4

func closeTo(got, want float64) bool { return math.Abs(got-want) <= tolerance }

func closeTo32(got, want float32) bool { return math.Abs(float64(got-want)) <= tolerance }

// The Listener's own axes, as NewListener builds them: right-handed, forward
// -Z, up +Y, which is the W3C default Listener exactly.
var (
	defaultFront = m.Vec3{Z: -1}
	defaultUp    = m.Vec3{Y: 1}
)

// The landmarks are exact, and they are what makes an author's mental model
// correct: ahead is 0, right is +90, left is -90, and behind is -180.
func TestTheAzimuthLandmarksAreExact(t *testing.T) {
	for _, landmark := range []struct {
		where  string
		source m.Vec3
		want   float64
	}{
		{"ahead", m.Vec3{Z: -1}, 0},
		{"to the right", m.Vec3{X: 1}, 90},
		{"to the left", m.Vec3{X: -1}, -90},
		{"behind", m.Vec3{Z: 1}, -180},
	} {
		azimuth, elevation := azimuthElevation(landmark.source.MulS(7), m.Vec3{}, defaultFront, defaultUp)
		if !closeTo(azimuth, landmark.want) {
			t.Errorf("a source %s is at azimuth %v, want %v", landmark.where, azimuth, landmark.want)
		}
		if !closeTo(elevation, 0) {
			t.Errorf("a source %s is at elevation %v, and it is level with the Listener", landmark.where, elevation)
		}
	}
}

// Elevation is the other half of the bearing, and it is retained rather than
// used: W3C's equalpower panner reads azimuth alone, and elevation is what an
// HRTF effort would want.
func TestElevationIsPlusNinetyOverheadAndMinusNinetyUnderfoot(t *testing.T) {
	azimuth, elevation := azimuthElevation(m.Vec3{Y: 3}, m.Vec3{}, defaultFront, defaultUp)
	if !closeTo(elevation, 90) {
		t.Errorf("a source overhead is at elevation %v, want 90", elevation)
	}

	// The zenith snap: the horizontal projection vanishes, so azimuth is 0 by
	// definition and nothing normalizes a zero vector on the way there.
	if !closeTo(azimuth, 0) {
		t.Errorf("a source overhead is at azimuth %v, want 0 by definition", azimuth)
	}

	if _, underfoot := azimuthElevation(m.Vec3{Y: -3}, m.Vec3{}, defaultFront, defaultUp); !closeTo(underfoot, -90) {
		t.Errorf("a source underfoot is at elevation %v, want -90", underfoot)
	}
}

// A source standing exactly where the Listener does has no direction at all.
// The implementation answers 0 and 0 rather than normalizing a zero vector,
// which is the one degenerate case that would otherwise reach a device as NaN.
func TestASourceAtTheListenersExactPositionIsHeardCentred(t *testing.T) {
	here := m.Vec3{X: 12, Y: -3, Z: 40}

	azimuth, elevation := azimuthElevation(here, here, defaultFront, defaultUp)
	if azimuth != 0 || elevation != 0 {
		t.Fatalf("a source on top of the Listener is at (%v, %v), want (0, 0)", azimuth, elevation)
	}

	gains := equalPowerGains(azimuth, false)
	if math.IsNaN(float64(gains[0][0])) || math.IsNaN(float64(gains[0][1])) {
		t.Fatalf("the degenerate case produced %v, and a NaN gain reaches the device", gains)
	}
	if !closeTo32(gains[0][0], gains[0][1]) {
		t.Fatalf("the degenerate case produced %v, want it heard centred", gains)
	}
}

// Constant power is the whole point of the model: the two ears sum to one in
// power across the sweep, so a source moving past the player never gets louder
// or quieter for moving.
func TestEqualPowerHoldsAcrossTheSweep(t *testing.T) {
	for azimuth := -180.0; azimuth <= 180.0; azimuth += 2.5 {
		gains := equalPowerGains(azimuth, false)
		left, right := float64(gains[0][0]), float64(gains[0][1])
		if power := left*left + right*right; !closeTo(power, 1) {
			t.Fatalf("a mono source at azimuth %v carries power %v, want 1", azimuth, power)
		}
	}
}

// A full orbit favours neither ear. It is the property that catches a sign
// error in the front/back fold, which a single landmark cannot: every landmark
// can be right while the half-turns between them lean one way.
func TestAFullOrbitFavoursNeitherEar(t *testing.T) {
	var left, right float64
	for degrees := 0; degrees < 360; degrees++ {
		radians := float64(degrees) * math.Pi / 180
		source := m.Vec3{X: float32(math.Sin(radians) * 5), Z: float32(-math.Cos(radians) * 5)}
		azimuth, _ := azimuthElevation(source, m.Vec3{}, defaultFront, defaultUp)
		gains := equalPowerGains(azimuth, false)
		left += float64(gains[0][0])
		right += float64(gains[0][1])
	}
	if math.Abs(left-right) > 1e-3 {
		t.Fatalf("a full orbit put %v in the left ear and %v in the right", left, right)
	}
}

// The fold behind, accepted rather than fixed: two speakers cannot say behind,
// and W3C folds rather than inventing a cue.
func TestASourceBehindPansAsItsMirrorImageInFront(t *testing.T) {
	front, _ := azimuthElevation(m.Vec3{X: 3, Z: -3}, m.Vec3{}, defaultFront, defaultUp)
	behind, _ := azimuthElevation(m.Vec3{X: 3, Z: 3}, m.Vec3{}, defaultFront, defaultUp)
	if closeTo(front, behind) {
		t.Fatalf("a source in front and one behind share azimuth %v, and they should not", front)
	}
	if got, want := equalPowerGains(behind, false), equalPowerGains(front, false); got != want {
		t.Fatalf("behind pans to %v and in front to %v, and the fold makes them one", got, want)
	}
}

// A mono Clip at the centre is 0.707 in each ear, not unity in both. That is
// equal power rather than a doubling, and it is the number a non-positional
// Voice gets too, because "heard centred" is a position on the circle and not
// a second rule.
func TestAMonoClipAtTheCentreIsEqualPowerRatherThanUnity(t *testing.T) {
	gains := equalPowerGains(0, false)
	want := float32(math.Sqrt2 / 2)
	if !closeTo32(gains[0][0], want) || !closeTo32(gains[0][1], want) {
		t.Fatalf("a centred mono Clip is %v, want %v in each ear", gains, want)
	}
}

// A stereo Clip keeps both channels and shifts weight between them: W3C's
// stereo arm passes one channel at unity and bleeds the other into it. It is
// not mixed down to mono first, because mixing down is a departure a PannerNode
// backend would have to imitate by hand.
func TestAStereoClipPansByBleedingRatherThanByMixingDown(t *testing.T) {
	// Hard right: the left channel folds into the right at unity, and nothing
	// is left in the left ear.
	hardRight := equalPowerGains(90, true)
	if !closeTo32(hardRight[0][0], 0) || !closeTo32(hardRight[1][0], 0) {
		t.Fatalf("a hard right stereo Clip puts %v in the left ear, want nothing", hardRight)
	}
	if !closeTo32(hardRight[1][1], 1) || !closeTo32(hardRight[0][1], 1) {
		t.Fatalf("a hard right stereo Clip is %v, want both channels at unity in the right ear", hardRight)
	}

	// Centred, it is its own diagonal: each channel goes to its own ear
	// untouched, which is why a non-positional stereo Clip sounds like itself.
	centred := equalPowerGains(0, true)
	if !closeTo32(centred[0][0], 1) || !closeTo32(centred[1][1], 1) {
		t.Fatalf("a centred stereo Clip is %v, want its own diagonal", centred)
	}
	if !closeTo32(centred[1][0], 0) || !closeTo32(centred[0][1], 0) {
		t.Fatalf("a centred stereo Clip is %v, and the channels are bleeding into each other", centred)
	}
}

// The three distance models, at the numbers the prototype measured and the
// spec quotes: on W3C's defaults a source at radius 4 is -12 dB and at radius
// 10 is -20 dB. Correct, and far quieter than an author expects off metre
// scale, which is why the guidance is to set Ref to the world's own.
func TestTheDistanceModelsAreW3CsOwnNumbers(t *testing.T) {
	defaults := DefaultFalloff()

	for _, at := range []struct {
		distance float64
		want     float64
		decibels float64
	}{{1, 1, 0}, {4, 0.25, -12.04}, {10, 0.1, -20}} {
		gain := distanceGain(defaults, at.distance)
		if !closeTo(gain, at.want) {
			t.Errorf("the inverse model at radius %v is %v, want %v (%v dB)", at.distance, gain, at.want, at.decibels)
		}
	}

	// The exponential model, with a rolloff of 2: (d/dRef)^-2.
	exponential := Falloff{Model: DistanceExponential, Ref: 1, Max: 10000, Rolloff: 2}
	if gain := distanceGain(exponential, 4); !closeTo(gain, 0.0625) {
		t.Errorf("the exponential model at radius 4 is %v, want 0.0625", gain)
	}

	// The linear model is the odd one: it alone reads Max, and it alone
	// reaches zero.
	linear := Falloff{Model: DistanceLinear, Ref: 1, Max: 10, Rolloff: 1}
	if gain := distanceGain(linear, 5.5); !closeTo(gain, 0.5) {
		t.Errorf("the linear model halfway between Ref and Max is %v, want 0.5", gain)
	}
	if gain := distanceGain(linear, 1000); !closeTo(gain, 0) {
		t.Errorf("the linear model past Max is %v, want 0", gain)
	}
}

// Max clamps the linear model alone and never silences a Voice under the other
// two. That is W3C's quirk, inherited deliberately: correcting it here would
// be the one place our numbers and a PannerNode's disagreed.
func TestMaxClampsTheLinearModelAndSilencesNothingElse(t *testing.T) {
	far := 1e6

	inverse := Falloff{Model: DistanceInverse, Ref: 1, Max: 10, Rolloff: 1}
	if gain := distanceGain(inverse, far); gain <= 0 {
		t.Errorf("the inverse model past Max is %v, and Max does not silence it", gain)
	}

	exponential := Falloff{Model: DistanceExponential, Ref: 1, Max: 10, Rolloff: 1}
	if gain := distanceGain(exponential, far); gain <= 0 {
		t.Errorf("the exponential model past Max is %v, and Max does not silence it", gain)
	}

	linear := Falloff{Model: DistanceLinear, Ref: 1, Max: 10, Rolloff: 1}
	if gain := distanceGain(linear, far); !closeTo(gain, 0) {
		t.Errorf("the linear model past Max is %v, and Max is where it bottoms out", gain)
	}
}

// A Falloff or a Cone nobody filled in is the W3C defaults rather than silence,
// which is m.Transform's zero-value rule one Slot over. An all-zero Cone read
// literally is an outer angle of 0 degrees at an outer gain of 0 - a Voice
// that cannot be heard from anywhere - and that is exactly what an ECS
// Component an author never touched would carry.
func TestAnAllZeroFalloffAndConeAreTheW3CDefaults(t *testing.T) {
	if got, want := (Falloff{}).resolve(), DefaultFalloff(); got != want {
		t.Fatalf("an unfilled Falloff resolves to %+v, want %+v", got, want)
	}
	if got, want := (Cone{}).resolve(), DefaultCone(); got != want {
		t.Fatalf("an unfilled Cone resolves to %+v, want %+v", got, want)
	}
	if gain := coneGain((Cone{}).resolve(), m.Vec3{}, m.Vec3{Z: -1}, m.Vec3{X: 5}); !closeTo(gain, 1) {
		t.Fatalf("an unfilled Cone attenuates to %v, want no cone at all", gain)
	}
	// Partly filled is taken literally, the way a partly zero Scale is.
	if got := (Falloff{Ref: 64}).resolve(); got != (Falloff{Ref: 64}) {
		t.Fatalf("a Falloff with one field set resolves to %+v, want it taken literally", got)
	}
}

// An undefined divisor is no attenuation. W3C's formulas divide by numbers the
// game chose, and a Falloff that makes one of them zero divides zero by zero;
// a NaN gain silences or explodes a device rather than being quiet.
func TestAnUndefinedDivisorIsNoAttenuationRatherThanNaN(t *testing.T) {
	for _, falloff := range []Falloff{
		{Model: DistanceLinear, Ref: 4, Max: 4, Rolloff: 1},
		{Model: DistanceExponential, Ref: 0, Rolloff: 1},
		{Model: DistanceInverse, Ref: 0, Rolloff: 0},
	} {
		for _, distance := range []float64{0, 4, 400} {
			if gain := distanceGain(falloff, distance); math.IsNaN(gain) {
				t.Fatalf("%+v at radius %v produced NaN", falloff, distance)
			}
		}
	}
}

// The cone gain, transcribed: no attenuation inside the inner cone, the outer
// gain outside the outer one, and a straight line between them. The angles are
// the whole angle and not the half-angle, which is why the implementation
// halves them.
func TestTheConeGainIsW3CsThreeBands(t *testing.T) {
	cone := Cone{Inner: 90, Outer: 180, OuterGain: 0.2}
	source := m.Vec3{}
	forward := m.Vec3{Z: -1}

	at := func(degrees float64) m.Vec3 {
		radians := degrees * math.Pi / 180
		return m.Vec3{X: float32(math.Sin(radians) * 6), Z: float32(-math.Cos(radians) * 6)}
	}

	if gain := coneGain(cone, source, forward, at(0)); !closeTo(gain, 1) {
		t.Errorf("a Listener straight ahead of the cone hears %v, want 1", gain)
	}
	if gain := coneGain(cone, source, forward, at(44)); !closeTo(gain, 1) {
		t.Errorf("a Listener inside the inner cone hears %v, want 1", gain)
	}
	if gain := coneGain(cone, source, forward, at(67.5)); !closeTo(gain, 0.6) {
		t.Errorf("a Listener halfway between the cones hears %v, want 0.6", gain)
	}
	if gain := coneGain(cone, source, forward, at(120)); !closeTo(gain, 0.2) {
		t.Errorf("a Listener outside the outer cone hears %v, want the outer gain 0.2", gain)
	}
}

// The 2D table, measured on an 800x600 canvas with the player at centre. It is
// the spec's own, reproduced here because the failure it describes is quiet:
// distances are correct in every row either way, so an unrotated Listener gives
// three working pans and nothing reports anything.
func TestTheTwoDimensionalListenerRotationIsTheSpecsTable(t *testing.T) {
	rotated := m.QuatRotationX(-math.Pi / 2)
	front, up := rotated.Rotate(m.Vec3{Z: -1}), rotated.Rotate(m.Vec3{Y: 1})

	for _, row := range []struct {
		source     string
		at         m.Vec3
		unrotated  float64
		wantWhenOk float64
	}{
		// Canvas is Y-down, so up-screen is negative Y.
		{"20 px right, level", m.Vec3{X: 20}, 90, 90},
		{"200 px right, 200 px up-screen", m.Vec3{X: 200, Y: -200}, 90, 45},
		{"20 px right, 400 px up-screen", m.Vec3{X: 20, Y: -400}, 90, 2.8624},
		{"straight up-screen", m.Vec3{Y: -300}, 0, 0},
	} {
		broken, _ := azimuthElevation(row.at, m.Vec3{}, defaultFront, defaultUp)
		if !closeTo(broken, row.unrotated) {
			t.Errorf("%s under an unrotated Listener is %v, want the spec's %v", row.source, broken, row.unrotated)
		}
		azimuth, elevation := azimuthElevation(row.at, m.Vec3{}, front, up)
		if !closeTo(azimuth, row.wantWhenOk) {
			t.Errorf("%s under a rotated Listener is %v, want %v", row.source, azimuth, row.wantWhenOk)
		}
		// Screen Y is front-and-back, not elevation: under the rotation,
		// elevation is identically zero in a 2D game.
		if !closeTo(elevation, 0) {
			t.Errorf("%s has elevation %v in a 2D game, want 0", row.source, elevation)
		}
	}
}

// The 2D cone is the same quaternion turned in-plane, so an author learns one
// rotation and spends it on the Listener and on every directional emitter.
func TestTheTwoDimensionalConeIsTheSameQuaternionTurnedInPlane(t *testing.T) {
	base := m.QuatRotationX(-math.Pi / 2)
	for _, turn := range []struct {
		degrees float64
		want    m.Vec3
	}{
		{0, m.Vec3{Y: -1}},
		{90, m.Vec3{X: 1}},
		{180, m.Vec3{Y: 1}},
	} {
		facing := m.QuatRotationZ(float32(turn.degrees * math.Pi / 180)).Mul(base)
		forward := facing.Rotate(m.Vec3{Z: -1})
		if !closeTo32(forward.X, turn.want.X) || !closeTo32(forward.Y, turn.want.Y) || !closeTo32(forward.Z, turn.want.Z) {
			t.Errorf("an emitter turned %v degrees in-plane faces %v, want %v", turn.degrees, forward, turn.want)
		}
	}
}
