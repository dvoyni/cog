package scene

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/m"
)

// testAnim is a two-joint model with two clips a second long, which at the
// default rate is 61 frames each.
func testAnim() *residentAnimation {
	joints := 2
	return &residentAnimation{
		jointCount: joints,
		sampleRate: testSampleRate,
		jointNames: []string{"root", "tip"},
		clips: []bakedClip{
			{name: "walk", duration: 1, frames: 61, base: joints},
			{name: "run", duration: 1, frames: 61, base: joints * 62},
		},
	}
}

// collectReports gathers what the pack path reported, keyed the way the Lookup
// would key it, so a test can assert the key as well as the error.
func collectReports() (reportOnce, *[]string) {
	var keys []string
	return func(key string, _ error) { keys = append(keys, key) }, &keys
}

// A play names a clip and a time; the CPU turns that into two rows and two
// scalars, so the shader does no clip-length or wrap arithmetic at all.
func TestResolvePlaysFoldsTheFramePairAndItsWeights(t *testing.T) {
	report, _ := collectReports()
	anim := testAnim()
	// A quarter of a frame past frame 12, so the fraction is observable.
	plays, _ := resolvePlays(anim, "m.glb", []ClipPlay{
		{Clip: "walk", Time: 12.25 / testSampleRate, Weight: 1},
	}, nil, nil, report)
	if len(plays) != 1 {
		t.Fatalf("plays = %d, want one", len(plays))
	}
	got := plays[0]
	if want := uint32(anim.clips[0].base + 12*anim.jointCount); got.BaseRow0 != want {
		t.Errorf("BaseRow0 = %d, want %d", got.BaseRow0, want)
	}
	if want := uint32(anim.clips[0].base + 13*anim.jointCount); got.BaseRow1 != want {
		t.Errorf("BaseRow1 = %d, want %d", got.BaseRow1, want)
	}
	if math.Abs(float64(got.W0)-0.75) > 1e-5 || math.Abs(float64(got.W1)-0.25) > 1e-5 {
		t.Errorf("weights = %v/%v, want 0.75/0.25", got.W0, got.W1)
	}
}

// The blend is a weighted mean of TRS, not an additive layer: weights summing
// to 0.5 would not half-apply the animation, they would shrink every bone
// toward the origin. So there is no legitimate non-unit sum, and "as given"
// would preserve only the ability to express a bug.
func TestResolvePlaysNormalisesWeights(t *testing.T) {
	report, _ := collectReports()
	plays, _ := resolvePlays(testAnim(), "m.glb", []ClipPlay{
		{Clip: "walk", Weight: 3},
		{Clip: "run", Weight: 1},
	}, nil, nil, report)
	if len(plays) != 2 {
		t.Fatalf("plays = %d, want two", len(plays))
	}
	// Every play is at frame 0, so W0 carries the whole normalised weight.
	if math.Abs(float64(plays[0].W0)-0.75) > 1e-6 || math.Abs(float64(plays[1].W0)-0.25) > 1e-6 {
		t.Errorf("weights = %v and %v, want 0.75 and 0.25", plays[0].W0, plays[1].W0)
	}
	var total float32
	for _, play := range plays {
		total += play.W0 + play.W1
	}
	if math.Abs(float64(total)-1) > 1e-6 {
		t.Errorf("weights total %v, want one", total)
	}
}

// A total of about zero falls back to the rest frame rather than dividing by
// it, which is one of the three things row 0 answers.
func TestResolvePlaysFallsBackToTheRestFrameOnZeroWeight(t *testing.T) {
	report, _ := collectReports()
	plays, _ := resolvePlays(testAnim(), "m.glb", []ClipPlay{
		{Clip: "walk", Weight: 0},
		{Clip: "run", Weight: 0},
	}, nil, nil, report)
	if len(plays) != 0 {
		t.Errorf("plays = %v, want the rest frame", plays)
	}
}

