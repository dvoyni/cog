package internal

import "github.com/dvoyni/cog/libs/m"

// ModelView is what one draw's Scene and Node selectors resolve to on a
// resident model: the contiguous slice of the flattened list the draw expands,
// and the re-root that slice is placed through.
//
// It is a slice rather than a filter because the flatten is depth-first, which
// is the whole mechanism: a subtree's primitives are emitted before anything
// outside it, so selecting one is two indices and no per-primitive test.
type ModelView struct {
	Primitives []modelPrimitive
	// start is where that slice begins in the entry's own flattened list, which
	// is what lets the arrays parallel to it - the load-time boxes the lookup
	// facade reports - be indexed by the same range.
	start int
	// Materials and neverCull come along so that the view is the whole answer
	// to what a draw expands into: the expansion consults the model table once,
	// in the sizing pass, and never again.
	Materials []modelMaterial
	NeverCull bool
	// Reroot is the inverse of the named node's authored world transform, which
	// the draw's own Transform then replaces. rerooted is false for a
	// whole-scene draw, and the multiply is skipped rather than done against an
	// identity: a scene is authored as one unit and keeps its root transforms.
	Reroot   m.Mat4
	Rerooted bool
	// RerootJoint and rerootRest are the animated half of the re-root, and are
	// -1 and unset for the overwhelming majority of nodes, whose ancestors
	// hold still. Where they are set, the frame's true re-root is
	// reroot * rerootRest * inverse(pose(rerootJoint)), which the packer folds
	// in once per draw.
	RerootJoint int
	RerootRest  m.Mat4
	// Animation is the model's clip table and the two group 2 buffers a draw
	// of it binds. It comes along so the view is the whole answer to what a
	// draw expands into: the expansion consults the model table once, in the
	// sizing pass, and never again.
	Animation *ResidentAnimation
	// Resolved separates a selector that matched an empty subtree - a real node
	// that happens to carry no geometry - from one that matched nothing.
	Resolved bool
}

// ModelSelectorError is a selector failure that knows the key it reports under.
// The key carries the selector rather than only the path, so two typo'd names
// in one file are two reports and one typo drawn every frame is still one.
type ModelSelectorError interface {
	error
	reportKey() string
}

func (e ErrModelSceneMissing) reportKey() string   { return "model:" + e.Model + "#scene:" + e.Scene }
func (e ErrModelNodeMissing) reportKey() string    { return "model:" + e.Model + "#" + e.Node }
func (e ErrModelNodeDegenerate) reportKey() string { return "model:" + e.Model + "#" + e.Node }

// View resolves one draw's selectors against a resident model, or says why the
// draw is skipped.
//
// Nothing falls back. An unmatched scene does not become the default one and an
// unmatched node does not become the whole scene: one typo'd node name
// rendering an entire building at the origin is the worse failure of the two,
// and it is the one a report cannot make visible.
func (e *ModelEntry) View(path, scene, node string) (ModelView, ModelSelectorError) {
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
			return ModelView{}, ErrModelSceneMissing{Model: path, Scene: scene}
		}
	}
	within := &e.scenes[selected]
	view := ModelView{
		Materials: e.Materials, NeverCull: e.neverCull, Resolved: true,
		RerootJoint: -1, Animation: &e.animation,
	}
	if node == "" {
		view.Primitives, view.start = e.primitives[within.start:within.end], within.start
		return view, nil
	}
	named, ok := within.nodes[node]
	if !ok {
		return ModelView{}, ErrModelNodeMissing{Model: path, Scene: scene, Node: node}
	}
	if !named.rerootable {
		return ModelView{}, ErrModelNodeDegenerate{Model: path, Node: node}
	}
	view.Primitives, view.start = e.primitives[named.start:named.end], named.start
	view.Reroot, view.Rerooted = named.reroot, true
	if len(named.animated) > 0 {
		view.RerootJoint, view.RerootRest = named.rerootJoint, named.rest
	}
	return view, nil
}
