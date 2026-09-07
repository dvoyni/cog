package scene

import (
	pathpkg "path"
	"strings"

	"github.com/dvoyni/cog/m"
)

// ModelRef names what a scene- or node-scoped query is asking about, mirroring
// ModelDraw's own selectors field for field.
//
// It is a struct rather than three bare strings because the bare form has a
// transposition bug that compiles: Bounds(path, "crate", "") and
// Bounds(path, "", "crate") are both valid calls and mean different things.
//
// Only Nodes, Bounds and AABB take one. Everything else on the facade is per
// path, because path is the whole cache key: a model has one joint index space,
// and MorphTargets is one flattened list that Node re-rooting does not renumber.
type ModelRef struct {
	// Path is the glTF file, the same string a draw of it names.
	Path string
	// Scene names an entry in the file's scenes array; empty is the file's
	// declared default. Node names a node within that scene; empty is the whole
	// scene. Neither falls back, exactly as in a draw: a selector that matches
	// nothing is ok = false and one report, never the default.
	Scene string
	Node  string
}

// reportOnce fires one report under a key the lookup has not reported yet,
// adapting kernel.ReportError's bool return - which says whether the engine
// should keep running - to the reporting the table does.
func (la LookupAccess) reportOnce(key string, err error) {
	la.lookup.reportOnce(func(err error) { la.kernel.ReportError(err) }, key, err)
}

// modelBox is one primitive's declared axis-aligned bounds. known is false for
// a primitive whose POSITION accessor carried no min/max, which is what makes
// the whole model never-cull - and it has to be carried rather than defaulted,
// because the zero box is a real box at the origin.
type modelBox struct {
	box   m.Box3
	known bool
}

// modelKey is the cache key a caller-supplied path resolves to, and whether it
// is a usable path at all.
//
// An invalid path is its own key - the string the caller passed - so that the
// failure it is recorded under is the one they can unload to clear.
func modelKey(path string) (string, bool) {
	clean, ok := validateResourcePath(path)
	if !ok {
		return path, false
	}
	return clean, true
}