// Loop is on the play rather than on the caller's time, because the wrap can
// only be built by whoever knows the clip wraps - and the modulus is what makes
// negative time legal and a reversed animation free.
func TestResolvePlaysWrapsOrClampsByLoop(t *testing.T) {
	report, _ := collectReports()
	anim := testAnim()
	base := uint32(anim.clips[0].base)
	for _, sample := range []struct {
		name string
		play ClipPlay
		row0 uint32
	}{
		{"looped past the end", ClipPlay{Clip: "walk", Time: 1.5, Loop: true, Weight: 1},
			base + uint32(30*anim.jointCount)},
		{"looped before the start", ClipPlay{Clip: "walk", Time: -0.5, Loop: true, Weight: 1},
			base + uint32(30*anim.jointCount)},
		{"clamped past the end", ClipPlay{Clip: "walk", Time: 99, Weight: 1},
			base + uint32(60*anim.jointCount)},
		{"clamped before the start", ClipPlay{Clip: "walk", Time: -99, Weight: 1}, base},
	} {
		t.Run(sample.name, func(t *testing.T) {
			plays, _ := resolvePlays(anim, "m.glb", []ClipPlay{sample.play}, nil, nil, report)
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
	plays, _ := resolvePlays(anim, "m.glb", []ClipPlay{
		{Clip: "walk", Time: 99, Weight: 1},
	}, nil, nil, report)
	last := uint32(anim.clips[0].base + 60*anim.jointCount)
	if plays[0].BaseRow0 != last || plays[0].BaseRow1 != last {
		t.Errorf("pair = %d/%d, want both on the last frame %d",
			plays[0].BaseRow0, plays[0].BaseRow1, last)
	}
}

// One typo'd clip name should cost the one play, not the character. It reports
// under a key carrying the name, so two typos in one file are two reports.
func TestResolvePlaysDropsAnUnknownClipAndReportsIt(t *testing.T) {
	report, keys := collectReports()
	plays, _ := resolvePlays(testAnim(), "m.glb", []ClipPlay{
		{Clip: "sprint", Weight: 1},
		{Clip: "walk", Weight: 1},
	}, nil, nil, report)
	if len(plays) != 1 {
		t.Fatalf("plays = %d, want the one that named a real clip", len(plays))
	}
	if len(*keys) != 1 || (*keys)[0] != "model:m.glb#clip:sprint" {
		t.Errorf("reported %v, want one key naming the missing clip", *keys)
	}
	// The survivor is normalised on its own, so dropping a play does not
	// silently half-apply the rest.
	if math.Abs(float64(plays[0].W0)-1) > 1e-6 {
		t.Errorf("the survivor weighs %v, want the whole blend", plays[0].W0)
	}
}

// A fifth play is dropped by lowest weight, not by position, so which four
// survive does not depend on the order the caller listed them.
func TestResolvePlaysDropsTheLightestOverTheCap(t *testing.T) {
	report, keys := collectReports()
	anim := testAnim()
	anim.clips = append(anim.clips,
		bakedClip{name: "idle", duration: 1, frames: 61, base: anim.jointCount * 123},
		bakedClip{name: "jump", duration: 1, frames: 61, base: anim.jointCount * 184},
		bakedClip{name: "fall", duration: 1, frames: 61, base: anim.jointCount * 245},
	)
	plays, _ := resolvePlays(anim, "m.glb", []ClipPlay{
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

// A model with no joints has nothing to play, however many plays a draw names.
func TestResolvePlaysIsEmptyForAModelWithNoJoints(t *testing.T) {
	report, keys := collectReports()
	plays, _ := resolvePlays(&residentAnimation{}, "m.glb", []ClipPlay{
		{Clip: "walk", Weight: 1},
	}, nil, nil, report)
	if len(plays) != 0 {
		t.Errorf("plays = %v, want none", plays)
	}
	if len(*keys) != 0 {
		t.Errorf("reported %v; a model with no clips at all is not a typo", *keys)
	}
}

// The block is a two-vec4 header and one vec4 per play, and animOffset counts
// vec4s from the start of the whole arena - not from a per-pass range, because
// sceneInstances is bound per pass and sceneAnim is not.
func TestPackAnimLaysTheBlockOutInVec4s(t *testing.T) {
	var build frameBuild
	if got := build.packAnim(nil, morphBlock{}); got != sceneNoAnim {
		t.Errorf("an empty block is at %d, want sceneNoAnim", got)
	}
	first := build.packAnim([]scenePlayRecord{{BaseRow0: 4, BaseRow1: 6, W0: 0.5, W1: 0.5}}, morphBlock{})
	if first != 0 {
		t.Errorf("the first block is at %d, want the start of the arena", first)
	}
	second := build.packAnim([]scenePlayRecord{{}, {}}, morphBlock{})
	if want := uint32(animHeaderVec4s + playRecordVec4s); second != want {
		t.Errorf("the second block is at %d, want %d", second, want)
	}
	if got, want := len(build.anims.bytes()), (2+1+2+2)*16; got != want {
		t.Errorf("the arena is %d bytes, want %d", got, want)
	}
}

// An instance that animates nothing carries SCENE_NOSKIN and SCENE_NO_ANIM,
// which is what skips the whole pose path for a debug line or a procedural
// terrain mesh rather than charging it a per-vertex fetch of an identity.
func TestPackInstanceMarksAnUnskinnedDraw(t *testing.T) {
	instance := packInstance(m.NewMat4(), animBinding{offset: sceneNoAnim})
	if instance.Flags&sceneNoSkin == 0 {
		t.Error("a draw with no skin of its own carries SCENE_NOSKIN")
	}
	if instance.AnimOffset != sceneNoAnim {
		t.Errorf("AnimOffset = %d, want sceneNoAnim", instance.AnimOffset)
	}
	skinned := packInstance(m.NewMat4(), animBinding{offset: 7, skinned: true})
	if skinned.Flags&sceneNoSkin != 0 {
		t.Error("a skinned draw must not carry SCENE_NOSKIN")
	}
	if skinned.AnimOffset != 7 {
		t.Errorf("AnimOffset = %d, want the block's offset", skinned.AnimOffset)
	}
}

// A skinned draw with no plays reads the rest frame, and blendJoint is the
// CPU's side of that - the one thing the shader cannot do for the re-root.
func TestBlendJointReadsTheRestFrameWithNoPlays(t *testing.T) {
	anim := testAnim()
	anim.poseRows = []scenePose{
		restPose(),
		{Rotation: m.Vec4{W: 1}, Translation: m.Vec4{Y: 5}, Scale: m.Vec4{X: 1, Y: 1, Z: 1}},
	}
	got, ok := blendJoint(anim, nil, 1)
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
	anim := &residentAnimation{jointCount: 1, sampleRate: testSampleRate}
	anim.poseRows = []scenePose{
		restPose(),
		{Rotation: m.Vec4{W: 1}, Translation: m.Vec4{X: 2}, Scale: m.Vec4{X: 1, Y: 1, Z: 1}},
		{Rotation: m.Vec4{W: 1}, Translation: m.Vec4{X: 6}, Scale: m.Vec4{X: 1, Y: 1, Z: 1}},
	}
	got, ok := blendJoint(anim, []scenePlayRecord{{BaseRow0: 1, BaseRow1: 2, W0: 0.5, W1: 0.5}}, 0)
	if !ok {
		t.Fatal("a play over real rows blends")
	}
	if want := (m.Vec3{X: 4}); got.Translation() != want {
		t.Errorf("blended translation = %v, want %v", got.Translation(), want)
	}
}

// A model that keeps no CPU pose rows - which is almost every model - answers
// no, and the caller keeps the load's rest-pose re-root.
func TestBlendJointDeclinesWithoutCPUPoseRows(t *testing.T) {
	if _, ok := blendJoint(testAnim(), nil, 0); ok {
		t.Error("a model with no CPU rows has no frame-resolved pose to give")
	}
}

// restPose is a pose row that transforms nothing, which several blends here use
// as the row they measure another against.
func restPose() scenePose {
	return scenePose{Rotation: m.Vec4{W: 1}, Scale: m.Vec4{X: 1, Y: 1, Z: 1}}
}
