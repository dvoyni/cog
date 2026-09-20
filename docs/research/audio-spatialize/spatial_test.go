package main

// THROWAWAY PROTOTYPE - not production code. See doc.go.
//
// These are not the prototype's answer; ears are. They are the floor under it:
// a listening verdict on arithmetic that does not match the W3C spec would be a
// verdict on a bug, so the landmarks are pinned before anyone puts headphones
// on.

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

func TestAzimuthIsZeroAheadAndNinetyToTheRight(t *testing.T) {
	l := DefaultListener()
	cases := []struct {
		name    string
		at      m.Vec3
		azimuth float32
	}{
		{"straight ahead is -Z", m.Vec3{Z: -1}, 0},
		{"to the right is +X", m.Vec3{X: 1}, 90},
		{"to the left is -X", m.Vec3{X: -1}, -90},
		{"behind is +Z", m.Vec3{Z: 1}, 180},
		{"ahead and right is 45", m.Vec3{X: 1, Z: -1}, 45},
		{"behind and right is 135", m.Vec3{X: 1, Z: 1}, 135},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := azimuthElevation(c.at, l)
			if math.Abs(float64(abs32(got)-abs32(c.azimuth))) > 0.01 {
				t.Fatalf("azimuth of %v: got %.3f, want %.3f", c.at, got, c.azimuth)
			}
		})
	}
}

func TestStraightOverheadHasNinetyElevationAndZeroAzimuth(t *testing.T) {
	// The projection onto the horizontal plane vanishes, so the spec defines
	// azimuth as 0. This is the case the ticket sends you to listen to.
	azimuth, elevation := azimuthElevation(m.Vec3{Y: 3}, DefaultListener())
	if azimuth != 0 {
		t.Errorf("azimuth overhead: got %.3f, want 0", azimuth)
	}
	if math.Abs(float64(elevation-90)) > 0.01 {
		t.Errorf("elevation overhead: got %.3f, want 90", elevation)
	}
}

func TestEqualPowerPanningHoldsItsPowerAcrossTheWholeSweep(t *testing.T) {
	// The defining property of equal-power panning, and the one that says there
	// is no hole in the middle. If this drifts, a source crossing the centre
	// gets quieter or louder and no amount of listening will fix it.
	for a := float32(-180); a <= 180; a += 0.5 {
		mat := panMatrix(a, 1)
		power := mat[0][0]*mat[0][0] + mat[0][1]*mat[0][1]
		if math.Abs(float64(power-1)) > 1e-5 {
			t.Fatalf("azimuth %.1f: L^2+R^2 = %.6f, want 1", a, power)
		}
	}
}

func TestPanningFoldsFrontToBackSoBehindSoundsLikeInFront(t *testing.T) {
	// Not a bug: two speakers cannot say "behind", and W3C folds rather than
	// inventing a cue. The test pins it so the listening note has something to
	// point at.
	for _, d := range []float32{1, 5, 30, 89} {
		front := panMatrix(90-d, 1)
		behind := panMatrix(90+d, 1)
		if math.Abs(float64(front[0][0]-behind[0][0])) > 1e-6 ||
			math.Abs(float64(front[0][1]-behind[0][1])) > 1e-6 {
			t.Fatalf("%.0f deg either side of the right ear pans differently: %v vs %v",
				d, front, behind)
		}
	}
}

func TestAStereoSourceNeedsAllFourMatrixEntries(t *testing.T) {
	// #376's claim. If a scalar pan could express this, the off-diagonal would
	// always be zero, and it is not.
	left := panMatrix(-45, 2)
	right := panMatrix(45, 2)
	if left[1][0] == 0 {
		t.Errorf("panned left, the right source channel should bleed into the left output: %v", left)
	}
	if right[0][1] == 0 {
		t.Errorf("panned right, the left source channel should bleed into the right output: %v", right)
	}
	if left[0][0] != 1 {
		t.Errorf("panned left, the left source channel should pass at unity: %v", left)
	}
	if right[1][1] != 1 {
		t.Errorf("panned right, the right source channel should pass at unity: %v", right)
	}
}

