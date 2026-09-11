package scene

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
)

// ModelDraw is everything one Model call says beyond which file it draws.
//
// A zero ModelDraw is a valid draw of the file's default scene at the origin,
// culled by the bounds the file declares.
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

	// Plays are the animation clips this draw blends, up to four. An empty
	// Plays draws the model's rest pose, which is a real pose rather than a
	// collapse: row 0 of every model is the authored hierarchy resolved once.
	//
	// Weights are normalised across the plays before anything is packed, so
	// they express proportions rather than intensities: two plays at 1.0 each
	// is an even blend, and so is two at 0.1. A total of about zero falls back
	// to the rest pose.
	//
	// The slice is copied into the frame's own arena, so a caller may reuse
	// its backing the moment the call returns. A draw's instances share its
	// plays - a hundred crates is one call, and a hundred independently
	// animated characters is a hundred calls.
	Plays []ClipPlay

	// MorphWeights are this draw's morph target weights, positional over the
	// model's whole flattened target list - which MorphTargets(path) names, in
	// depth-first node order, one entry per target of every morphed node.
	//
	// It is the one index-addressed thing in the plugin. Name addressing would
	// put about fifty-two map hits per face per frame on the recording path to
	// re-derive a mapping the caller computed at startup; naming lives on the
	// lookup facade instead, so this path is a memcpy.
	//
	// A non-nil value overrides the animated result wholesale; nil falls back
	// to the animated weights, then node.weights, then mesh.weights, then zero.
	// A short slice leaves the remaining targets at 0 and a long one ignores
	// the tail and reports once per model. Neither is an error.
	//
	// The slice is copied into the frame's own arena, so a caller may reuse its
	// backing the moment the call returns. A draw's instances share it, exactly
	// as they share Plays.
	MorphWeights []float32

	// Material replaces the file's own materials wholesale, and nil is the
	// file's. A non-nil value is bound instead of every record the load built,
	// and the file's PBR records are not bound at all - its base colours,
	// factors and texture transforms do not survive. That is the dissolve, the
	// silhouette and the depth-only case, where binding the artist's numbers
	// under a shader that never heard of them would be a wrong picture with
	// nothing in the frame to explain it.
	//
	// It does not overlap with OverrideParams. This one replaces and that one
	// merges, and a draw may still use both: the replacement takes glTF's own
	// defaults for its record and the overrides merge over those.
	Material Material

	// OverrideParams merges by name over each primitive's own material,
	// keeping the file's textures. That is the team-colour, hit-flash and fade
	// case. glTF's parameter names are the user-facing contract, so
	// gfx.ColorParam("baseColorFactor", c) is what tints a model, and the glTF
	// specification is the documentation of what each name means.
	//
	// It broadcasts to every material the draw binds - all six of a
	// six-material model's - which is what the common per-draw override
	// actually wants. Matching is against the resolved tag entry, and a name
	// that entry's shader does not declare is ignored rather than reported:
	// that is what keeps the broadcast safe across tags, since an alphaMode
	// MASK shadow shader declares baseColorTexture and alphaCutoff where an
	// OPAQUE one declares neither.
	//
	// A nil Material with no overrides binds the file's records directly, with
	// no copy of either.
	//
	// The slice is copied into the frame's own arena, so a caller may reuse its
	// backing the moment the call returns, exactly as with Plays.
	OverrideParams []gfx.ParameterDescr
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
	// plays aliases the recording's play arena for the same reason, and
	// morphWeights the recording's weight arena. A nil morphWeights is the
	// draw taking the animated result; an empty non-nil one is the caller
	// asking for every target at zero, which are different answers.
	plays        []ClipPlay
	morphWeights []float32
	overridden   bool
	// material is the caller's replacement for the file's own, nil when the
	// draw takes the file's, and overrides aliases the recording's parameter
	// arena for the same reason plays and morphWeights alias theirs.
	material  Material
	overrides []gfx.ParameterDescr
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
	plays := draw.Plays
	if len(plays) > 0 {
		start := len(q.plays)
		q.plays = append(q.plays, plays...)
		plays = q.plays[start:len(q.plays):len(q.plays)]
	}
	weights := draw.MorphWeights
	overridden := weights != nil
	if len(weights) > 0 {
		start := len(q.morphWeights)
		q.morphWeights = append(q.morphWeights, weights...)
		weights = q.morphWeights[start:len(q.morphWeights):len(q.morphWeights)]
	}
	overrides := draw.OverrideParams
	if len(overrides) > 0 {
		start := len(q.meshes.params)
		q.meshes.params = append(q.meshes.params, overrides...)
		overrides = q.meshes.params[start:len(q.meshes.params):len(q.meshes.params)]
	}
	draw.Transforms, draw.Plays, draw.MorphWeights = transforms, plays, weights
	draw.OverrideParams = overrides
	q.calls = append(q.calls, Op{Kind: OpModel, Layers: layers, Path: path, Model: draw})
	q.models = append(q.models, modelDrawRecord{
		layers: layers, path: path, scene: draw.Scene, node: draw.Node,
		transform: draw.Transform, transforms: transforms, plays: plays,
		morphWeights: weights, overridden: overridden,
		material: draw.Material, overrides: overrides,
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
	// Animation resolves in its own pass, between the two, because packing a
	// block appends to an arena and the draw records written below carry only
	// the offset it returned. It is per model draw rather than per primitive:
	// one call's primitives share its plays, and so do its instances.
	p.resolveModelAnimation(report, lookup, models)
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
			// A replacement material unbinds the file's record along with its
			// bindings: "the file's parameters do not survive" is as much the
			// numbers as the textures, and a nil record is what makes the draw
			// take glTF's own defaults instead.
			// The variant is what this primitive deforms, not what its model
			// does: a static prop bolted to an animated model reads neither
			// the poses nor the deltas, and asking the model would charge it
			// four of the eight storage buffers a browser core adapter
			// guarantees. The bindings are narrowed to the same answer here,
			// so the module a draw compiles and what it binds come from one
			// place and can never disagree.
			owned := &view.materials[primitive.material]
			anim := p.modelAnims[i]
			anim.skinned, anim.joint, anim.plain =
				primitive.skinned, primitive.joint, primitive.plain
			anim.skin.bound = anim.skin.bound && primitive.skinned
			anim.skin.morphed = anim.skin.morphed && primitive.morph.morphed()
			material, record := owned.variants[variantFor(anim.skin.bound, anim.skin.morphed)], &owned.record
			if model.material != nil {
				material, record = model.material, nil
			}
			// A morphed model packs a block per primitive rather than per
			// call, because the four morph words and the sparse weight list
			// are the primitive's, not the draw's.
			if anim.morphAt >= 0 {
				anim.offset = p.modelMorphOffsets[anim.morphAt+j]
			}
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
					material:  material,
					mesh:      primitive.mesh,
					pbr:       record,
					// The overrides ride on the draw's gfx parameters, which
					// is where every name the entry's shader declares is
					// resolved, and are marked as also addressing the record,
					// which gfx cannot see.
					params:          model.overrides,
					overridesRecord: len(model.overrides) > 0,
					bounds:          primitive.bounds,
					// A skinned placement is never culled. Its bind-pose sphere
					// is the only bound the load has, and where the joints put
					// it this frame is not knowable without replaying the blend
					// on the CPU - which is the per-frame hierarchy walk this
					// whole design exists to remove.
					//
					// The placement is what is asked, not the mesh: a shared
					// primitive culled by a static instance's bounds while its
					// animated instance walks out of them is a mesh that
					// disappears with nothing reported.
					neverCull: view.neverCull || primitive.skinned,
					group:     group,
					anim:      anim,
				})
				at++
			}
		}
	}
}

