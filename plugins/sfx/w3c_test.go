package sfx

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// The equations are pinned here, once, against W3C's published numbers, with
// no kernel, and no game's test re-asserts them.
//
// Everything compares with a tolerance: sfx promises run-to-run determinism
// on one build and not bit-equality across architectures, since math.Acos and
// math.Cos carry no such guarantee.
const tolerance = 1e-4

func isClose(got, want float64) bool { return math.Abs(got-want) <= tolerance }

func isClose32(got, want float32) bool { return isClose(float64(got), float64(want)) }

// The axes of an unrotated listener: right-handed, forward -Z, up +Y, which
// is W3C's default listener.
var (
	defaultFront = m.Vec3{Z: -1}
	defaultUp    = m.Vec3{Y: 1}
)

// The landmarks are exact: ahead is 0, right +90, left -90 and behind -180.
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
		azimuth, elevation := computeAzimuthElevation(landmark.source.MulS(7), m.Vec3{}, defaultFront, defaultUp)
		if !isClose(azimuth, landmark.want) {
			t.Errorf("a source %s is at azimuth %v, want %v", landmark.where, azimuth, landmark.want)
		}
		if !isClose(elevation, 0) {
			t.Errorf("a source %s is at elevation %v, and it is level with the listener", landmark.where, elevation)
		}
	}
}

// Elevation is computed and no equal-power gain reads it: it is what an HRTF
// effort would want.
func TestElevationIsPlusNinetyOverheadAndMinusNinetyUnderfoot(t *testing.T) {
	azimuth, elevation := computeAzimuthElevation(m.Vec3{Y: 3}, m.Vec3{}, defaultFront, defaultUp)
	if !isClose(elevation, 90) {
		t.Errorf("a source overhead is at elevation %v, want 90", elevation)
	}
	// The zenith snap: the horizontal projection vanishes, so the azimuth is
	// 0 by definition, and nothing normalizes a zero vector on the way.
	if azimuth != 0 {
		t.Errorf("a source overhead is at azimuth %v, want 0 by definition", azimuth)
	}
	if _, underfoot := computeAzimuthElevation(m.Vec3{Y: -3}, m.Vec3{}, defaultFront, defaultUp); !isClose(underfoot, -90) {
		t.Errorf("a source underfoot is at elevation %v, want -90", underfoot)
	}
}

// A source at the listener's exact position has no direction: the azimuth
// and elevation are 0 rather than a normalized zero vector's NaN, and it is
// heard centred.
func TestASourceAtTheListenersExactPositionIsHeardCentred(t *testing.T) {
	here := m.Vec3{X: 12, Y: -3, Z: 40}

	azimuth, elevation := computeAzimuthElevation(here, here, defaultFront, defaultUp)
	if azimuth != 0 || elevation != 0 {
		t.Fatalf("a source on the listener is at (%v, %v), want (0, 0)", azimuth, elevation)
	}
	gains := computeEqualPowerGains(azimuth, false)
	if math.IsNaN(float64(gains[0][0])) || math.IsNaN(float64(gains[0][1])) {
		t.Fatalf("the degenerate case produced %v, and a NaN gain reaches the device", gains)
	}
	if !isClose32(gains[0][0], gains[0][1]) {
		t.Fatalf("the degenerate case produced %v, want it heard centred", gains)
	}
}

// The two ears sum to one in power across the sweep, so a source moving past
// the player is never louder or quieter for moving.
func TestEqualPowerHoldsAcrossTheSweep(t *testing.T) {
	for azimuth := -180.0; azimuth <= 180.0; azimuth += 2.5 {
		gains := computeEqualPowerGains(azimuth, false)
		left := float64(gains[0][0])
		right := float64(gains[0][1])
		if power := left*left + right*right; !isClose(power, 1) {
			t.Fatalf("a mono source at azimuth %v carries power %v, want 1", azimuth, power)
		}
	}
}

// A full orbit favours neither ear, which catches a sign error in the fold
// behind that every landmark alone would miss.
func TestAFullOrbitFavoursNeitherEar(t *testing.T) {
	var left, right float64
	for degrees := range 360 {
		radians := float64(degrees) * math.Pi / 180
		source := m.Vec3{X: float32(math.Sin(radians) * 5), Z: float32(-math.Cos(radians) * 5)}
		azimuth, _ := computeAzimuthElevation(source, m.Vec3{}, defaultFront, defaultUp)
		gains := computeEqualPowerGains(azimuth, false)
		left += float64(gains[0][0])
		right += float64(gains[0][1])
	}
	if math.Abs(left-right) > 1e-3 {
		t.Fatalf("a full orbit put %v in the left ear and %v in the right", left, right)
	}
}

