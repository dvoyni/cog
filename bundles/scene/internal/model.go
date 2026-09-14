package internal

import (
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal/types"
	"github.com/dvoyni/cog/kernel"
)

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
func (p *plugin) expandModels(
	k kernel.Kernel, report func(error), lookup *scene.Lookup, write *scene.OpQueue,
) {
	models := types.OpQueueFlushModels(write)
	if len(models) == 0 {
		return
	}
	// The world matrices are sized in one pass before any of them is written,
	// because a draw record points into this arena and appending to it while
	// records already point at it would move the backing under them. The
	// selectors resolve in that same pass and the views are kept, so a draw
	// whose Node matched nothing is skipped and reported exactly once rather
	// than resolved twice.
	var single [1]scene.Transform
	p.modelViews = grow(p.modelViews, len(models))
	worlds := 0
	for i := range models {
		p.modelViews[i] = types.ModelView{}
		entry, ok := types.LookupRequestModel(lookup, k, models[i].Path)
		if !ok {
			continue
		}
		view, err := entry.View(models[i].Path, models[i].Scene, models[i].Node)
		if err != nil {
			types.LookupReportOnce(lookup, report, types.SelectorReportKey(err), err)
			continue
		}
		p.modelViews[i] = view
		worlds += len(view.Primitives) * len(models[i].Instances(&single))
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
		if !view.Resolved {
			continue
		}
		instances := model.Instances(&single)
		for j := range view.Primitives {
			primitive := &view.Primitives[j]
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
			owned := &view.Materials[primitive.Material]
			anim := p.modelAnims[i]
			anim.Skinned, anim.Joint, anim.Plain =
				primitive.Skinned, primitive.Joint, primitive.Plain
			anim.Skin.Bound = anim.Skin.Bound && primitive.Skinned
			anim.Skin.Morphed = anim.Skin.Morphed && primitive.Morph.Morphed()
			material, record := owned.Variants[types.VariantFor(anim.Skin.Bound, anim.Skin.Morphed)], &owned.Record
			if model.Material != nil {
				material, record = model.Material, nil
			}
			// A morphed model packs a block per primitive rather than per
			// call, because the four morph words and the sparse weight list
			// are the primitive's, not the draw's.
			if anim.MorphAt >= 0 {
				anim.Offset = p.modelMorphOffsets[anim.MorphAt+j]
			}
			group := uint32(0)
			if len(instances) > 1 {
				group = uint32(types.OpQueueDrawCount(write)) + 1
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
				types.OpQueueAppendDraw(write, types.DrawRecord{
					Layers:   model.Layers,
					Matrix:   &p.modelWorlds[at],
					Material: material,
					Mesh:     primitive.Mesh,
					Pbr:      record,
					// The overrides ride on the draw's gfx parameters, which
					// is where every name the entry's shader declares is
					// resolved, and are marked as also addressing the record,
					// which gfx cannot see.
					Params:          model.Overrides,
					OverridesRecord: len(model.Overrides) > 0,
					Bounds:          primitive.Bounds,
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
func (p *plugin) resolveModelAnimation(
	report func(error), lookup *scene.Lookup, models []types.ModelDrawRecord,
) {
	p.modelAnims = grow(p.modelAnims, len(models))
	p.modelMorphOffsets = p.modelMorphOffsets[:0]
	once := func(key string, err error) { types.LookupReportOnce(lookup, report, key, err) }
	for i := range models {
		p.modelAnims[i] = types.AnimBinding{Offset: types.SceneNoAnim, MorphAt: -1}
		view := &p.modelViews[i]
		if !view.Resolved {
			continue
		}
		anim := view.Animation
		p.modelAnims[i].Skin = anim.Skin()
		p.modelPlays, p.modelWeightFrames = types.ResolvePlays(
			anim, models[i].Path, models[i].Plays,
			p.modelPlays[:0], p.modelWeightFrames[:0], once,
		)
		// A model with no shapes packs one block for the whole call, which is
		// what every primitive of it reads. A morphed one packs a block per
		// primitive instead, because the morph words and the sparse weight list
		// are the primitive's rather than the draw's.
		if anim.SlotCount == 0 {
			p.modelAnims[i].Offset = p.build.packAnim(p.modelPlays, morphBlock{})
		} else {
			p.packModelMorphs(&p.modelAnims[i], anim, view, &models[i], once)
		}
		// The load's re-root inverse is the rest pose's. It is the right answer
		// for every node whose ancestors hold still - which is almost all of
		// them - and the wrong one for a subtree hanging off a bone a clip
		// steers, whose true place this frame is only in the pose rows.
		if view.Rerooted && view.RerootJoint >= 0 {
			if pose, ok := types.BlendJoint(anim, p.modelPlays, view.RerootJoint); ok {
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
	binding *types.AnimBinding, anim *types.ResidentAnimation, view *types.ModelView,
	model *types.ModelDrawRecord, report types.ReportOnce,
) {
	p.modelWeights = types.BlendMorphWeights(
		anim, model.Path, p.modelPlays, p.modelWeightFrames,
		model.MorphWeights, model.Overridden, p.modelWeights, report,
	)
	binding.MorphAt = len(p.modelMorphOffsets)
	for j := range view.Primitives {
		morph := view.Primitives[j].Morph
		block := morphBlock{binding: morph}
		if morph.Morphed() {
			slots := p.modelWeights[morph.SlotBase : morph.SlotBase+morph.Targets]
			block.targets = types.SelectMorphTargets(slots, p.modelTargets[:0], model.Path, report)
			p.modelTargets = block.targets
		}
		p.modelMorphOffsets = append(
			p.modelMorphOffsets, p.build.packAnim(p.modelPlays, block))
	}
}
