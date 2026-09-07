package scene

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/qmuntal/gltf"
)

// morphModel is the smallest morphed file: one node drawing a two-target
// triangle whose second shape starts at half weight.
func morphModel(t testing.TB) *gltf.Document {
	t.Helper()
	doc := testDoc()
	mesh := morphedMesh(doc, false, false, []gltf.PrimitiveAttributes{
		deltaTarget(doc, [][3]float32{{1, 0, 0}, {0, 0, 0}, {0, 0, 0}}, nil, nil),
		deltaTarget(doc, [][3]float32{{0, 0, 0}, {0, 2, 0}, {0, 0, 0}}, nil, nil),
	})
	doc.Meshes[mesh].Weights = []float64{0, 0.5}
	doc.Meshes[mesh].Extras = map[string]any{"targetNames": []any{"smile", "blink"}}
	doc.Nodes = []*gltf.Node{{Name: "face", Mesh: gltf.Index(mesh)}}
	sceneOf(doc, 0)
	doc.Scene = gltf.Index(0)
	return doc
}

// animBlock is one instance's sceneAnim block decoded out of the frame's arena:
// the header words the shader reads before it loops, and the sparse morph list
// behind the play records.
//
// It decodes the bytes rather than reading scene's own structs back, because
// the offsets are the contract the shader reads by and a decode through the
// writer's own types would agree with itself whatever they are.
type animBlock struct {
	playCount, targetCount uint32
	morphBase, morphStride uint32
	morphTargetStride      uint32
	targets                []sceneMorphWeight
}

func decodeAnimBlock(t *testing.T, arena []byte, offset uint32) animBlock {
	t.Helper()
	word := func(at int) uint32 {
		if at+4 > len(arena) {
			t.Fatalf("the block runs past the %d-byte arena", len(arena))
		}
		return binary.LittleEndian.Uint32(arena[at:])
	}
	base := int(offset) * 16
	block := animBlock{
		playCount:         word(base),
		targetCount:       word(base + 4),
		morphBase:         word(base + 8),
		morphStride:       word(base + 12),
		morphTargetStride: word(base + 16),
	}
	list := base + animHeaderVec4s*16 + int(block.playCount)*16
	for i := range int(block.targetCount) {
		at := list + i*8
		block.targets = append(block.targets, sceneMorphWeight{
			Target: word(at),
			Weight: math.Float32frombits(word(at + 4)),
		})
	}
	return block
}

// residentMorphModel loads one morphed file and returns the harness once it
// draws.
func residentMorphModel(t *testing.T, doc *gltf.Document, draw ModelDraw) *harness {
	t.Helper()
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), drawModel(modelPath, draw))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 1
	})
	return h
}

// A morphed model binds its own delta buffer, and everything else binds the
// null skin's single zero record. sceneMorphDeltas is declared on the one
// module every draw goes through, and a declared binding that nothing binds
// does not degrade the frame - it takes the whole command buffer down silently.
func TestAMorphedModelBindsItsOwnDeltasAndABoxBindsTheNullOne(t *testing.T) {
	h := residentMorphModel(t, morphModel(t), ModelDraw{})
	// Two targets over three vertices at one slot each.
	if got, want := len(boundBytes(t, h, "sceneMorphDeltas")), 2*3*morphRecordSize; got != want {
		t.Errorf("the bound delta buffer is %d bytes, want the model's %d", got, want)
	}
	box := newHarness(t, func(q *OpQueue) {
		q.Camera(cameraMain, testCameraDescr())
		q.Box(0, At(0, 0, 0), testBoxColor)
	})
	box.frame()
	if got := len(boundBytes(t, box, "sceneMorphDeltas")); got != morphRecordSize {
		t.Errorf("a debug box binds %d bytes of deltas, want the null skin's one record", got)
	}
}

