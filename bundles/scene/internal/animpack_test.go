package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// testAnim is a two-joint model with two clips a second long, which at the
// default rate is 61 frames each.
func testAnim() *ResidentAnimation {
	joints := 2
	return &ResidentAnimation{
		JointCount: joints,
		SampleRate: testSampleRate,
		JointNames: []string{"root", "tip"},
		Clips: []BakedClip{
			{Name: "walk", Duration: 1, Frames: 61, Base: joints},
			{Name: "run", Duration: 1, Frames: 61, Base: joints * 62},
		},
	}
}

// collectReports gathers what the pack path reported, keyed the way the Lookup
// would key it, so a test can assert the key as well as the error.
func collectReports() (ReportOnce, *[]string) {
	var keys []string
	return func(key string, _ error) { keys = append(keys, key) }, &keys
}

// A play names a clip and a time; the CPU turns that into two rows and two
// scalars, so the shader does no clip-length or wrap arithmetic at all.
func TestResolvePlaysFoldsTheFramePairAndItsWeights(t *testing.T) {
	report, _ := collectReports()
	anim := testAnim()
	// A quarter of a frame past frame 12, so the fraction is observable.
	plays, _ := ResolvePlays(anim, "m.glb", []ClipPlay{
		{Clip: "walk", Time: 12.25 / testSampleRate, Weight: 1},
	}, nil, nil, report)
	if len(plays) != 1 {
		t.Fatalf("plays = %d, want one", len(plays))
	}
	got := plays[0]
	if want := uint32(anim.Clips[0].Base + 12*anim.JointCount); got.BaseRow0 != want {
		t.Errorf("BaseRow0 = %d, want %d", got.BaseRow0, want)
	}
	if want := uint32(anim.Clips[0].Base + 13*anim.JointCount); got.BaseRow1 != want {
		t.Errorf("BaseRow1 = %d, want %d", got.BaseRow1, want)
	}
	if math.Abs(float64(got.W0)-0.75) > 1e-5 || math.Abs(float64(got.W1)-0.25) > 1e-5 {
		t.Errorf("weights = %v/%v, want 0.75/0.25", got.W0, got.W1)
	}
}

// Loop is on the play rather than on the caller's time, because the wrap can
// only be built by whoever knows the clip wraps - and the modulus is what makes
// negative time legal and a reversed animation free.
func TestResolvePlaysWrapsOrClampsByLoop(t *testing.T) {
	report, _ := collectReports()
	anim := testAnim()
	base := uint32(anim.Clips[0].Base)
	for _, sample := range []struct {
		name string
		play ClipPlay
		row0 uint32
	}{
		{"looped past the end", ClipPlay{Clip: "walk", Time: 1.5, Loop: true, Weight: 1},
			base + uint32(30*anim.JointCount)},
		{"looped before the start", ClipPlay{Clip: "walk", Time: -0.5, Loop: true, Weight: 1},
			base + uint32(30*anim.JointCount)},
		{"clamped past the end", ClipPlay{Clip: "walk", Time: 99, Weight: 1},
			base + uint32(60*anim.JointCount)},
		{"clamped before the start", ClipPlay{Clip: "walk", Time: -99, Weight: 1}, base},
	} {
		t.Run(sample.name, func(t *testing.T) {
			plays, _ := ResolvePlays(anim, "m.glb", []ClipPlay{sample.play}, nil, nil, report)
			if len(plays) != 1 {
				t.Fatalf("plays = %d, want one", len(plays))
			}
			if plays[0].BaseRow0 != sample.row0 {
				t.Errorf("BaseRow0 = %d, want %d", plays[0].BaseRow0, sample.row0)
			}
		})
	}
}

// A clamped play at the very end must not address a row past the clip: the
// grid's last frame has no successor, so both halves of the pair land on it.
func TestResolvePlaysKeepsTheClampedPairInsideTheClip(t *testing.T) {
	report, _ := collectReports()
	anim := testAnim()
	plays, _ := ResolvePlays(anim, "m.glb", []ClipPlay{
		{Clip: "walk", Time: 99, Weight: 1},
	}, nil, nil, report)
	last := uint32(anim.Clips[0].Base + 60*anim.JointCount)
	if plays[0].BaseRow0 != last || plays[0].BaseRow1 != last {
		t.Errorf("pair = %d/%d, want both on the last frame %d",
			plays[0].BaseRow0, plays[0].BaseRow1, last)
	}
}

