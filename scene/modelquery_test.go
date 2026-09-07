package scene

import (
	"testing"

	"github.com/dvoyni/cog/m"
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

// residentBoundsModel loads boundsModel and returns a harness with it resident,
// so a query test asserts the answer rather than the wait.
func residentBoundsModel(t testing.TB) *harness {
	t.Helper()
	return residentModel(t, boundsModel(t))
}

// residentModel loads one document and returns a harness with it resident.
func residentModel(t testing.TB, doc *gltf.Document) *harness {
	t.Helper()
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), func(*OpQueue) {})
	h.lookup(func(la LookupAccess) { la.Preload(modelPath) })
	h.frameUntil(t, "the model to become resident", func() bool {
		var state ModelState
		h.lookup(func(la LookupAccess) { state = la.State(modelPath) })
		return state == ModelResident
	})
	return h
}

// State is what tells a loading screen "wait" from "never coming", and it fires
// the same load a draw does - so a caller who polls only State still gets the
// model, rather than polling ModelMissing forever.
func TestStateFiresTheLoadAndReachesResident(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, boundsModel(t))), func(*OpQueue) {})
	var first ModelState
	h.lookup(func(la LookupAccess) { first = la.State(modelPath) })
	if first != ModelLoading {
		t.Fatalf("the first State = %v, want ModelLoading: the query fires the load", first)
	}
	h.frameUntil(t, "the model to become resident", func() bool {
		var state ModelState
		h.lookup(func(la LookupAccess) { state = la.State(modelPath) })
		return state == ModelResident
	})
}

// An invalid path never reaches a load command, so nothing in the goroutine can
// ever report it. The facade has to validate where the caller is standing.
func TestAnInvalidPathIsFailedSynchronously(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, boundsModel(t))), func(*OpQueue) {})
	var state ModelState
	h.lookup(func(la LookupAccess) { state = la.State("../escape.glb") })
	if state != ModelFailed {
		t.Fatalf("State(%q) = %v, want ModelFailed without waiting a frame", "../escape.glb", state)
	}
	if _, ok := reportedAs[ErrModelPathInvalid](h.errors()); !ok {
		t.Fatalf("reported %v, want the invalid path reported at once", h.errors())
	}
	// Terminal means terminal: a second query neither reports again nor
	// enqueues anything.
	h.lookup(func(la LookupAccess) { state = la.State("../escape.glb") })
	if state != ModelFailed || len(h.errors()) != 1 {
		t.Fatalf("second query: state %v, reports %v; want one report and still failed", state, h.errors())
	}
}

// Nodes with no Node selector lists the scene's addressable names in the order
// the flatten walked them, which is the order a Node selector's own semantics
// are defined in.
func TestNodesListsTheSceneDepthFirst(t *testing.T) {
	h := residentBoundsModel(t)
	var names []string
	var ok bool
	h.lookup(func(la LookupAccess) { names, ok = la.Nodes(ModelRef{Path: modelPath}, nil) })
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
	h.lookup(func(la LookupAccess) {
		whole, _ = la.Nodes(ModelRef{Path: modelPath, Node: "root"}, nil)
		leaf, _ = la.Nodes(ModelRef{Path: modelPath, Node: "crate"}, nil)
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
	var scene, node m.Vec4
	var sceneMin, sceneMax, nodeMin, nodeMax m.Vec3
	h.lookup(func(la LookupAccess) {
		scene, _ = la.Bounds(ModelRef{Path: modelPath})
		sceneMin, sceneMax, _ = la.AABB(ModelRef{Path: modelPath})
		node, _ = la.Bounds(ModelRef{Path: modelPath, Node: "crate"})
		nodeMin, nodeMax, _ = la.AABB(ModelRef{Path: modelPath, Node: "crate"})
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
	if scene.X != 10.5 {
		t.Errorf("scene Bounds centre X = %v, want 10.5", scene.X)
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

	for _, ref := range []ModelRef{
		{Path: modelPath},
		{Path: modelPath, Node: "root"},
		{Path: modelPath, Node: "crate"},
		{Path: modelPath, Node: "sibling"},
	} {
		var stillMin, stillMax, movingMin, movingMax m.Vec3
		var stillOK, movingOK bool
		still.lookup(func(la LookupAccess) { stillMin, stillMax, stillOK = la.AABB(ref) })
		moving.lookup(func(la LookupAccess) { movingMin, movingMax, movingOK = la.AABB(ref) })
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
	h.lookup(func(la LookupAccess) { la.Preload(modelPath) })
	h.frameUntil(t, "the model to become resident", func() bool {
		var state ModelState
		h.lookup(func(la LookupAccess) { state = la.State(modelPath) })
		return state == ModelResident
	})
	h.lookup(func(la LookupAccess) {
		if _, ok := la.Bounds(ModelRef{Path: modelPath}); ok {
			t.Error("a model whose POSITION declared no min/max has no bound to report")
		}
		if _, _, ok := la.AABB(ModelRef{Path: modelPath}); ok {
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
	h.lookup(func(la LookupAccess) {
		names, ok = la.Nodes(ModelRef{Path: modelPath, Node: "typo"}, []string{"kept"})
		_, _ = la.Bounds(ModelRef{Path: modelPath, Node: "typo"})
	})
	if ok {
		t.Error("an unmatched node is not a real answer")
	}
	if len(names) != 1 || names[0] != "kept" {
		t.Errorf("dst = %v, want it untouched", names)
	}
	missing := 0
	for _, err := range h.errors() {
		if _, is := err.(ErrModelNodeMissing); is {
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
	h.lookup(func(la LookupAccess) {
		if _, ok := la.Nodes(ModelRef{Path: modelPath, Scene: "nope"}, nil); ok {
			t.Error("an unmatched scene is not a real answer")
		}
	})
	if _, ok := reportedAs[ErrModelSceneMissing](h.errors()); !ok {
		t.Errorf("reported %v, want the missing scene", h.errors())
	}
}

// Every query fires the load, so a caller who never calls Preload still gets an
// answer eventually rather than polling an empty list forever.
func TestAQueryOnAMissingPathFiresTheLoad(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, boundsModel(t))), func(*OpQueue) {})
	var names []string
	var ok bool
	h.lookup(func(la LookupAccess) { names, ok = la.Nodes(ModelRef{Path: modelPath}, nil) })
	if ok || names != nil {
		t.Fatalf("the first query cannot be resident: %v %v", names, ok)
	}
	h.frameUntil(t, "the query alone to bring the model in", func() bool {
		h.lookup(func(la LookupAccess) { names, ok = la.Nodes(ModelRef{Path: modelPath}, nil) })
		return ok
	})
	if len(names) != 3 {
		t.Fatalf("nodes = %v, want the three the file names", names)
	}
}