// A morph-only model has no joints and no pose row anywhere, and it must still
// carry a live animOffset: the null-skin fill answers the two halves of group 2
// separately, and clearing the offset because there are no poses would silently
// unmorph the model.
func TestAMorphOnlyModelKeepsItsAnimBlock(t *testing.T) {
	h := residentMorphModel(t, morphModel(t), ModelDraw{})
	instance := firstInstance(t, h)
	if instance.Flags&sceneNoSkin == 0 {
		t.Error("a model with no joints carries SCENE_NOSKIN, morphed or not")
	}
	if instance.AnimOffset == sceneNoAnim {
		t.Fatal("AnimOffset = sceneNoAnim; a morphed draw reads a block whatever its skin does")
	}
	block := decodeAnimBlock(t, boundBytes(t, h, "sceneAnim"), instance.AnimOffset)
	// Two counts, independently zero-checkable, with no flags bitfield.
	if block.playCount != 0 {
		t.Errorf("playCount = %d, want none: the file has no clips", block.playCount)
	}
	// One target survives the cull: the file's defaults are 0 and 0.5.
	if block.targetCount != 1 {
		t.Fatalf("targetCount = %d, want the one non-zero default", block.targetCount)
	}
	if got := block.targets[0]; got.Target != 1 || got.Weight != 0.5 {
		t.Errorf("target = %+v, want target 1 at the mesh's authored 0.5", got)
	}
	// The addressing constants are the primitive's, folded on the CPU: one
	// slot per vertex, three vertices to a target.
	if block.morphBase != 0 || block.morphStride != 1 || block.morphTargetStride != 3 {
		t.Errorf("addressing = base %d stride %d targetStride %d, want 0/1/3",
			block.morphBase, block.morphStride, block.morphTargetStride)
	}
}

// MorphWeights overrides the animated result wholesale, and it is positional
// over the model's flattened target list rather than named: naming lives on the
// lookup facade so that this path is a memcpy.
func TestMorphWeightsOverrideReachesThePackedList(t *testing.T) {
	h := residentMorphModel(t, morphModel(t), ModelDraw{MorphWeights: []float32{0.25}})
	instance := firstInstance(t, h)
	block := decodeAnimBlock(t, boundBytes(t, h, "sceneAnim"), instance.AnimOffset)
	// The override wins whole: target 1's authored 0.5 is gone, not merged.
	if block.targetCount != 1 {
		t.Fatalf("targetCount = %d, want the one weight the caller gave", block.targetCount)
	}
	if got := block.targets[0]; got.Target != 0 || got.Weight != 0.25 {
		t.Errorf("target = %+v, want target 0 at the caller's 0.25", got)
	}
}

// A caller asking for every target at zero is not the same as a caller asking
// for the file's defaults, so an empty but non-nil MorphWeights is an override
// rather than an absence - and every weight at zero leaves nothing to blend, so
// the draw carries no block at all.
func TestAnEmptyMorphWeightsIsAnOverrideRatherThanAnAbsence(t *testing.T) {
	h := residentMorphModel(t, morphModel(t), ModelDraw{MorphWeights: []float32{}})
	if got := firstInstance(t, h).AnimOffset; got != sceneNoAnim {
		t.Errorf("AnimOffset = %d, want sceneNoAnim: every target was asked for at zero", got)
	}
}

// A file with no shapes packs no morph list and reads no delta, which is what
// makes morph targets cost a static prop and a plain rig nothing.
func TestAModelWithNoShapesPacksNoMorphList(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, onePrimitiveModel(t))),
		drawModel(modelPath, ModelDraw{}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 1
	})
	if got := firstInstance(t, h).AnimOffset; got != sceneNoAnim {
		t.Errorf("AnimOffset = %d, want sceneNoAnim for a model with no animation at all", got)
	}
	if got := len(boundBytes(t, h, "sceneMorphDeltas")); got != morphRecordSize {
		t.Errorf("a static model binds %d bytes of deltas, want the null skin's one record", got)
	}
	h.lookup(func(access LookupAccess) {
		if got, _ := access.MorphBytes(modelPath); got != 0 {
			t.Errorf("MorphBytes = %d, want none for a file with no targets", got)
		}
	})
}

