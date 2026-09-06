package scene

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/m"
	"github.com/qmuntal/gltf"
)

// propsFile is the file the selector tests draw out of: a root placed away from
// the origin with two named props under it, each one triangle. It is the shape
// the whole feature exists for - one file holding several independent props,
// laid out however the artist found convenient.
func propsFile(t testing.TB) *gltf.Document {
	t.Helper()
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "layout", Children: []int{1, 2}, Translation: [3]float64{4, 0, 0}},
		{Name: "crate", Mesh: gltf.Index(mesh), Translation: [3]float64{0, 3, 0}},
		{Name: "barrel", Mesh: gltf.Index(mesh), Translation: [3]float64{0, -3, 0}},
	}
	sceneOf(doc, 0)
	doc.Scene = gltf.Index(0)
	return doc
}

// residentDraw runs frames until the model is resident and the pass packed the
// instances the test expects, then hands back the harness.
func residentDraw(t *testing.T, doc *gltf.Document, draw ModelDraw, instances int) *harness {
	t.Helper()
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), drawModel(modelPath, draw))
	h.frameUntil(t, "the model to become resident", func() bool {
		passes := h.passes()
		return len(passes) == 1 && passes[0].Instances == instances
	})
	return h
}

// A Node draw takes that node's contiguous slice of the flattened list and
// nothing else, which is what lets one file hold many independent props.
func TestANodeDrawTakesOnlyThatSubtree(t *testing.T) {
	h := residentDraw(t, propsFile(t), ModelDraw{Node: "crate"}, 1)
	if batches := h.passes()[0].Batches; len(batches) != 1 {
		t.Fatalf("batches = %v, want the crate alone out of the file's two props", batches)
	}
}

// A subtree comes out whole: the named node's own primitives and every
// descendant's, because depth-first order made it a slice.
func TestANodeDrawTakesTheWholeSubtree(t *testing.T) {
	h := residentDraw(t, propsFile(t), ModelDraw{Node: "layout"}, 2)
	if batches := h.passes()[0].Batches; len(batches) != 2 {
		t.Fatalf("batches = %v, want both props under the layout node", batches)
	}
}

// Re-rooting discards the node's authored world transform and the draw's
// Transform replaces it, so a prop drawn by name lands where the draw put it
// however the artist laid the file out.
func TestANodeDrawReRootsToTheDrawTransform(t *testing.T) {
	h := residentDraw(t, propsFile(t), ModelDraw{Node: "crate", Transform: At(10, 0, 0)}, 1)
	// The triangle's declared box is (0,0,0)..(1,1,0), so its sphere sits at
	// (0.5, 0.5, 0) in the crate's own space. The file's 4 on X and 3 on Y are
	// what re-rooting throws away.
	if got := drawnSphere(t, h).Center; abs32(got.X-10.5) > 1e-4 || abs32(got.Y-0.5) > 1e-4 {
		t.Errorf("the re-rooted crate sits at %v, want {10.5 0.5 0}", got)
	}
}

// Re-rooting inverts a rotation as readily as a translation. The chain here is
// CesiumMilkTruck's own, numbers and depth included: a Yup2Zup root, the truck
// body, the wheel's placement node at 1.43 on X, and the wheel itself turned in
// place - four deep, with a real authored world transform to discard.
func TestANodeDrawReRootsThroughARotatedChain(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "Yup2Zup", Children: []int{1}, Rotation: [4]float64{0.5, -0.5, 0.5, 0.5}},
		{Name: "Cesium_Milk_Truck", Children: []int{2}},
		{Name: "Node", Children: []int{3}, Translation: [3]float64{1.43267, 0, -0.427722}},
		{Name: "Wheels", Mesh: gltf.Index(mesh), Rotation: [4]float64{0, 0.0884859, 0, -0.9960774}},
	}
	sceneOf(doc, 0)
	doc.Scene = gltf.Index(0)
	h := residentDraw(t, doc, ModelDraw{Node: "Wheels", Transform: At(10, 0, 0)}, 1)
	// The wheel's mesh hangs off the named node itself, so re-rooting leaves
	// the draw's own transform and nothing else: the triangle's sphere sits
	// where it does in its own space, moved by the call.
	if got := drawnSphere(t, h).Center; abs32(got.X-10.5) > 1e-4 ||
		abs32(got.Y-0.5) > 1e-4 || abs32(got.Z) > 1e-4 {
		t.Errorf("the re-rooted wheel sits at %v, want {10.5 0.5 0}", got)
	}
}