func TestInverseDistanceHalvesAtTwiceTheReferenceDistance(t *testing.T) {
	f := DefaultFalloff()
	for _, c := range []struct{ distance, gain float32 }{
		{0, 1},    // inside refDistance nothing happens at all
		{0.5, 1},  //
		{1, 1},    // at refDistance
		{2, 0.5},  //
		{4, 0.25}, //
		{11, 1.0 / 11},
	} {
		if got := f.gain(c.distance); math.Abs(float64(got-c.gain)) > 1e-5 {
			t.Errorf("inverse at %.2f: got %.5f, want %.5f", c.distance, got, c.gain)
		}
	}
}

func TestTheDefaultConeIsSilentAboutDirection(t *testing.T) {
	c := DefaultCone()
	for _, at := range []m.Vec3{{X: 1}, {X: -1}, {Z: 1}, {Z: -1}, {Y: 1}} {
		if g := c.gain(at, m.Vec3{Z: -1}, m.Vec3{}); g != 1 {
			t.Errorf("default cone at %v: got %.3f, want 1", at, g)
		}
	}
}

func TestAConeAttenuatesBehindItselfAndNotInFront(t *testing.T) {
	c := Cone{InnerAngle: 60, OuterAngle: 180, OuterGain: 0.1}
	// Source at the origin pointing at -Z; the listener is where the cone looks.
	facing := m.Vec3{Z: -1}
	if g := c.gain(m.Vec3{}, facing, m.Vec3{Z: -5}); g != 1 {
		t.Errorf("inside the inner cone: got %.3f, want 1", g)
	}
	if g := c.gain(m.Vec3{}, facing, m.Vec3{Z: 5}); g != 0.1 {
		t.Errorf("outside the outer cone: got %.3f, want the outer gain 0.1", g)
	}
	// 45 degrees off the axis: past the inner half-angle of 30, short of the
	// outer half-angle of 90, so it is on the ramp between them.
	side := c.gain(m.Vec3{}, facing, m.Vec3{X: 5, Z: -5})
	if side <= 0.1 || side >= 1 {
		t.Errorf("between the cones: got %.3f, want something strictly between 0.1 and 1", side)
	}
}

func TestANonPositionalVoiceIsCentredAndUnattenuated(t *testing.T) {
	s := Spatialize(Emitter{Volume: 1}, DefaultListener(), 1)
	if math.Abs(float64(s.Matrix[0][0]-s.Matrix[0][1])) > 1e-6 {
		t.Errorf("music should sit in the middle: %v", s.Matrix)
	}
	power := s.Matrix[0][0]*s.Matrix[0][0] + s.Matrix[0][1]*s.Matrix[0][1]
	if math.Abs(float64(power-1)) > 1e-5 {
		t.Errorf("music should be at full power: L^2+R^2 = %.5f", power)
	}
}

// TestTheTwoDPlaneHardPansEverySpriteUnlessTheListenerStandsBack is the finding
// ScenarioMouse2D exists to produce, pinned as a test because it is a fact
// about the model rather than a matter of taste.
func TestTheTwoDPlaneHardPansEverySpriteUnlessTheListenerStandsBack(t *testing.T) {
	l := DefaultListener()
	for _, at := range []m.Vec3{{X: 1, Y: 0}, {X: 1, Y: 5}, {X: 1, Y: -9}, {X: 0.01, Y: 3}} {
		azimuth, _ := azimuthElevation(at, l)
		if math.Abs(float64(azimuth)-90) > 1e-3 {
			t.Fatalf("a sprite at %v on the Z=0 plane: azimuth %.3f, expected a hard +90", at, azimuth)
		}
	}
	// Standing the listener back along +Z is what recovers a usable azimuth.
	back := DefaultListener()
	back.Position = m.Vec3{Z: 6}
	azimuth, _ := azimuthElevation(m.Vec3{X: 1, Y: 0}, back)
	if azimuth >= 45 {
		t.Fatalf("with the listener 6 back, azimuth should be modest: got %.3f", azimuth)
	}
}
