package scene

import (
	"github.com/dvoyni/cog/kernel"
)

// ModelDraw is everything one Model call says beyond which file it draws.
//
// A zero ModelDraw is a valid draw of the file's default scene at the origin,
// culled by the bounds the file declares.
//
// The fields the specification's ModelDraw also carries - ClipPlays,
// MorphWeights, Material and OverrideParams - arrive with the tickets that
// implement them. A field that parses and does nothing is worse than an absent
// one: it compiles at the call site and renders the wrong picture with nothing
// to explain it.
type ModelDraw struct {
	// Transform places the single instance. Transforms, when it is non-empty,
	// overrides it and places one instance per entry, exactly as MeshDraw does.
	//
	// Instancing is per primitive: a six-primitive model drawn at a hundred
	// transforms is six batches of a hundred, not six hundred draw calls. The
	// instances share the draw's animation, so a hundred crates is one call and
	// a hundred independently-animated characters is a hundred.
	Transform  Transform
	Transforms []Transform

	// Scene names an entry in the file's scenes array; empty is the file's
	// declared default. glTF scene names are optional, so a file whose scenes
	// are unnamed has no addressable scene but that default.
	//
	// Node names a node within that scene, and empty is the whole scene. It is
	// a plain name matched against the first depth-first node carrying it, not
	// a slash path: a path would make the selector a parsed string, and glTF
	// node names are not unique enough to make one mean anything.
	//
	// A Node draw re-roots. The node's authored world transform inside the file
	// is discarded and Transform replaces it, descendants keeping their
	// relative transforms, so props.glb with Node "crate" behaves as an
	// independent asset however the artist laid the file out. An empty Node
	// keeps the scene's root transforms, because a scene is authored as one
	// unit.
	//
	// Neither selector falls back. A Scene or Node that matches nothing skips
	// the draw and reports once: one typo'd node name rendering an entire
	// building at the origin is the worse failure.
	Scene string
	Node  string
}

// ModelLight is one KHR_lights_punctual light a model file declares, in the
// model's own space, with its node's flattened transform already applied.
//
// Lights are exposed as data and nothing converts one automatically. A file's
// lights are authored for the file, not for the scene it is dropped into: a
// lamp prop placed forty times would silently blow the sixteen-light per-pass
// cap, and which of a level's lights matter is the app's judgement, not the
// loader's. So an app reads these and declares the ones it wants through
// PointLight and SpotLight, at whatever world transform it drew the model at.
type ModelLight struct {
	Name string
	// Directional marks a glTF directional light, which scene has no recording
	// call for at all - the one directional light scene shades with is the
	// camera's own sun. Descr.Direction is the only placement such a light has.
	Directional bool
	// Descr is the light as scene's own recording calls take it, so declaring
	// one is PointLight(layers, light.Descr) with the position and direction
	// carried into world space.
	Descr LightDescr
}

// modelDrawRecord is one recorded Model call. It is kept apart from drawRecord
// because a model draw expands into one draw per primitive at flush time, and
// the expansion needs the path to be resolved against residency first - a
// non-resident model contributes no draws at all.
type modelDrawRecord struct {
	layers LayerMask
	path   string
	// scene and node are the draw's selectors, resolved against the resident
	// entry at expansion rather than at record: the path may not be resident
	// yet, and a selector means nothing until it is.
	scene, node string
	transform   Transform
	// transforms aliases the recording's transform arena, never the caller's
	// array, and is empty for a single-instance draw.
	transforms []Transform
}

