package sceneimpl

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal"
	"github.com/dvoyni/cog/libs/m"
)

// testAnim is a two-joint model with two clips a second long, which at the
// default rate is 61 frames each.
func testAnim() *internal.ResidentAnimation {
	joints := 2
	return &internal.ResidentAnimation{
		JointCount: joints,
		SampleRate: testSampleRate,
		JointNames: []string{"root", "tip"},
		Clips: []internal.BakedClip{
			{Name: "walk", Duration: 1, Frames: 61, Base: joints},
			{Name: "run", Duration: 1, Frames: 61, Base: joints * 62},
		},
	}
}

// collectReports gathers what the pack path reported, keyed the way the Lookup
// would key it, so a test can assert the key as well as the error.
func collectReports() (internal.ReportOnce, *[]string) {
	var keys []string
	return func(key string, _ error) { keys = append(keys, key) }, &keys
}

// The blend is a weighted mean of TRS, not an additive layer: weights summing
// to 0.5 would not half-apply the animation, they would shrink every bone
// toward the origin. So there is no legitimate non-unit sum, and "as given"
// would preserve only the ability to express a bug.
func TestResolvePlaysNormalisesWeights(t *testing.T) {
	report, _ := collectReports()
	plays, _ := internal.ResolvePlays(testAnim(), "m.glb", []scene.ClipPlay{
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
	plays, _ := internal.ResolvePlays(testAnim(), "m.glb", []scene.ClipPlay{
		{Clip: "walk", Weight: 0},
		{Clip: "run", Weight: 0},
	}, nil, nil, report)
	if len(plays) != 0 {
		t.Errorf("plays = %v, want the rest frame", plays)
	}
}

// One typo'd clip name should cost the one play, not the character. It reports
// under a key carrying the name, so two typos in one file are two reports.
func TestResolvePlaysDropsAnUnknownClipAndReportsIt(t *testing.T) {
	report, keys := collectReports()
	plays, _ := internal.ResolvePlays(testAnim(), "m.glb", []scene.ClipPlay{
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

// A model with no joints has nothing to play, however many plays a draw names.
func TestResolvePlaysIsEmptyForAModelWithNoJoints(t *testing.T) {
	report, keys := collectReports()
	plays, _ := internal.ResolvePlays(&internal.ResidentAnimation{}, "m.glb", []scene.ClipPlay{
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
	if got := build.packAnim(nil, morphBlock{}); got != internal.SceneNoAnim {
		t.Errorf("an empty block is at %d, want sceneNoAnim", got)
	}
	first := build.packAnim([]internal.ScenePlayRecord{{BaseRow0: 4, BaseRow1: 6, W0: 0.5, W1: 0.5}}, morphBlock{})
	if first != 0 {
		t.Errorf("the first block is at %d, want the start of the arena", first)
	}
	second := build.packAnim([]internal.ScenePlayRecord{{}, {}}, morphBlock{})
	if want := uint32(internal.AnimHeaderVec4s + internal.PlayRecordVec4s); second != want {
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
	instance := packInstance(m.NewMat4(), internal.AnimBinding{Offset: internal.SceneNoAnim}, 0)
	if instance.Flags&sceneNoSkin == 0 {
		t.Error("a draw with no skin of its own carries SCENE_NOSKIN")
	}
	if instance.AnimOffset != internal.SceneNoAnim {
		t.Errorf("AnimOffset = %d, want sceneNoAnim", instance.AnimOffset)
	}
	skinned := packInstance(m.NewMat4(), internal.AnimBinding{Offset: 7, Skinned: true}, 0)
	if skinned.Flags&sceneNoSkin != 0 {
		t.Error("a skinned draw must not carry SCENE_NOSKIN")
	}
	if skinned.AnimOffset != 7 {
		t.Errorf("AnimOffset = %d, want the block's offset", skinned.AnimOffset)
	}
}

// A model that keeps no CPU pose rows - which is almost every model - answers
// no, and the caller keeps the load's rest-pose re-root.
func TestBlendJointDeclinesWithoutCPUPoseRows(t *testing.T) {
	if _, ok := internal.BlendJoint(testAnim(), nil, 0); ok {
		t.Error("a model with no CPU rows has no frame-resolved pose to give")
	}
}