// The names are what a caller reads at startup to build the index mapping
// MorphWeights is positional over, and the byte counts are what makes delta
// memory a measured number rather than a guess.
func TestTheLookupReportsMorphTargetsAndBytes(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, morphModel(t))), func(*OpQueue) {})
	var names []string
	var bytes int
	h.frameUntil(t, "the model to become resident", func() bool {
		h.lookup(func(access LookupAccess) {
			names, _ = access.MorphTargets(modelPath, names[:0])
			bytes, _ = access.MorphBytes(modelPath)
		})
		return len(names) > 0
	})
	if len(names) != 2 || names[0] != "smile" || names[1] != "blink" {
		t.Errorf("MorphTargets = %v, want the mesh's two named shapes", names)
	}
	if want := 2 * 3 * morphRecordSize; bytes != want {
		t.Errorf("MorphBytes = %d, want %d", bytes, want)
	}
	h.lookup(func(access LookupAccess) {
		if got := access.TotalMorphBytes(); got != bytes {
			t.Errorf("TotalMorphBytes = %d, want the one resident model's %d", got, bytes)
		}
	})
}

// No vendored asset is both skinned and morphed, so the interaction has to be
// built here: the null-skin fill answers the two halves of group 2 separately,
// and a model that owns both must bind neither of the shared ones.
func TestASkinnedAndMorphedModelBindsBothOfItsOwnBuffers(t *testing.T) {
	doc := testDoc()
	mesh := morphedMesh(doc, false, false, []gltf.PrimitiveAttributes{
		deltaTarget(doc, [][3]float32{{1, 0, 0}, {0, 0, 0}, {0, 0, 0}}, nil, nil),
	})
	doc.Meshes[mesh].Weights = []float64{0.5}
	doc.Nodes = []*gltf.Node{{Name: "face", Mesh: gltf.Index(mesh)}}
	sceneOf(doc, 0)
	doc.Scene = gltf.Index(0)
	// A rotation on the node makes it a degenerate single-joint skin, which is
	// how glTF authors a moving part - so the file is rigged and shaped at once.
	rotationClip(doc, "spin", 0, []float32{0, 1}, [][4]float32{{0, 0, 0, 1}, {0, 0, 1, 0}})
	h := residentMorphModel(t, doc, ModelDraw{
		Plays: []ClipPlay{{Clip: "spin", Time: 0.5, Weight: 1}},
	})
	instance := firstInstance(t, h)
	if instance.Flags&sceneNoSkin != 0 {
		t.Error("a skinned draw must not carry SCENE_NOSKIN, morphed or not")
	}
	// One joint over 61 frames plus the rest frame, not the null skin's row.
	if got, want := len(boundBytes(t, h, "scenePoses")), 62*poseSize; got != want {
		t.Errorf("the bound pose buffer is %d bytes, want the model's %d", got, want)
	}
	if got, want := len(boundBytes(t, h, "sceneMorphDeltas")), 3*morphRecordSize; got != want {
		t.Errorf("the bound delta buffer is %d bytes, want the model's %d", got, want)
	}
	block := decodeAnimBlock(t, boundBytes(t, h, "sceneAnim"), instance.AnimOffset)
	// Both counts live in the one block, which is the whole reason there are
	// two of them rather than a flags bitfield.
	if block.playCount != 1 {
		t.Errorf("playCount = %d, want the one play", block.playCount)
	}
	if block.targetCount != 1 {
		t.Fatalf("targetCount = %d, want the one shape above the cull", block.targetCount)
	}
	// The morph list sits behind the play records, so a block that packed its
	// plays and its targets in the wrong order reads a row as a weight.
	if got := block.targets[0]; got.Target != 0 || got.Weight != 0.5 {
		t.Errorf("target = %+v, want target 0 at the mesh's authored 0.5", got)
	}
}