// validateResourcePath normalizes a resource path and rejects empty, absolute,
// NUL-bearing, or root-escaping inputs, so every model and texture the lookup
// holds shares one cache key and one security boundary.
//
// It is canvas's rule, duplicated rather than shared: the two plugins have no
// package between them, and a third package holding six lines of string
// handling would be a dependency edge bought for nothing.
func validateResourcePath(path string) (string, bool) {
	if path == "" || strings.ContainsRune(path, 0) {
		return "", false
	}
	cleaned := pathpkg.Clean(strings.ReplaceAll(path, "\\", "/"))
	if cleaned == "" || cleaned == "." {
		return "", false
	}
	if strings.HasPrefix(cleaned, "/") {
		return "", false
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

// State reports one path's residency, and is the only way to tell "wait" from
// "never coming": every other query answers ok = false for both, and a loading
// screen watching only ok hangs forever on a typo.
//
// It fires the same idempotent load a draw fires, like every query here, so a
// caller who polls State alone still gets the model. That makes ModelMissing
// unobservable through this call on a valid path - the state exists as the
// table's zero value and as what an unload resets a slot to, and the very act
// of asking moves it to ModelLoading.
func (la LookupAccess) State(path string) ModelState {
	if !la.Valid() {
		return ModelMissing
	}
	la.lookup.requestModel(la.kernel, path)
	key, _ := modelKey(path)
	if entry, ok := la.lookup.models[key]; ok {
		return entry.state
	}
	return ModelMissing
}

// Nodes appends the names of the addressable nodes in a ref's scene, in the
// depth-first order the flatten walked them, and reports whether they are real.
// A non-empty Node lists that subtree instead, the node itself first.
//
// Depth-first rather than sorted because the order is the hierarchy's, which is
// the thing a caller is looking at: an alphabetical list of a rig's bones tells
// nobody what hangs off what. Unnamed nodes are absent, because a selector is a
// name and a node without one cannot be addressed.
//
// A degenerate node - one whose authored world transform collapsed an axis, so
// a draw of it is skipped - still lists here. Its names are a real answer;
// only re-rooting it is impossible, which is Bounds's and AABB's problem.
func (la LookupAccess) Nodes(ref ModelRef, dst []string) ([]string, bool) {
	if !la.Valid() {
		return dst, false
	}
	entry, ok := la.lookup.requestModel(la.kernel, ref.Path)
	if !ok {
		return dst, false
	}
	scene, err := entry.scene(ref)
	if err != nil {
		la.reportOnce(err.reportKey(), err)
		return dst, false
	}
	if ref.Node == "" {
		return append(dst, scene.order...), true
	}
	named, found := scene.nodes[ref.Node]
	if !found {
		err := ErrModelNodeMissing{Model: ref.Path, Scene: ref.Scene, Node: ref.Node}
		la.reportOnce(err.reportKey(), err)
		return dst, false
	}
	return append(dst, scene.order[named.first:named.last]...), true
}

// Bounds returns a ref's bounding sphere as xyz centre and w radius - the same
// m.Vec4 convention MeshDraw.Bounds uses, so the name means one thing across
// the plugin - and reports whether it is real.
//
// It is local space post-re-rooting: a Node ref answers in the space a draw of
// that node would place it in, with the node's authored world transform already
// discarded. A skinned model answers in its rest pose, because that is the only
// pose the load has.
//
// The sphere is the union of the primitives' own spheres, each transformed,
// rather than the circumsphere of the box AABB reports. Under a rotation those
// differ and the union is the tighter of the two, since transforming a sphere
// is exact where refitting a box around rotated corners is not; where nothing
// rotates the box can be the tighter one, and neither dominates in general.
func (la LookupAccess) Bounds(ref ModelRef) (m.Vec4, bool) {
	sphere, _, ok := la.bounds(ref)
	if !ok {
		return m.Vec4{}, false
	}
	return m.Vec4{X: sphere.Center.X, Y: sphere.Center.Y, Z: sphere.Center.Z, W: sphere.Radius}, true
}

// AABB returns a ref's axis-aligned bounding box in the same space and under
// the same pose rules as Bounds, and reports whether it is real.
//
// It is published alongside the sphere because scene has both anyway: the box
// is the load-time by-product the per-primitive spheres are computed from.
// Publishing only the box would be a regression - a sphere derived from one is
// its circumsphere, up to sqrt(3) loose - and publishing only the sphere would
// cost a caller who wants to intersect a box the box.
func (la LookupAccess) AABB(ref ModelRef) (min, max m.Vec3, ok bool) {
	_, box, ok := la.bounds(ref)
	if !ok {
		return m.Vec3{}, m.Vec3{}, false
	}
	return box.Min, box.Max, true
}

// bounds resolves a ref and accumulates both bound geometries over the
// primitives it selects, in one pass, because the two queries differ only in
// which half of the answer they return.
//
// A primitive whose POSITION accessor declared no min/max contributes nothing
// and makes the whole answer false. A bound over the rest of the model would be
// a real-looking number that the unbounded piece sticks out of, which is worse
// than no answer: the caller cannot see the hole.
func (la LookupAccess) bounds(ref ModelRef) (m.Sphere, m.Box3, bool) {
	if !la.Valid() {
		return m.Sphere{}, m.Box3{}, false
	}
	entry, ok := la.lookup.requestModel(la.kernel, ref.Path)
	if !ok {
		return m.Sphere{}, m.Box3{}, false
	}
	view, err := entry.view(ref.Path, ref.Scene, ref.Node)
	if err != nil {
		la.reportOnce(err.reportKey(), err)
		return m.Sphere{}, m.Box3{}, false
	}
	// A selector that matched a real node carrying no geometry has no bound,
	// and saying so is the point: it is a different fact from a typo, and the
	// caller is told apart from it by the absence of a report.
	start, end := view.start, view.start+len(view.primitives)
	if start == end {
		return m.Sphere{}, m.Box3{}, false
	}
	var sphere m.Sphere
	var box m.Box3
	first := true
	for i := start; i < end; i++ {
		bound := entry.boxes[i]
		if !bound.known {
			return m.Sphere{}, m.Box3{}, false
		}
		place := entry.primitives[i].local
		if view.rerooted {
			place = view.reroot.Mul(place)
		}
		primitiveBox := bound.box.Transform(place)
		primitiveSphere := bound.box.Sphere().Transform(place)
		if first {
			sphere, box, first = primitiveSphere, primitiveBox, false
			continue
		}
		sphere, box = sphere.Union(primitiveSphere), box.Union(primitiveBox)
	}
	return sphere, box, true
}

// scene resolves a ref's Scene selector to the entry it names, or says why it
// matched nothing. It is the half of view() that a name query needs: Nodes
// answers for a degenerate node that view() would reject, and re-rooting is
// what view() adds on top.
func (e *modelEntry) scene(ref ModelRef) (*loadedScene, modelSelectorError) {
	selected := e.defaultScene
	if ref.Scene != "" {
		selected = -1
		for i := range e.scenes {
			if e.scenes[i].name == ref.Scene {
				selected = i
				break
			}
		}
		if selected < 0 {
			return nil, ErrModelSceneMissing{Model: ref.Path, Scene: ref.Scene}
		}
	}
	if selected < 0 || selected >= len(e.scenes) {
		return nil, ErrModelSceneMissing{Model: ref.Path, Scene: ref.Scene}
	}
	return &e.scenes[selected], nil
}