// The fold behind, accepted rather than fixed: two speakers cannot say
// behind, and W3C folds rather than inventing a cue.
func TestASourceBehindPansAsItsMirrorImageInFront(t *testing.T) {
	front, _ := computeAzimuthElevation(m.Vec3{X: 3, Z: -3}, m.Vec3{}, defaultFront, defaultUp)
	behind, _ := computeAzimuthElevation(m.Vec3{X: 3, Z: 3}, m.Vec3{}, defaultFront, defaultUp)
	if isClose(front, behind) {
		t.Fatalf("a source in front and one behind share azimuth %v, and they should not", front)
	}
	if got, want := computeEqualPowerGains(behind, false), computeEqualPowerGains(front, false); got != want {
		t.Fatalf("behind pans to %v and in front to %v, and the fold makes them one", got, want)
	}
}

// A mono clip at the centre is cos(π/4) in each ear, not unity in both: equal
// power rather than a doubling, which a non-positional voice gets too.
func TestAMonoClipAtTheCentreIsEqualPowerRatherThanUnity(t *testing.T) {
	gains := computeEqualPowerGains(0, false)
	want := float32(math.Sqrt2 / 2)
	if !isClose32(gains[0][0], want) || !isClose32(gains[0][1], want) {
		t.Fatalf("a centred mono clip is %v, want %v in each ear", gains, want)
	}
}

// A stereo clip keeps both channels: W3C's stereo arm passes one channel at
// unity and bleeds the other into it, rather than mixing down to mono.
func TestAStereoClipPansByBleedingRatherThanByMixingDown(t *testing.T) {
	// Hard right: the left channel folds into the right at unity, and nothing
	// is left in the left ear.
	hardRight := computeEqualPowerGains(90, true)
	if !isClose32(hardRight[0][0], 0) || !isClose32(hardRight[1][0], 0) {
		t.Fatalf("a hard right stereo clip puts %v in the left ear, want nothing", hardRight)
	}
	if !isClose32(hardRight[1][1], 1) || !isClose32(hardRight[0][1], 1) {
		t.Fatalf("a hard right stereo clip is %v, want both channels at unity in the right ear", hardRight)
	}

	// Centred, it is its own diagonal: each channel to its own ear.
	centred := computeEqualPowerGains(0, true)
	if !isClose32(centred[0][0], 1) || !isClose32(centred[1][1], 1) {
		t.Fatalf("a centred stereo clip is %v, want its own diagonal", centred)
	}
	if !isClose32(centred[1][0], 0) || !isClose32(centred[0][1], 0) {
		t.Fatalf("a centred stereo clip is %v, and its channels bleed into each other", centred)
	}
}

// The three distance models at W3C's numbers: on the defaults a source at a
// distance of 4 is -12 dB and at 10 is -20 dB.
func TestTheDistanceModelsAreW3CsOwnNumbers(t *testing.T) {
	defaults := MakeDefaultFalloff()
	for _, at := range []struct {
		distance float64
		want     float64
		decibels float64
	}{{1, 1, 0}, {4, 0.25, -12.04}, {10, 0.1, -20}} {
		if gain := computeDistanceGain(defaults, at.distance); !isClose(gain, at.want) {
			t.Errorf("the inverse model at %v is %v, want %v (%v dB)", at.distance, gain, at.want, at.decibels)
		}
	}

	// The exponential model with a rolloff of 2: (d/dRef)^-2.
	exponential := Falloff{Model: DistanceExponential, Ref: 1, Max: 10000, Rolloff: 2}
	if gain := computeDistanceGain(exponential, 4); !isClose(gain, 0.0625) {
		t.Errorf("the exponential model at 4 is %v, want 0.0625", gain)
	}

	// The linear model alone reads Max, and alone reaches zero.
	linear := Falloff{Model: DistanceLinear, Ref: 1, Max: 10, Rolloff: 1}
	if gain := computeDistanceGain(linear, 5.5); !isClose(gain, 0.5) {
		t.Errorf("the linear model halfway between Ref and Max is %v, want 0.5", gain)
	}
	if gain := computeDistanceGain(linear, 1000); !isClose(gain, 0) {
		t.Errorf("the linear model past Max is %v, want 0", gain)
	}
}

// Max clamps the linear model alone and silences nothing under the other two:
// W3C's quirk, inherited so sfx's numbers and a PannerNode's never disagree.
func TestMaxClampsTheLinearModelAndSilencesNothingElse(t *testing.T) {
	far := 1e6
	if gain := computeDistanceGain(Falloff{Model: DistanceInverse, Ref: 1, Max: 10, Rolloff: 1}, far); gain <= 0 {
		t.Errorf("the inverse model past Max is %v, and Max does not silence it", gain)
	}
	if gain := computeDistanceGain(Falloff{Model: DistanceExponential, Ref: 1, Max: 10, Rolloff: 1}, far); gain <= 0 {
		t.Errorf("the exponential model past Max is %v, and Max does not silence it", gain)
	}
	if gain := computeDistanceGain(Falloff{Model: DistanceLinear, Ref: 1, Max: 10, Rolloff: 1}, far); !isClose(gain, 0) {
		t.Errorf("the linear model past Max is %v, and Max is where it bottoms out", gain)
	}
}

