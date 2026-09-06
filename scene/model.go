package scene

import (
	"github.com/dvoyni/cog/kernel"
)

// ModelDraw is everything one Model call says beyond which file it draws.
//
// A zero ModelDraw is a valid draw of the file's default scene at the origin,
// culled by the bounds the file declares.
//
// The fields the specification's ModelDraw also carries - Scene and Node
// selectors, ClipPlays, MorphWeights, Material and OverrideParams - arrive with
// the tickets that implement them. A field that parses and does nothing is
// worse than an absent one: it compiles at the call site and renders the wrong
// picture with nothing to explain it.
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
	layers    LayerMask
	path      string
	transform Transform
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
		layers: layers, path: path, transform: draw.Transform, transforms: transforms,
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
func (p *Plugin) expandModels(k kernel.Kernel, lookup *Lookup, write *OpQueue) {
	models := write.flushModels()
	if len(models) == 0 {
		return
	}
	// The world matrices are sized in one pass before any of them is written,
	// because a draw record points into this arena and appending to it while
	// records already point at it would move the backing under them.
	var single [1]Transform
	worlds := 0
	for i := range models {
		entry, ok := lookup.requestModel(k, models[i].path)
		if !ok {
			continue
		}
		worlds += len(entry.primitives) * len(models[i].instances(&single))
	}
	p.modelWorlds = grow(p.modelWorlds, worlds)
	at := 0
	for i := range models {
		model := &models[i]
		entry, ok := lookup.residentModel(model.path)
		if !ok {
			continue
		}
		instances := model.instances(&single)
		for j := range entry.primitives {
			primitive := &entry.primitives[j]
			material := &entry.materials[primitive.material]
			group := uint32(0)
			if len(instances) > 1 {
				group = uint32(write.drawCount()) + 1
			}
			for _, instance := range instances {
				p.modelWorlds[at] = instance.Mat4().Mul(primitive.local)
				write.appendFlushDraw(drawRecord{
					layers:    model.layers,
					transform: Transform{Matrix: &p.modelWorlds[at]},
					material:  material.material,
					mesh:      primitive.mesh,
					pbr:       &material.record,
					bounds:    primitive.bounds,
					neverCull: entry.neverCull,
					group:     group,
				})
				at++
			}
		}
	}
}
