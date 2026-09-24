package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/qmuntal/gltf"
)

// boundsModel is the fixture every lookup query test reads: a named root ten
// units along X, carrying two named children that each draw the unit triangle
// at a different offset. Two primitives under one re-rootable node is the
// smallest shape that makes a subtree bound differ from the whole scene's, and
// the smallest that makes a node list have an order worth asserting.
func boundsModel(t testing.TB) *gltf.Document {
	t.Helper()
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "root", Children: []int{1, 2}, Translation: [3]float64{10, 0, 0}},
		{Name: "crate", Translation: [3]float64{0, 5, 0}, Mesh: gltf.Index(mesh)},
		{Name: "sibling", Translation: [3]float64{0, 0, 3}, Mesh: gltf.Index(mesh)},
	}
	sceneOf(doc, 0)
	doc.Scene = gltf.Index(0)
	return doc
}

// residentBoundsModel loads boundsModel and returns a harness with it loaded,
// so a query test asserts the answer rather than the load.
func residentBoundsModel(t testing.TB) *harness {
	t.Helper()
	return residentModel(t, boundsModel(t))
}

// residentModel loads one document and returns a harness with it loaded. There
// is nothing to wait for: Preload reads, parses and uploads before it returns,
// which is what makes it the lever a loading screen pulls.
func residentModel(t testing.TB, doc *gltf.Document) *harness {
	t.Helper()
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), func(*OpQueue) {})
	h.device(func(la model.LookupDeviceAccess) {
		la.Preload(modelPath)
		if err := la.State(modelPath); err != nil {
			t.Fatalf("Preload left %q unloaded: %v", modelPath, err)
		}
	})
	return h
}

// State is what tells a loading screen "not there" from "never coming", and it
// fires the same load a draw does - so a caller who polls only State gets the
// model, and gets it in the call that asked.
//
// nil is loaded. There is no state word left to return, because there is no
// in-flight state to name: by the time State returns, the read, the parse and
// every upload have happened.
func TestStateLoadsTheFileAndAnswersNil(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, boundsModel(t))), func(*OpQueue) {})
	var first error
	h.device(func(la model.LookupDeviceAccess) { first = la.State(modelPath) })
	if first != nil {
		t.Fatalf("the first State = %v, want nil: the query loads the file", first)
	}
}

// A model whose file is not there is terminal and says why. The Library owns
// the read and reports it once; State turns the entry it cached into the reason
// a HUD prints.
func TestStateNamesTheReasonAModelIsNotThere(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, boundsModel(t))), func(*OpQueue) {})
	const missing = "models/absent.glb"
	var err error
	h.device(func(la model.LookupDeviceAccess) { err = la.State(missing) })
	if err == nil {
		t.Fatal("State on a file that is not there must not answer nil")
	}
	if len(h.errors()) != 1 {
		t.Fatalf("reported %v, want the failed read reported exactly once", h.errors())
	}
	// Terminal means terminal: a second query neither reloads nor reports
	// again, because the entry the failure left is the record that it ran.
	h.device(func(la model.LookupDeviceAccess) { err = la.State(missing) })
	if err == nil || len(h.errors()) != 1 {
		t.Fatalf("second query: err %v, reports %v; want one report and still failed", err, h.errors())
	}
}

// An invalid path never enters the cache at all: it is refused where the caller
// is standing, with no entry and no tombstone behind it, so a typo is
// permanently a typo.
func TestAnInvalidPathIsRefusedBeforeTheCache(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, boundsModel(t))), func(*OpQueue) {})
	var err error
	h.device(func(la model.LookupDeviceAccess) { err = la.State("../escape.glb") })
	if _, ok := err.(model.ErrModelPathInvalid); !ok {
		t.Fatalf("State(%q) = %v, want the invalid path refused at once", "../escape.glb", err)
	}
	if _, ok := reportedAs[model.ErrModelPathInvalid](h.errors()); !ok {
		t.Fatalf("reported %v, want the invalid path reported at once", h.errors())
	}
	// Terminal means terminal: a second query neither reports again nor
	// loads anything.
	h.device(func(la model.LookupDeviceAccess) { err = la.State("../escape.glb") })
	if err == nil || len(h.errors()) != 1 {
		t.Fatalf("second query: err %v, reports %v; want one report and still refused", err, h.errors())
	}
}