// Model records one draw of the glTF file at path.
//
// Loading is asynchronous and a non-resident model is skipped, never
// substituted: the first call of a path enqueues a load and draws nothing that
// frame, and there is no placeholder. Drawing the same path every frame while
// it loads enqueues exactly one command, because an in-flight path is a state
// rather than an absence. A path that failed to load is never retried - a typo
// must not spawn a load command every frame forever - and clears only on
// unload.
//
// Failures report once through kernel.ReportError. Because the report fires
// from the load command's goroutine it lands a frame or more after the draw
// that triggered it, so an error can outlive the draw call that caused it: a
// caller that draws a bad path once and never again still gets exactly one
// report.
func (q *opQueue) Model(layers LayerMask, path string, draw ModelDraw) {
	transforms := draw.Transforms
	if len(transforms) > 0 {
		start := len(q.meshes.transforms)
		q.meshes.transforms = append(q.meshes.transforms, transforms...)
		transforms = q.meshes.transforms[start:len(q.meshes.transforms):len(q.meshes.transforms)]
	}
	draw.Transforms = transforms
	q.calls = append(q.calls, Op{Kind: OpModel, Layers: layers, Path: path, Model: draw})
	q.models = append(q.models, modelDrawRecord{
		layers: layers, path: path, scene: draw.Scene, node: draw.Node,
		transform: draw.Transform, transforms: transforms,
	})
}

// flushModels lists the model draws the flush is consuming, in recording order.
func (q *opQueue) flushModels() []modelDrawRecord { return q.publishedModels }

// instances resolves one model draw's placements, reading a single-instance
// draw as the one-element case so the expansion has one shape.
func (r modelDrawRecord) instances(single *[1]Transform) []Transform {
	if len(r.transforms) > 0 {
		return r.transforms
	}
	single[0] = r.transform
	return single[:]
}

// expandModels turns the frame's model draws into ordinary draw records, one
// per primitive per instance, and enqueues a load for every path that is not
// resident yet.
//
// The expansion appends to the draws the flush is already consuming rather than
// carrying a parallel list, so culling, sorting, batching and inspection are
// blind to whether a draw came from a file or from BakeMesh. That is what makes
// a model draw cost exactly what the same geometry recorded by hand would.
//
// Records are laid out primitive-major: one primitive's instances are
// contiguous and share a group, so the packer's run scan finds them the way it
// finds a Mesh call's. Instance-major would interleave two primitives' records
// and break the contiguity the whole batching path assumes.
func (p *Plugin) expandModels(
	k kernel.Kernel, report func(error), lookup *Lookup, write *OpQueue,
) {
	models := write.flushModels()
	if len(models) == 0 {
		return
	}
	// The world matrices are sized in one pass before any of them is written,
	// because a draw record points into this arena and appending to it while
	// records already point at it would move the backing under them. The
	// selectors resolve in that same pass and the views are kept, so a draw
	// whose Node matched nothing is skipped and reported exactly once rather
	// than resolved twice.
	var single [1]Transform
	p.modelViews = grow(p.modelViews, len(models))
	worlds := 0
	for i := range models {
		p.modelViews[i] = modelView{}
		entry, ok := lookup.requestModel(k, models[i].path)
		if !ok {
			continue
		}
		view, err := entry.view(models[i].path, models[i].scene, models[i].node)
		if err != nil {
			lookup.reportOnce(report, err.reportKey(), err)
			continue
		}
		p.modelViews[i] = view
		worlds += len(view.primitives) * len(models[i].instances(&single))
	}
	p.modelWorlds = grow(p.modelWorlds, worlds)
	at := 0
	for i := range models {
		model := &models[i]
		view := &p.modelViews[i]
		if !view.resolved {
			continue
		}
		instances := model.instances(&single)
		for j := range view.primitives {
			primitive := &view.primitives[j]
			material := &view.materials[primitive.material]
			group := uint32(0)
			if len(instances) > 1 {
				group = uint32(write.drawCount()) + 1
			}
			for _, instance := range instances {
				// Re-rooting sits between the draw's own transform and the
				// primitive's flattened one, which is exactly what "the draw's
				// Transform replaces the node's authored world transform"
				// means: descendants keep their relative places, the subtree as
				// a whole moves to where the call put it.
				world := instance.Mat4()
				if view.rerooted {
					world = world.Mul(view.reroot)
				}
				p.modelWorlds[at] = world.Mul(primitive.local)
				write.appendFlushDraw(drawRecord{
					layers:    model.layers,
					transform: Transform{Matrix: &p.modelWorlds[at]},
					material:  material.material,
					mesh:      primitive.mesh,
					pbr:       &material.record,
					bounds:    primitive.bounds,
					neverCull: view.neverCull,
					group:     group,
				})
				at++
			}
		}
	}
}
