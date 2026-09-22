package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
)

// morphAnim is a resident animation with a weight grid a test can address
// directly: the rest row, then one clip of two frames.
func morphAnim() *model.ResidentAnimation {
	return &model.ResidentAnimation{
		SampleRate: testSampleRate,
		SlotCount:  3,
		Clips: []model.BakedClip{{
			Name: "smile", Duration: 1.0 / testSampleRate, Frames: 2, WeightBase: 3,
		}},
		Weights: []float32{
			0.1, 0.2, 0.3, // rest
			1, 0, 0, // clip frame 0
			0, 1, 0, // clip frame 1
		},
	}
}

// A draw with no plays reads the rest row, which is node.weights over
// mesh.weights over zero, resolved once at load.
func TestBlendMorphWeightsFallsBackToTheRestRow(t *testing.T) {
	report, keys := collectReports()
	got := model.BlendMorphWeights(morphAnim(), "m.glb", nil, nil, nil, false, nil, report)
	want := []float32{0.1, 0.2, 0.3}
	for slot, expected := range want {
		if got[slot] != expected {
			t.Errorf("slot %d = %v, want the authored default %v", slot, got[slot], expected)
		}
	}
	if len(*keys) != 0 {
		t.Errorf("reported %v; a draw taking its defaults is not a failure", *keys)
	}
}

// A non-nil MorphWeights overrides the animated result wholesale. A short slice
// leaves the rest at 0 rather than at the file's defaults - the caller said
// what the shape is, and a half-answer merged with the animation would be a
// third thing nobody asked for.
func TestBlendMorphWeightsOverridesWholesale(t *testing.T) {
	report, keys := collectReports()
	anim := morphAnim()
	plays, frames := model.ResolvePlays(anim, "m.glb", []scene.ClipPlay{
		{Clip: "smile", Weight: 1},
	}, nil, nil, report)
	got := model.BlendMorphWeights(anim, "m.glb", plays, frames, []float32{0.75}, true, nil, report)
	for slot, want := range []float32{0.75, 0, 0} {
		if got[slot] != want {
			t.Errorf("slot %d = %v, want %v", slot, got[slot], want)
		}
	}
	if len(*keys) != 0 {
		t.Errorf("reported %v; a short slice is not an error", *keys)
	}
}

// The list is sparse because the CPU knows which entries are non-zero before it
// writes anything: a 52-shape face with five active shapes costs 40 bytes
// instead of 256, and the shader's zero-skip branch disappears entirely.
func TestSelectMorphTargetsCullsByMagnitude(t *testing.T) {
	report, keys := collectReports()
	// A negative weight is meaningful - glTF does not clamp weights to [0, 1] -
	// so the cull is by absolute value and keeps it.
	got := model.SelectMorphTargets([]float32{0, 0.5, 1e-6, -0.25}, nil, "m.glb", report)
	if len(got) != 2 {
		t.Fatalf("kept %v, want the two targets above the tolerance", got)
	}
	if got[0] != (model.SceneMorphWeight{Target: 1, Weight: 0.5}) {
		t.Errorf("entry 0 = %+v, want target 1 at 0.5", got[0])
	}
	if got[1] != (model.SceneMorphWeight{Target: 3, Weight: -0.25}) {
		t.Errorf("entry 1 = %+v, want target 3 at -0.25: a negative weight is a shape", got[1])
	}
	if len(*keys) != 0 {
		t.Errorf("reported %v; culling zeros is the ordinary path", *keys)
	}
}

// The block is a two-vec4 header, one vec4 per play, then the sparse list two
// entries to a vec4 - and the arena stays whole vec4s, because animOffset
// counts them and an odd target count would leave the next block at an offset
// no instance can name.
func TestPackAnimLaysTheMorphListOutInWholeVec4s(t *testing.T) {
	var build frameBuild
	block := morphBlock{
		binding: model.MorphBinding{Base: 7, Stride: 2, Targets: 3},
		targets: []model.SceneMorphWeight{{Target: 0, Weight: 1}, {Target: 2, Weight: 0.5}},
	}
	// A morphed draw with no plays still packs a block: playCount and
	// targetCount are independently zero-checkable, with no flags bitfield.
	first := build.packAnim(nil, block)
	if first != 0 {
		t.Fatalf("the first block is at %d, want the start of the arena", first)
	}
	// Two entries fill one vec4 exactly, so the second block follows the
	// header plus one.
	if want := uint32(model.AnimHeaderVec4s + 1); build.packAnim(nil, block) != want {
		t.Errorf("the second block is not at %d", want)
	}
	// An odd count pads: three entries are two vec4s, the last half empty.
	odd := morphBlock{
		binding: block.binding,
		targets: append(block.targets, model.SceneMorphWeight{Target: 1, Weight: 0.25}),
	}
	third := build.packAnim(nil, odd)
	if want := uint32(2 * (model.AnimHeaderVec4s + 1)); third != want {
		t.Errorf("the third block is at %d, want %d", third, want)
	}
	// Two blocks of header plus one vec4, then one of header plus two.
	if got, want := len(build.anims.bytes()), (2*(model.AnimHeaderVec4s+1)+model.AnimHeaderVec4s+2)*16; got != want {
		t.Errorf("the arena is %d bytes, want %d", got, want)
	}
}