// resolveModelAnimation turns each model draw's clip plays into one sceneAnim
// block, and folds an animated re-root into the view that needs one.
//
// It runs as its own pass over the frame's model draws, after the selectors
// resolve and before any draw record is written: packing a block appends to
// the frame's arena, and a record carries only the offset that append
// returned.
func (p *Plugin) resolveModelAnimation(
	report func(error), lookup *Lookup, models []modelDrawRecord,
) {
	p.modelAnims = grow(p.modelAnims, len(models))
	p.modelMorphOffsets = p.modelMorphOffsets[:0]
	once := func(key string, err error) { lookup.reportOnce(report, key, err) }
	for i := range models {
		p.modelAnims[i] = animBinding{offset: sceneNoAnim, morphAt: -1}
		view := &p.modelViews[i]
		if !view.resolved {
			continue
		}
		anim := view.animation
		p.modelAnims[i].skin = anim.skin()
		p.modelPlays, p.modelWeightFrames = resolvePlays(
			anim, models[i].path, models[i].plays,
			p.modelPlays[:0], p.modelWeightFrames[:0], once,
		)
		// A model with no shapes packs one block for the whole call, which is
		// what every primitive of it reads. A morphed one packs a block per
		// primitive instead, because the morph words and the sparse weight list
		// are the primitive's rather than the draw's.
		if anim.slotCount == 0 {
			p.modelAnims[i].offset = p.build.packAnim(p.modelPlays, morphBlock{})
		} else {
			p.packModelMorphs(&p.modelAnims[i], anim, view, &models[i], once)
		}
		// The load's re-root inverse is the rest pose's. It is the right answer
		// for every node whose ancestors hold still - which is almost all of
		// them - and the wrong one for a subtree hanging off a bone a clip
		// steers, whose true place this frame is only in the pose rows.
		if view.rerooted && view.rerootJoint >= 0 {
			if pose, ok := blendJoint(anim, p.modelPlays, view.rerootJoint); ok {
				if inverse, ok := pose.InverseAffine(); ok {
					view.reroot = view.reroot.Mul(view.rerootRest).Mul(inverse)
				}
			}
		}
	}
}

// packModelMorphs packs one model draw's per-primitive sceneAnim blocks. It is
// called only for a model that has morph slots; a model without them packs one
// block for the whole call.
//
// The blend is done once for the whole draw - it is over the model's flattened
// slot list, which every primitive indexes into - and the cull, the cap and the
// sparse list are then per primitive, because a primitive's targets are its own
// block's and its addressing constants are per primitive too.
//
// A model with no shapes leaves morphAt at -1 and keeps the one block per call
// the skinned path packs, which is what makes morph targets cost a rig nothing.
func (p *Plugin) packModelMorphs(
	binding *animBinding, anim *residentAnimation, view *modelView,
	model *modelDrawRecord, report reportOnce,
) {
	p.modelWeights = blendMorphWeights(
		anim, model.path, p.modelPlays, p.modelWeightFrames,
		model.morphWeights, model.overridden, p.modelWeights, report,
	)
	binding.morphAt = len(p.modelMorphOffsets)
	for j := range view.primitives {
		morph := view.primitives[j].morph
		block := morphBlock{binding: morph}
		if morph.morphed() {
			slots := p.modelWeights[morph.slotBase : morph.slotBase+morph.targets]
			block.targets = selectMorphTargets(slots, p.modelTargets[:0], model.path, report)
			p.modelTargets = block.targets
		}
		p.modelMorphOffsets = append(
			p.modelMorphOffsets, p.build.packAnim(p.modelPlays, block))
	}
}