// A Falloff or a Cone nobody filled in is W3C's defaults rather than
// silence: an all-zero cone read literally is a voice heard nowhere.
func TestAnAllZeroFalloffAndConeAreTheW3CDefaults(t *testing.T) {
	if got, want := (Falloff{}).resolve(), MakeDefaultFalloff(); got != want {
		t.Fatalf("an unfilled Falloff resolves to %+v, want %+v", got, want)
	}
	if got, want := (Cone{}).resolve(), MakeDefaultCone(); got != want {
		t.Fatalf("an unfilled Cone resolves to %+v, want %+v", got, want)
	}
	if gain := computeConeGain((Cone{}).resolve(), m.Vec3{}, m.Vec3{Z: -1}, m.Vec3{X: 5}); !isClose(gain, 1) {
		t.Fatalf("an unfilled Cone attenuates to %v, want no cone at all", gain)
	}
	// Partly filled is taken literally.
	if got := (Falloff{Ref: 64}).resolve(); got != (Falloff{Ref: 64}) {
		t.Fatalf("a Falloff with one field set resolves to %+v, want it taken literally", got)
	}
}

// A divisor the falloff's own fields made zero is no attenuation, never a NaN
// gain that would silence or explode a device.
func TestAnUndefinedDivisorIsNoAttenuationRatherThanNaN(t *testing.T) {
	for _, falloff := range []Falloff{
		{Model: DistanceLinear, Ref: 4, Max: 4, Rolloff: 1},
		{Model: DistanceExponential, Ref: 0, Rolloff: 1},
		{Model: DistanceInverse, Ref: 0, Rolloff: 0},
	} {
		for _, distance := range []float64{0, 4, 400} {
			if gain := computeDistanceGain(falloff, distance); math.IsNaN(gain) {
				t.Fatalf("%+v at %v produced NaN", falloff, distance)
			}
		}
	}
}

// The cone gain: no attenuation inside the inner cone, the outer gain outside
// the outer one, and a straight line between, the whole angles halved.
func TestTheConeGainIsW3CsThreeBands(t *testing.T) {
	cone := Cone{Inner: 90, Outer: 180, OuterGain: 0.2}
	forward := m.Vec3{Z: -1}
	at := func(degrees float64) m.Vec3 {
		radians := degrees * math.Pi / 180
		return m.Vec3{X: float32(math.Sin(radians) * 6), Z: float32(-math.Cos(radians) * 6)}
	}

	for _, band := range []struct {
		where   string
		degrees float64
		want    float64
	}{
		{"straight ahead of the cone", 0, 1},
		{"inside the inner cone", 44, 1},
		{"halfway between the cones", 67.5, 0.6},
		{"outside the outer cone", 120, 0.2},
	} {
		if gain := computeConeGain(cone, m.Vec3{}, forward, at(band.degrees)); !isClose(gain, band.want) {
			t.Errorf("a listener %s hears %v, want %v", band.where, gain, band.want)
		}
	}
}

// The design's 2D table, on an 800×600 screen with the player at its centre.
// Distances are right in every row either way, so an unrotated listener's
// three working pans are a quiet failure.
func TestTheTwoDimensionalListenerRotationIsTheSpecsTable(t *testing.T) {
	rotated := m.QuatRotationX(-math.Pi / 2)
	front := rotated.Rotate(m.Vec3{Z: -1})
	up := rotated.Rotate(m.Vec3{Y: 1})

	for _, row := range []struct {
		source    string
		at        m.Vec3
		unrotated float64
		rotated   float64
	}{
		// A 2D game is Y-down, so up the screen is negative Y.
		{"20 px right, level", m.Vec3{X: 20}, 90, 90},
		{"200 px right, 200 px up the screen", m.Vec3{X: 200, Y: -200}, 90, 45},
		{"20 px right, 400 px up the screen", m.Vec3{X: 20, Y: -400}, 90, 2.8624},
		{"straight up the screen", m.Vec3{Y: -300}, 0, 0},
	} {
		broken, _ := computeAzimuthElevation(row.at, m.Vec3{}, defaultFront, defaultUp)
		if !isClose(broken, row.unrotated) {
			t.Errorf("%s under an unrotated listener is %v, want the design's %v", row.source, broken, row.unrotated)
		}
		azimuth, elevation := computeAzimuthElevation(row.at, m.Vec3{}, front, up)
		if !isClose(azimuth, row.rotated) {
			t.Errorf("%s under a rotated listener is %v, want %v", row.source, azimuth, row.rotated)
		}
		// Screen Y is front and back, not elevation.
		if !isClose(elevation, 0) {
			t.Errorf("%s has elevation %v in a 2D game, want 0", row.source, elevation)
		}
	}
}

// The 2D cone is the listener's quaternion turned in-plane, so a 2D author
// learns one rotation and spends it on the listener and on every directional
// emitter.
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
		if !isClose32(forward.X, turn.want.X) || !isClose32(forward.Y, turn.want.Y) || !isClose32(forward.Z, turn.want.Z) {
			t.Errorf("an emitter turned %v degrees in-plane faces %v, want %v", turn.degrees, forward, turn.want)
		}
	}
}