// A fifth play is dropped by lowest weight, not by position, so which four
// survive does not depend on the order the caller listed them.
func TestResolvePlaysDropsTheLightestOverTheCap(t *testing.T) {
	report, keys := collectReports()
	anim := testAnim()
	anim.Clips = append(anim.Clips,
		BakedClip{Name: "idle", Duration: 1, Frames: 61, Base: anim.JointCount * 123},
		BakedClip{Name: "jump", Duration: 1, Frames: 61, Base: anim.JointCount * 184},
		BakedClip{Name: "fall", Duration: 1, Frames: 61, Base: anim.JointCount * 245},
	)
	plays, _ := ResolvePlays(anim, "m.glb", []ClipPlay{
		{Clip: "walk", Weight: 0.1},
		{Clip: "run", Weight: 4},
		{Clip: "idle", Weight: 3},
		{Clip: "jump", Weight: 2},
		{Clip: "fall", Weight: 1},
	}, nil, nil, report)
	if len(plays) != maxClipPlays {
		t.Fatalf("plays = %d, want the cap of %d", len(plays), maxClipPlays)
	}
	if len(*keys) != 1 || (*keys)[0] != "model:m.glb#plays" {
		t.Errorf("reported %v, want one report per model", *keys)
	}
	// walk at 0.1 is the lightest and is the one displaced, leaving 4+3+2+1.
	for _, play := range plays {
		if math.Abs(float64(play.W0)-0.1/10.0) < 1e-6 {
			t.Error("the lightest play survived; the cap drops by weight, not by position")
		}
	}
}

// A skinned draw with no plays reads the rest frame, and BlendJoint is the
// CPU's side of that - the one thing the shader cannot do for the re-root.
func TestBlendJointReadsTheRestFrameWithNoPlays(t *testing.T) {
	anim := testAnim()
	anim.poseRows = []scenePose{
		restPose(),
		{Rotation: m.Vec4{W: 1}, Translation: m.Vec4{Y: 5}, Scale: m.Vec4{X: 1, Y: 1, Z: 1}},
	}
	got, ok := BlendJoint(anim, nil, 1)
	if !ok {
		t.Fatal("the rest frame is always available")
	}
	if want := (m.Vec3{Y: 5}); got.Translation() != want {
		t.Errorf("rest translation = %v, want %v", got.Translation(), want)
	}
}

// Blending on the CPU is the same weighted mean the shader takes, so a
// half-and-half blend of two rows lands halfway between them.
func TestBlendJointTakesTheWeightedMean(t *testing.T) {
	anim := &ResidentAnimation{JointCount: 1, SampleRate: testSampleRate}
	anim.poseRows = []scenePose{
		restPose(),
		{Rotation: m.Vec4{W: 1}, Translation: m.Vec4{X: 2}, Scale: m.Vec4{X: 1, Y: 1, Z: 1}},
		{Rotation: m.Vec4{W: 1}, Translation: m.Vec4{X: 6}, Scale: m.Vec4{X: 1, Y: 1, Z: 1}},
	}
	got, ok := BlendJoint(anim, []ScenePlayRecord{{BaseRow0: 1, BaseRow1: 2, W0: 0.5, W1: 0.5}}, 0)
	if !ok {
		t.Fatal("a play over real rows blends")
	}
	if want := (m.Vec3{X: 4}); got.Translation() != want {
		t.Errorf("blended translation = %v, want %v", got.Translation(), want)
	}
}

// restPose is a pose row that transforms nothing, which several blends here use
// as the row they measure another against.
func restPose() scenePose {
	return scenePose{Rotation: m.Vec4{W: 1}, Scale: m.Vec4{X: 1, Y: 1, Z: 1}}
}
