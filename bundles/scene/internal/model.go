package internal

import (
	"io/fs"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

// expandModels turns the frame's model draws into ordinary draw records, one
// per primitive per instance, loading every path it names that the cache does
// not already hold.
//
// The load runs here, in this handler, holding these locks: the read, the JSON
// parse, the image decodes and every upload. A large file hitches the frame it
// was first named in, which is the cost this design takes deliberately and
// which Preload is the lever for. A path that does not load expands into no
// primitives at all - skip, never substitute - and the reason was reported
// where it was found.
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
func (p *plugin) expandModels(
	k kernel.Kernel, lookup *model.Lookup, write *OpQueue,
	filesystem storage.FileSystem, resources *gfx.ResourceQueue,
) {
	models := OpQueueFlushModels(write)
	if len(models) == 0 {
		return
	}
	// The one boxed filesystem the frame pays for. Handing a storage.FileSystem
	// out as an fs.FS allocates 32 bytes, and a Get needs it materialised
	// before the call, so it is converted once here rather than once per model
	// draw. A frame with no model draws has already returned above, so it pays
	// nothing at all.
	fsys := fs.FS(filesystem)
	// The world matrices are sized in one pass before any of them is written,
	// because a draw record points into this arena and appending to it while
	// records already point at it would move the backing under them. The
	// selectors resolve in that same pass and the views are kept, so a draw
	// whose Node matched nothing is skipped and reported exactly once rather
	// than resolved twice.
	var single [1]m.Transform
	p.modelViews = grow(p.modelViews, len(models))
	worlds := 0
	for i := range models {
		p.modelViews[i] = model.ModelView{}
		view, err, ok := lookup.ModelView(
			k, fsys, resources, models[i].Path, models[i].Scene, models[i].Node)
		if !ok {
			continue
		}
		if err != nil {
			k.ReportErrorOnce(err.ReportKey(), err)
			continue
		}
		p.modelViews[i] = view
		worlds += len(view.Primitives) * len(models[i].Instances(&single))
	}
	// Animation resolves in its own pass, between the two, because packing a
	// block appends to an arena and the draw records written below carry only
	// the offset it returned. It is per model draw rather than per primitive:
	// one call's primitives share its plays, and so do its instances.
	p.resolveModelAnimation(k, models)
	p.modelWorlds = grow(p.modelWorlds, worlds)
	at := 0
	for i := range models {
		call := &models[i]
		view := &p.modelViews[i]
		if !view.Resolved {
			continue
		}
		instances := call.Instances(&single)
		for j := range view.Primitives {
			primitive := &view.Primitives[j]
			// A replacement material unbinds the file's params, numbers and
			// textures alike: "the file's parameters do not survive".
			// The variant is what this primitive deforms, not what its model
			// does: a static prop bolted to an animated model reads neither
			// the poses nor the deltas, and asking the model would charge it
			// four of the eight storage buffers a browser core adapter
			// guarantees. The bindings are narrowed to the same answer here,
			// so the module a draw compiles and what it binds come from one
			// place and can never disagree.
			owned := &view.Materials[primitive.Material]
			anim := p.modelAnims[i]
			anim.Skinned, anim.Joint, anim.Plain =
				primitive.Skinned, primitive.Joint, primitive.Plain
			anim.Skin.Bound = anim.Skin.Bound && primitive.Skinned
			anim.Skin.Morphed = anim.Skin.Morphed && primitive.Morph.Morphed()
			material, key := call.Material, call.MaterialKey
			if material == nil {
				// The file's own material names no pass, so it is wrapped as
				// the forward one here. Its key derives from the one the load
				// took, so the flush never fingerprints a file material.
				variant := model.VariantFor(anim.Skin.Bound, anim.Skin.Morphed)
				material = p.forwardMaterial(owned.Forward[variant])
				key = ForwardMaterialKey(owned.Key[variant])
			}
			// A morphed model packs a block per primitive rather than per
			// call, because the four morph words and the sparse weight list
			// are the primitive's, not the draw's.
			if anim.MorphAt >= 0 {
				anim.Offset = p.modelMorphOffsets[anim.MorphAt+j]
			}
			group := uint32(0)
			if len(instances) > 1 {
				group = uint32(OpQueueDrawCount(write)) + 1
			}
			for _, instance := range instances {
				// Re-rooting sits between the draw's own transform and the
				// primitive's flattened one, which is exactly what "the draw's
				// Transform replaces the node's authored world transform"
				// means: descendants keep their relative places, the subtree as
				// a whole moves to where the call put it.
				world := instance.Mat4()
				if view.Rerooted {
					world = world.Mul(view.Reroot)
				}
				p.modelWorlds[at] = world.Mul(primitive.Local)
				OpQueueAppendDraw(write, DrawRecord{
					Layers:      call.Layers,
					Matrix:      &p.modelWorlds[at],
					Material:    material,
					MaterialKey: key,
					Mesh:        primitive.Mesh,
					// The overrides ride on the draw's gfx parameters, which
					// is where every name the entry's shader declares is
					// resolved, the material's numbers among them.
					Params: call.Overrides,
					Bounds: primitive.Bounds,
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
					NeverCull: view.NeverCull || primitive.Skinned,
					Group:     group,
					Anim:      anim,
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
func (p *plugin) resolveModelAnimation(k kernel.Kernel, models []ModelDrawRecord) {
	p.modelAnims = grow(p.modelAnims, len(models))
	p.modelMorphOffsets = p.modelMorphOffsets[:0]
	once := func(key string, err error) { k.ReportErrorOnce(key, err) }
	for i := range models {
		p.modelAnims[i] = AnimBinding{InstanceAnim: model.InstanceAnim{Offset: model.SceneNoAnim}, MorphAt: -1}
		view := &p.modelViews[i]
		if !view.Resolved {
			continue
		}
		anim := view.Animation
		p.modelAnims[i].Skin = anim.Skin()
		p.modelPlays, p.modelWeightFrames = model.ResolvePlays(
			anim, models[i].Path, models[i].Plays,
			p.modelPlays[:0], p.modelWeightFrames[:0], once,
		)
		// A model with no shapes packs one block for the whole call, which is
		// what every primitive of it reads. A morphed one packs a block per
		// primitive instead, because the morph words and the sparse weight list
		// are the primitive's rather than the draw's.
		if anim.SlotCount == 0 {
			p.modelAnims[i].Offset = p.build.appendAnim(p.modelPlays, model.AnimMorph{})
		} else {
			p.packModelMorphs(&p.modelAnims[i], anim, view, &models[i], once)
		}
		// The load's re-root inverse is the rest pose's. It is the right answer
		// for every node whose ancestors hold still - which is almost all of
		// them - and the wrong one for a subtree hanging off a bone a clip
		// steers, whose true place this frame is only in the pose rows.
		if view.Rerooted && view.RerootJoint >= 0 {
			if pose, ok := model.BlendJoint(anim, p.modelPlays, view.RerootJoint); ok {
				if inverse, ok := pose.InverseAffine(); ok {
					view.Reroot = view.Reroot.Mul(view.RerootRest).Mul(inverse)
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
func (p *plugin) packModelMorphs(
	binding *AnimBinding, anim *model.ResidentAnimation, view *model.ModelView,
	call *ModelDrawRecord, report model.ReportOnce,
) {
	p.modelWeights = model.BlendMorphWeights(
		anim, call.Path, p.modelPlays, p.modelWeightFrames,
		call.MorphWeights, call.Overridden, p.modelWeights, report,
	)
	binding.MorphAt = len(p.modelMorphOffsets)
	for j := range view.Primitives {
		morph := view.Primitives[j].Morph
		block := model.AnimMorph{Binding: morph}
		if morph.Morphed() {
			slots := p.modelWeights[morph.SlotBase : morph.SlotBase+morph.Targets]
			block.Targets = model.SelectMorphTargets(slots, p.modelTargets[:0], call.Path, report)
			p.modelTargets = block.Targets
		}
		p.modelMorphOffsets = append(
			p.modelMorphOffsets, p.build.appendAnim(p.modelPlays, block))
	}
}

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
	Transform  m.Transform
	Transforms []m.Transform

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
	Plays []model.ClipPlay

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
	// file's. A non-nil value is bound instead of every material the load
	// built, and the file's params are not bound at all - its textures, base
	// colours, factors and texture transforms do not survive. That is the dissolve, the
	// silhouette and the depth-only case, where binding the artist's numbers
	// under a shader that never heard of them would be a wrong picture with
	// nothing in the frame to explain it.
	//
	// It is copied into the frame's own arenas at record, exactly as a
	// MeshDraw's Material is, so a caller may reuse or change it the moment
	// the call returns.
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

// ModelDrawRecord is one recorded Model call. It is kept apart from DrawRecord
// because a model draw expands into one draw per primitive at flush time, and
// the expansion needs the path to be resolved against residency first - a
// unloaded model contributes no draws at all.
type ModelDrawRecord struct {
	Layers LayerMask
	Path   string
	// scene and node are the draw's selectors, resolved against the loaded
	// model at expansion rather than at record: the file is not read until the
	// flush, and a selector means nothing until it is.
	Scene, Node string
	transform   m.Transform
	// transforms aliases the recording's transform arena, never the caller's
	// array, and is empty for a single-instance draw.
	transforms []m.Transform
	// Plays aliases the recording's play arena for the same reason, and
	// morphWeights the recording's weight arena. A nil morphWeights is the
	// draw taking the animated result; an empty non-nil one is the caller
	// asking for every target at zero, which are different answers.
	Plays        []model.ClipPlay
	MorphWeights []float32
	Overridden   bool
	// Material is the caller's replacement for the file's own, nil when the
	// draw takes the file's. It and overrides alias the recording's material
	// and parameter arenas for the same reason plays and morphWeights alias
	// theirs.
	Material  Material
	Overrides []gfx.ParameterDescr
	// MaterialKey is Material's content key, taken at record; zero when
	// Material is nil or empty.
	MaterialKey MaterialKey
}

// Model records one draw of the glTF file at path.
//
// Loading is synchronous and a model that could not be loaded is skipped, never
// substituted: the flush this call is recorded into reads, parses and uploads
// the file, so the model draws in this same frame - and a large file hitches
// it. Preload is the lever that moves that cost somewhere the game chose. A
// path that failed is never loaded again - a typo must not re-read the file
// every frame forever - and clears only on unload, where there is no
// placeholder either way.
//
// Failures report once through kernel.ReportError, from the flush that hit
// them: the read failure from the asset library, under the descriptor, and
// everything the decode finds wrong from scene, under the path.
func (q *OpQueue) Model(layers LayerMask, path string, draw ModelDraw) {
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
	var key MaterialKey
	draw.Material, key = q.meshes.copyMaterial(draw.Material)
	draw.Transforms, draw.Plays, draw.MorphWeights = transforms, plays, weights
	draw.OverrideParams = overrides
	q.calls = append(q.calls, Op{Kind: OpModel, Layers: layers, Path: path, Model: draw})
	q.models = append(q.models, ModelDrawRecord{
		Layers: layers, Path: path, Scene: draw.Scene, Node: draw.Node,
		transform: draw.Transform, transforms: transforms, Plays: plays,
		MorphWeights: weights, Overridden: overridden,
		Material: draw.Material, Overrides: overrides, MaterialKey: key,
	})
}

// flushModels lists the model draws the flush is consuming, in recording order.
func (q *OpQueue) flushModels() []ModelDrawRecord { return q.publishedModels }

// Instances resolves one model draw's placements, reading a single-instance
// draw as the one-element case so the expansion has one shape.
func (r ModelDrawRecord) Instances(single *[1]m.Transform) []m.Transform {
	if len(r.transforms) > 0 {
		return r.transforms
	}
	single[0] = r.transform
	return single[:]
}
