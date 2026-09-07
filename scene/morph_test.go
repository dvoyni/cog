package scene

import (
	"math"
	"testing"
)

// morphAnim is a resident animation with a weight grid a test can address
// directly: the rest row, then one clip of two frames.
func morphAnim() *residentAnimation {
	return &residentAnimation{
		sampleRate: testSampleRate,
		slotCount:  3,
		clips: []bakedClip{{
			name: "smile", duration: 1.0 / testSampleRate, frames: 2, weightBase: 3,
		}},
		weights: []float32{
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
	got := blendMorphWeights(morphAnim(), "m.glb", nil, nil, nil, false, nil, report)
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

// Morphing is linear in the weights, so the two-frame lerp and the weighted
// mean across plays are one accumulation against the same folded weights the
// pose blend uses - and the result is exactly what morphing per play and
// blending the results would give.
func TestBlendMorphWeightsFoldsTheFramePairAndThePlays(t *testing.T) {
	report, _ := collectReports()
	anim := morphAnim()
	plays, frames := resolvePlays(anim, "m.glb", []ClipPlay{
		// Halfway between the clip's two frames.
		{Clip: "smile", Time: 0.5 / testSampleRate, Weight: 1},
	}, nil, nil, report)
	if len(plays) != 1 || len(frames) != 1 {
		t.Fatalf("resolved %d plays and %d frame pairs, want one of each", len(plays), len(frames))
	}
	if frames[0].row0 != 3 || frames[0].row1 != 6 {
		t.Fatalf("weight rows = %d/%d, want the clip's two rows", frames[0].row0, frames[0].row1)
	}
	got := blendMorphWeights(anim, "m.glb", plays, frames, nil, false, nil, report)
	for slot, want := range []float32{0.5, 0.5, 0} {
		if math.Abs(float64(got[slot]-want)) > 1e-5 {
			t.Errorf("slot %d = %v, want %v", slot, got[slot], want)
		}
	}
}

// A non-nil MorphWeights overrides the animated result wholesale. A short slice
// leaves the rest at 0 rather than at the file's defaults - the caller said
// what the shape is, and a half-answer merged with the animation would be a
// third thing nobody asked for.
func TestBlendMorphWeightsOverridesWholesale(t *testing.T) {
	report, keys := collectReports()
	anim := morphAnim()
	plays, frames := resolvePlays(anim, "m.glb", []ClipPlay{
		{Clip: "smile", Weight: 1},
	}, nil, nil, report)
	got := blendMorphWeights(anim, "m.glb", plays, frames, []float32{0.75}, true, nil, report)
	for slot, want := range []float32{0.75, 0, 0} {
		if got[slot] != want {
			t.Errorf("slot %d = %v, want %v", slot, got[slot], want)
		}
	}
	if len(*keys) != 0 {
		t.Errorf("reported %v; a short slice is not an error", *keys)
	}
}

// A long slice ignores the tail and reports once per model: MorphWeights is
// positional, so a caller whose array outlives an edit to the file should lose
// the shapes that went away rather than the model.
func TestBlendMorphWeightsReportsAnOverLongOverride(t *testing.T) {
	report, keys := collectReports()
	got := blendMorphWeights(morphAnim(), "m.glb", nil, nil,
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

// The list is sparse because the CPU knows which entries are non-zero before it
// writes anything: a 52-shape face with five active shapes costs 40 bytes
// instead of 256, and the shader's zero-skip branch disappears entirely.
func TestSelectMorphTargetsCullsByMagnitude(t *testing.T) {
	report, keys := collectReports()
	// A negative weight is meaningful - glTF does not clamp weights to [0, 1] -
	// so the cull is by absolute value and keeps it.
	got := selectMorphTargets([]float32{0, 0.5, 1e-6, -0.25}, nil, "m.glb", report)
	if len(got) != 2 {
		t.Fatalf("kept %v, want the two targets above the tolerance", got)
	}
	if got[0] != (sceneMorphWeight{Target: 1, Weight: 0.5}) {
		t.Errorf("entry 0 = %+v, want target 1 at 0.5", got[0])
	}
	if got[1] != (sceneMorphWeight{Target: 3, Weight: -0.25}) {
		t.Errorf("entry 1 = %+v, want target 3 at -0.25: a negative weight is a shape", got[1])
	}
	if len(*keys) != 0 {
		t.Errorf("reported %v; culling zeros is the ordinary path", *keys)
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
	got := selectMorphTargets(weights, nil, "m.glb", report)
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

// The block is a two-vec4 header, one vec4 per play, then the sparse list two
// entries to a vec4 - and the arena stays whole vec4s, because animOffset
// counts them and an odd target count would leave the next block at an offset
// no instance can name.
func TestPackAnimLaysTheMorphListOutInWholeVec4s(t *testing.T) {
	var build frameBuild
	block := morphBlock{
		binding: morphBinding{base: 7, stride: 2, targetStride: 12, targets: 3},
		targets: []sceneMorphWeight{{Target: 0, Weight: 1}, {Target: 2, Weight: 0.5}},
	}
	// A morphed draw with no plays still packs a block: playCount and
	// targetCount are independently zero-checkable, with no flags bitfield.
	first := build.packAnim(nil, block)
	if first != 0 {
		t.Fatalf("the first block is at %d, want the start of the arena", first)
	}
	// Two entries fill one vec4 exactly, so the second block follows the
	// header plus one.
	if want := uint32(animHeaderVec4s + 1); build.packAnim(nil, block) != want {
		t.Errorf("the second block is not at %d", want)
	}
	// An odd count pads: three entries are two vec4s, the last half empty.
	odd := morphBlock{
		binding: block.binding,
		targets: append(block.targets, sceneMorphWeight{Target: 1, Weight: 0.25}),
	}
	third := build.packAnim(nil, odd)
	if want := uint32(2 * (animHeaderVec4s + 1)); third != want {
		t.Errorf("the third block is at %d, want %d", third, want)
	}
	// Two blocks of header plus one vec4, then one of header plus two.
	if got, want := len(build.anims.bytes()), (2*(animHeaderVec4s+1)+animHeaderVec4s+2)*16; got != want {
		t.Errorf("the arena is %d bytes, want %d", got, want)
	}
}