// An empty Node keeps the scene's root transforms, because a scene is authored
// as one unit: the same draw of the same file lands where the artist put it.
func TestAWholeSceneDrawKeepsTheAuthoredTransforms(t *testing.T) {
	h := residentDraw(t, propsFile(t), ModelDraw{Transform: At(10, 0, 0)}, 2)
	if got := drawnSphere(t, h).Center; abs32(got.X-14.5) > 1e-4 || abs32(got.Y-3.5) > 1e-4 {
		t.Errorf("the whole scene's crate sits at %v, want the authored {14.5 3.5 0}", got)
	}
}

// A Scene selector draws that entry of the file's scenes array, not the
// default one.
func TestASceneSelectorDrawsThatScene(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "a", Mesh: gltf.Index(mesh)},
		{Name: "b", Mesh: gltf.Index(mesh), Translation: [3]float64{0, 1, 0}},
		{Name: "c", Mesh: gltf.Index(mesh), Translation: [3]float64{0, 2, 0}},
	}
	namedSceneOf(doc, "solo", 0)
	namedSceneOf(doc, "pair", 1, 2)
	doc.Scene = gltf.Index(0)
	h := residentDraw(t, doc, ModelDraw{Scene: "pair"}, 2)
	if batches := h.passes()[0].Batches; len(batches) != 2 {
		t.Fatalf("batches = %v, want the named scene's two nodes", batches)
	}
}

// A Node is resolved within the selected scene, so the same name in another
// scene is not a match.
func TestANodeIsResolvedWithinTheSelectedScene(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "crate", Mesh: gltf.Index(mesh)},
		{Name: "barrel", Mesh: gltf.Index(mesh)},
	}
	namedSceneOf(doc, "first", 0)
	namedSceneOf(doc, "second", 1)
	doc.Scene = gltf.Index(0)
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)),
		drawModel(modelPath, ModelDraw{Scene: "second", Node: "crate"}))
	h.frameUntil(t, "the unmatched node to be reported", func() bool {
		var missing ErrModelNodeMissing
		return anyErrorAs(h.errors(), &missing)
	})
	if passes := h.passes(); len(passes) != 1 || passes[0].Instances != 0 {
		t.Fatalf("packed %v, want nothing: crate is not in the second scene", passes)
	}
}

// An unmatched Node skips the draw and never falls back to the whole scene: one
// typo'd name rendering an entire building at the origin is the worse failure.
func TestAnUnmatchedNodeSkipsTheDrawAndReportsOnce(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, propsFile(t))),
		drawModel(modelPath, ModelDraw{Node: "crat"}))
	h.frameUntil(t, "the unmatched node to be reported", func() bool {
		var missing ErrModelNodeMissing
		return anyErrorAs(h.errors(), &missing)
	})
	for i := 0; i < 20; i++ {
		h.frame()
	}
	if passes := h.passes(); len(passes) != 1 || passes[0].Instances != 0 {
		t.Fatalf("packed %v, want nothing at all for a typo'd node", passes)
	}
	if got := countAs[ErrModelNodeMissing](h.errors()); got != 1 {
		t.Errorf("a typo'd node reported %d times over 20-odd frames, want once", got)
	}
}

// An unmatched Scene skips the draw too, and reports under its own key rather
// than the node's, so a bad scene and a bad node are two reports.
func TestAnUnmatchedSceneSkipsTheDrawAndReportsOnce(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, propsFile(t))),
		drawModel(modelPath, ModelDraw{Scene: "outdoors"}))
	h.frameUntil(t, "the unmatched scene to be reported", func() bool {
		var missing ErrModelSceneMissing
		return anyErrorAs(h.errors(), &missing)
	})
	for i := 0; i < 20; i++ {
		h.frame()
	}
	if passes := h.passes(); len(passes) != 1 || passes[0].Instances != 0 {
		t.Fatalf("packed %v, want nothing at all for a typo'd scene", passes)
	}
	if got := countAs[ErrModelSceneMissing](h.errors()); got != 1 {
		t.Errorf("a typo'd scene reported %d times over 20-odd frames, want once", got)
	}
}