// Nodes with no Node selector lists the scene's addressable names in the order
// the flatten walked them, which is the order a Node selector's own semantics
// are defined in.
func TestNodesListsTheSceneDepthFirst(t *testing.T) {
	h := residentBoundsModel(t)
	var names []string
	var ok bool
	h.device(func(la model.LookupDeviceAccess) { names, ok = la.Nodes(model.ModelRef{Path: modelPath}, nil) })
	if !ok {
		t.Fatal("a resident model's node list is real")
	}
	if len(names) != 3 || names[0] != "root" || names[1] != "crate" || names[2] != "sibling" {
		t.Fatalf("nodes = %v, want root, crate, sibling depth-first", names)
	}
}

// A non-empty Node lists that subtree, root included: the answer to "what is
// under crate" is a list a caller can pass straight back as a Node selector.
func TestNodesListsOneSubtree(t *testing.T) {
	h := residentBoundsModel(t)
	var whole, leaf []string
	h.device(func(la model.LookupDeviceAccess) {
		whole, _ = la.Nodes(model.ModelRef{Path: modelPath, Node: "root"}, nil)
		leaf, _ = la.Nodes(model.ModelRef{Path: modelPath, Node: "crate"}, nil)
	})
	if len(whole) != 3 {
		t.Errorf("root's subtree = %v, want all three", whole)
	}
	if len(leaf) != 1 || leaf[0] != "crate" {
		t.Errorf("crate's subtree = %v, want just crate", leaf)
	}
}

// Bounds and AABB are local space post-re-rooting: selecting a node answers in
// the space the draw of that node would put it in, not in the file's.
func TestBoundsAndAABBAreLocalSpacePostRerooting(t *testing.T) {
	h := residentBoundsModel(t)
	var whole, node m.Vec4
	var sceneMin, sceneMax, nodeMin, nodeMax m.Vec3
	h.device(func(la model.LookupDeviceAccess) {
		whole, _ = la.Bounds(model.ModelRef{Path: modelPath})
		sceneMin, sceneMax, _ = la.AABB(model.ModelRef{Path: modelPath})
		node, _ = la.Bounds(model.ModelRef{Path: modelPath, Node: "crate"})
		nodeMin, nodeMax, _ = la.AABB(model.ModelRef{Path: modelPath, Node: "crate"})
	})
	// The whole scene keeps its root transforms, so the box is where the file
	// put it: X in [10,11], Y from the sibling's 0 to the crate's 6, Z from the
	// crate's 0 to the sibling's 3.
	if sceneMin != (m.Vec3{X: 10}) || sceneMax != (m.Vec3{X: 11, Y: 6, Z: 3}) {
		t.Errorf("scene AABB = %v..%v, want {10 0 0}..{11 6 3}", sceneMin, sceneMax)
	}
	// The crate re-roots onto its own origin, so its authored (10,5,0) is gone
	// and the unit triangle sits at the origin.
	if nodeMin != (m.Vec3{}) || nodeMax != (m.Vec3{X: 1, Y: 1}) {
		t.Errorf("crate AABB = %v..%v, want the origin unit triangle", nodeMin, nodeMax)
	}
	if nodeCenter := (m.Vec3{X: node.X, Y: node.Y, Z: node.Z}); nodeCenter != (m.Vec3{X: 0.5, Y: 0.5}) {
		t.Errorf("crate Bounds centre = %v, want the triangle's own centre", nodeCenter)
	}
	if whole.X != 10.5 {
		t.Errorf("scene Bounds centre X = %v, want 10.5", whole.X)
	}
}

