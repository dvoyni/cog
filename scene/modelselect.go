package scene

import "github.com/dvoyni/cog/m"

// modelView is what one draw's Scene and Node selectors resolve to on a
// resident model: the contiguous slice of the flattened list the draw expands,
// and the re-root that slice is placed through.
//
// It is a slice rather than a filter because the flatten is depth-first, which
// is the whole mechanism: a subtree's primitives are emitted before anything
// outside it, so selecting one is two indices and no per-primitive test.
type modelView struct {
	primitives []modelPrimitive
	// materials and neverCull come along so that the view is the whole answer
	// to what a draw expands into: the expansion consults the model table once,
	// in the sizing pass, and never again.
	materials []modelMaterial
	neverCull bool
	// reroot is the inverse of the named node's authored world transform, which
	// the draw's own Transform then replaces. rerooted is false for a
	// whole-scene draw, and the multiply is skipped rather than done against an
	// identity: a scene is authored as one unit and keeps its root transforms.
	reroot   m.Mat4
	rerooted bool
	// rerootJoint and rerootRest are the animated half of the re-root, and are
	// -1 and unset for the overwhelming majority of nodes, whose ancestors
	// hold still. Where they are set, the frame's true re-root is
	// reroot * rerootRest * inverse(pose(rerootJoint)), which the packer folds
	// in once per draw.
	rerootJoint int
	rerootRest  m.Mat4
	// animation is the model's clip table and the two group 2 buffers a draw
	// of it binds. It comes along so the view is the whole answer to what a
	// draw expands into: the expansion consults the model table once, in the
	// sizing pass, and never again.
	animation *residentAnimation
	// resolved separates a selector that matched an empty subtree - a real node
	// that happens to carry no geometry - from one that matched nothing.
	resolved bool
}

// modelSelectorError is a selector failure that knows the key it reports under.
// The key carries the selector rather than only the path, so two typo'd names
// in one file are two reports and one typo drawn every frame is still one.
type modelSelectorError interface {
	error
	reportKey() string
}

func (e ErrModelSceneMissing) reportKey() string   { return "model:" + e.Model + "#scene:" + e.Scene }
func (e ErrModelNodeMissing) reportKey() string    { return "model:" + e.Model + "#" + e.Node }
func (e ErrModelNodeDegenerate) reportKey() string { return "model:" + e.Model + "#" + e.Node }

// view resolves one draw's selectors against a resident model, or says why the
// draw is skipped.
//
// Nothing falls back. An unmatched scene does not become the default one and an
// unmatched node does not become the whole scene: one typo'd node name
// rendering an entire building at the origin is the worse failure of the two,
// and it is the one a report cannot make visible.
func (e *modelEntry) view(path, scene, node string) (modelView, modelSelectorError) {
	selected := e.defaultScene
	if scene != "" {
		selected = -1
		for i := range e.scenes {
			if e.scenes[i].name == scene {
				selected = i
				break
			}
		}
		if selected < 0 {
			return modelView{}, ErrModelSceneMissing{Model: path, Scene: scene}
		}
	}
	within := &e.scenes[selected]
	view := modelView{
		materials: e.materials, neverCull: e.neverCull, resolved: true,
		rerootJoint: -1, animation: &e.animation,
	}
	if node == "" {
		view.primitives = e.primitives[within.start:within.end]
		return view, nil
	}
	named, ok := within.nodes[node]
	if !ok {
		return modelView{}, ErrModelNodeMissing{Model: path, Scene: scene, Node: node}
	}
	if !named.rerootable {
		return modelView{}, ErrModelNodeDegenerate{Model: path, Node: node}
	}
	view.primitives = e.primitives[named.start:named.end]
	view.reroot, view.rerooted = named.reroot, true
	if len(named.animated) > 0 {
		view.rerootJoint, view.rerootRest = named.rerootJoint, named.rest
	}
	return view, nil
}