// The report key carries the node, so two typos in one file are two reports
// rather than one silence.
func TestTwoUnmatchedNodesOfOneFileBothReport(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, propsFile(t))), func(q *OpQueue) {
		q.Camera(cameraMain, modelCamera())
		q.Model(LayersAll, modelPath, ModelDraw{Node: "crat"})
		q.Model(LayersAll, modelPath, ModelDraw{Node: "barrl"})
	})
	h.frameUntil(t, "both unmatched nodes to be reported", func() bool {
		return countAs[ErrModelNodeMissing](h.errors()) == 2
	})
	for i := 0; i < 20; i++ {
		h.frame()
	}
	if got := countAs[ErrModelNodeMissing](h.errors()); got != 2 {
		t.Errorf("two typo'd nodes reported %d times, want one report each", got)
	}
}

// A node whose authored world transform collapses an axis cannot be re-rooted,
// so the draw skips and says why rather than drawing through a matrix that is
// quietly wrong.
func TestADrawOfACollapsedNodeSkipsAndReports(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{{Name: "flat", Mesh: gltf.Index(mesh), Scale: [3]float64{1, 0, 1}}}
	sceneOf(doc, 0)
	doc.Scene = gltf.Index(0)
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)),
		drawModel(modelPath, ModelDraw{Node: "flat"}))
	h.frameUntil(t, "the collapsed node to be reported", func() bool {
		var degenerate ErrModelNodeDegenerate
		return anyErrorAs(h.errors(), &degenerate)
	})
	if passes := h.passes(); len(passes) != 1 || passes[0].Instances != 0 {
		t.Fatalf("packed %v, want nothing for a node that cannot be re-rooted", passes)
	}
}

// The selectors reach inspection with the rest of the call, because an Op
// reports what the recorder said.
func TestAModelCallCarriesItsSelectorsToInspection(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, propsFile(t))),
		drawModel(modelPath, ModelDraw{Scene: "scene", Node: "crate"}))
	h.frame()
	ops := h.ops()
	if len(ops) != 2 || ops[1].Kind != OpModel {
		t.Fatalf("ops = %d, want the camera and the model", len(ops))
	}
	if ops[1].Model.Scene != "scene" || ops[1].Model.Node != "crate" {
		t.Errorf("the op reports Scene %q Node %q, want scene/crate",
			ops[1].Model.Scene, ops[1].Model.Node)
	}
}

// A Node draw instances like any other: the subtree is one batch per primitive
// however many transforms the call carries.
func TestANodeDrawInstances(t *testing.T) {
	h := residentDraw(t, propsFile(t), ModelDraw{
		Node:       "layout",
		Transforms: []Transform{At(0, 0, 0), At(2, 0, 0), At(4, 0, 0)},
	}, 6)
	batches := h.passes()[0].Batches
	if len(batches) != 2 {
		t.Fatalf("batches = %v, want one per primitive of the subtree", batches)
	}
	for i, batch := range batches {
		if batch.InstanceCount != 3 {
			t.Errorf("batch %d holds %d instances, want the call's three", i, batch.InstanceCount)
		}
	}
}

// drawnSphere is the world-space bounding sphere of the frame's first expanded
// draw. With no GPU it is the assertion surface for where a draw ended up: the
// culler tests exactly this sphere against the pass frustum.
func drawnSphere(t *testing.T, h *harness) m.Sphere {
	t.Helper()
	var sphere m.Sphere
	h.inspect(func(q *OpQueue) {
		draws := q.flushDraws()
		if len(draws) == 0 {
			t.Fatal("no draws were expanded")
		}
		sphere = prepareDraw(draws[0], meshRecord{}).sphere
	})
	return sphere
}

// countAs counts the reports of one type, which is how a report-once claim is
// asserted over a run of frames.
func countAs[E error](reported []error) int {
	count := 0
	for _, err := range reported {
		var target E
		if errors.As(err, &target) {
			count++
		}
	}
	return count
}