// Animating a node does not move its bounds. A node with a clip of its own
// carrying a mesh takes the degenerate single-joint path: its placement moves
// out of the instance record and into the pose buffer, and modelPrimitive.local
// is left the identity. A bound read off that identity is a bound at the origin
// for geometry the frame draws somewhere else - and re-rooted it is worse than
// that, because the re-root then applies the inverse of a transform that was
// never applied in the first place.
//
// So the whole assertion is that these two documents answer the same: the only
// difference between them is a clip nobody plays.
func TestAnimatingANodeDoesNotMoveItsBounds(t *testing.T) {
	still := residentBoundsModel(t)
	doc := boundsModel(t)
	// One clip on the crate, which is node 1. Nothing plays it; declaring it is
	// what routes the node through the pose buffer.
	rotationClip(doc, "spin", 1, []float32{0, 1}, [][4]float32{{0, 0, 0, 1}, {0, 0, 1, 0}})
	moving := residentModel(t, doc)

	for _, ref := range []model.ModelRef{
		{Path: modelPath},
		{Path: modelPath, Node: "root"},
		{Path: modelPath, Node: "crate"},
		{Path: modelPath, Node: "sibling"},
	} {
		var stillMin, stillMax, movingMin, movingMax m.Vec3
		var stillOK, movingOK bool
		still.device(func(la model.LookupDeviceAccess) { stillMin, stillMax, stillOK = la.AABB(ref) })
		moving.device(func(la model.LookupDeviceAccess) { movingMin, movingMax, movingOK = la.AABB(ref) })
		if !stillOK || !movingOK {
			t.Fatalf("AABB(%+v) answered %v still and %v animated", ref, stillOK, movingOK)
		}
		if stillMin != movingMin || stillMax != movingMax {
			t.Errorf("AABB(%+v) = %v..%v with a clip on the crate and %v..%v without; "+
				"the rest pose is the same pose either way",
				ref, movingMin, movingMax, stillMin, stillMax)
		}
	}
}

// A model with no bound has no bound to publish. Returning a zero box would be
// indistinguishable from a real degenerate one, which is the whole reason these
// queries carry an ok at all.
func TestBoundsIsFalseWhenThePrimitiveDeclaredNone(t *testing.T) {
	doc := boundsModel(t)
	position := doc.Meshes[0].Primitives[0].Attributes[gltf.POSITION]
	doc.Accessors[position].Min, doc.Accessors[position].Max = nil, nil
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), func(*OpQueue) {})
	h.device(func(la model.LookupDeviceAccess) {
		la.Preload(modelPath)
		if _, ok := la.Bounds(model.ModelRef{Path: modelPath}); ok {
			t.Error("a model whose POSITION declared no min/max has no bound to report")
		}
		if _, _, ok := la.AABB(model.ModelRef{Path: modelPath}); ok {
			t.Error("and no box either")
		}
	})
}

// An unmatched selector on a resident model is false, not the default scene and
// not the whole subtree - the same never-fall-back rule a draw follows - and it
// reports once however many times it is asked.
func TestAnUnmatchedNodeIsFalseAndReportsOnce(t *testing.T) {
	h := residentBoundsModel(t)
	var names []string
	var ok bool
	h.device(func(la model.LookupDeviceAccess) {
		names, ok = la.Nodes(model.ModelRef{Path: modelPath, Node: "typo"}, []string{"kept"})
		_, _ = la.Bounds(model.ModelRef{Path: modelPath, Node: "typo"})
	})
	if ok {
		t.Error("an unmatched node is not a real answer")
	}
	if len(names) != 1 || names[0] != "kept" {
		t.Errorf("dst = %v, want it untouched", names)
	}
	missing := 0
	for _, err := range h.errors() {
		if _, is := err.(model.ErrModelNodeMissing); is {
			missing++
		}
	}
	if missing != 1 {
		t.Errorf("reported the missing node %d times, want once across both queries", missing)
	}
}

// An unmatched scene is its own report key, so a file with two typos reports
// both rather than the first swallowing the second.
func TestAnUnmatchedSceneIsFalse(t *testing.T) {
	h := residentBoundsModel(t)
	h.device(func(la model.LookupDeviceAccess) {
		if _, ok := la.Nodes(model.ModelRef{Path: modelPath, Scene: "nope"}, nil); ok {
			t.Error("an unmatched scene is not a real answer")
		}
	})
	if _, ok := reportedAs[model.ErrModelSceneMissing](h.errors()); !ok {
		t.Errorf("reported %v, want the missing scene", h.errors())
	}
}

// Every query loads, so a caller who never calls Preload still gets an answer -
// in the call that asked, rather than after polling an empty list for frames.
func TestAQueryOnAnUnloadedPathLoadsIt(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, boundsModel(t))), func(*OpQueue) {})
	var names []string
	var ok bool
	h.device(func(la model.LookupDeviceAccess) { names, ok = la.Nodes(model.ModelRef{Path: modelPath}, nil) })
	if !ok {
		t.Fatal("the query alone must bring the model in")
	}
	if len(names) != 3 {
		t.Fatalf("nodes = %v, want the three the file names", names)
	}
}
