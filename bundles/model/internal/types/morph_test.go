package types

import (
	"math"
	"testing"
)

// morphAnim is a resident animation with a weight grid a test can address
// directly: the rest row, then one clip of two frames.
func morphAnim() *ResidentAnimation {
	return &ResidentAnimation{
		SampleRate: testSampleRate,
		SlotCount:  3,
		Clips: []BakedClip{{
			Name: "smile", Duration: 1.0 / testSampleRate, Frames: 2, WeightBase: 3,
		}},
		Weights: []float32{
			0.1, 0.2, 0.3, // rest
			1, 0, 0, // clip frame 0
			0, 1, 0, // clip frame 1
		},
	}
}

// Morphing is linear in the weights, so the two-frame lerp and the weighted
// mean across plays are one accumulation against the same folded weights the
// pose blend uses - and the result is exactly what morphing per play and
// blending the results would give.
func TestBlendMorphWeightsFoldsTheFramePairAndThePlays(t *testing.T) {
	report, _ := collectReports()
	anim := morphAnim()
	plays, frames := ResolvePlays(anim, "m.glb", []ClipPlay{
		// Halfway between the clip's two frames.
		{Clip: "smile", Time: 0.5 / testSampleRate, Weight: 1},
	}, nil, nil, report)
	if len(plays) != 1 || len(frames) != 1 {
		t.Fatalf("resolved %d plays and %d frame pairs, want one of each", len(plays), len(frames))
	}
	if frames[0].row0 != 3 || frames[0].row1 != 6 {
		t.Fatalf("weight rows = %d/%d, want the clip's two rows", frames[0].row0, frames[0].row1)
	}
	got := BlendMorphWeights(anim, "m.glb", plays, frames, nil, false, nil, report)
	for slot, want := range []float32{0.5, 0.5, 0} {
		if math.Abs(float64(got[slot]-want)) > 1e-5 {
			t.Errorf("slot %d = %v, want %v", slot, got[slot], want)
		}
	}
}

// A long slice ignores the tail and reports once per model: MorphWeights is
// positional, so a caller whose array outlives an edit to the file should lose
// the shapes that went away rather than the model.
func TestBlendMorphWeightsReportsAnOverLongOverride(t *testing.T) {
	report, keys := collectReports()
	got := BlendMorphWeights(morphAnim(), "m.glb", nil, nil,
		[]float32{1, 2, 3, 4, 5}, true, nil, report)
	if len(got) != 3 {
		t.Fatalf("blended %d weights, want the model's three slots", len(got))
	}
	if got[2] != 3 {
		t.Errorf("slot 2 = %v, want 3", got[2])
	}
	if len(*keys) != 1 || (*keys)[0] != morphWeightsReportKey("m.glb") {
		t.Errorf("reported %v, want one report under the morph-weights key", *keys)
	}
}

// Stored targets are unlimited and active ones are capped, because with sparse
// packing the cap constrains neither memory nor layout - it is purely a guard
// against runaway per-vertex ALU. Over it, the lightest are dropped, so which
// targets survive does not depend on the order the file listed them.
func TestSelectMorphTargetsCapsByWeight(t *testing.T) {
	report, keys := collectReports()
	weights := make([]float32, maxMorphTargets+10)
	for i := range weights {
		// Ascending, so the ones the file listed first are the ones to drop.
		weights[i] = float32(i+1) / 100
	}
	got := SelectMorphTargets(weights, nil, "m.glb", report)
	if len(got) != maxMorphTargets {
		t.Fatalf("kept %d targets, want the cap of %d", len(got), maxMorphTargets)
	}
	lightest := got[0]
	for _, entry := range got {
		if abs(entry.Weight) < abs(lightest.Weight) {
			lightest = entry
		}
	}
	if want := uint32(10); lightest.Target != want {
		t.Errorf("the lightest survivor is target %d, want %d: the first ten are the lightest",
			lightest.Target, want)
	}
	if len(*keys) != 1 || (*keys)[0] != morphTargetsReportKey("m.glb") {
		t.Errorf("reported %v, want one report under the morph-targets key", *keys)
	}
}
