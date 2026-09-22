package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// Bucketing: every frame, each drawable Entity's instances land in the Batch
// its key names. The keys are the load System's, taken on change, so a steady
// frame hashes a key per instance to find its Batch and never fingerprints a
// material or a parameter.
//
// Layers, culling and the camera are not in the key. They filter a Batch's
// instances per pass, in flushPass.

// addModel buckets one Model Entity: one entry per primitive of the view its
// selectors resolve to, at the Transform it stands at. A model the load System
// has not keyed - not resident yet, failed, or a selector that matched nothing
// - draws nothing.
func (s *scratch) addModel(frame *frameInputs, e ecs.Entity, it *modelQuery) {
	keyed, ok := frame.keys.model(e)
	if !ok || keyed.handle == 0 || len(keyed.keys) == 0 {
		return
	}
	view, err, ok := frame.read.View(keyed.handle, it.Model.Ref.Scene, it.Model.Ref.Node)
	if !ok || err != nil || len(view.Primitives) != len(keyed.keys) {
		return
	}
	skin := view.Animation.Skin()
	offset, morphAt := s.resolveAnimation(frame, e, it, &view, skin)
	world := it.Place.Mat4()
	if view.Rerooted {
		world = world.Mul(view.Reroot)
	}
	for j := range view.Primitives {
		primitive := &view.Primitives[j]
		// The variant is what this primitive deforms, not what its model
		// does: a static prop bolted to an animated model reads neither the
		// poses nor the deltas.
		bound := skin.Bound && primitive.Skinned
		morphed := skin.Morphed && primitive.Morph.Morphed()
		anim := model.InstanceAnim{
			Offset: offset, Skinned: primitive.Skinned, Joint: primitive.Joint, Plain: primitive.Plain,
		}
		if morphAt >= 0 {
			anim.Offset = s.modelMorphOffsets[morphAt+j]
		}
		if !bound && !morphed {
			anim.Offset = model.SceneNoAnim
		}
		variant := model.VariantFor(bound, morphed)
		key := bucketKey{key: keyed.keys[j], variant: variant}
		b, ok := s.bucket[key]
		if !ok {
			b = s.newModelBatch(frame, e, &view, j, variant, skin, bound, morphed, key.key.material)
			s.bucket[key] = b
		}
		if b < 0 {
			continue
		}
		primitiveWorld := world.Mul(primitive.Local)
		// A skinned placement is never culled: its bind-pose sphere is the
		// only bound the load has, and where the joints put it this frame is
		// not knowable without replaying the blend on the CPU.
		sphere, cullable := prepareDraw(primitiveWorld, view.NeverCull || primitive.Skinned,
			primitive.Bounds, &s.batches[b].mesh)
		s.entries = append(s.entries, entry{
			world: primitiveWorld, sphere: sphere, cullable: cullable,
			layers: it.Model.Layers, batch: b, anim: anim,
		})
	}
}

// newModelBatch resolves everything a Model primitive's Batch shares, from the
// first Entity bucketed into it: its mesh, its material, its properties record
// and its Params. It returns -1 for a Batch that cannot draw.
func (s *scratch) newModelBatch(
	frame *frameInputs, e ecs.Entity, view *model.ModelView, j int,
	variant model.ShaderVariant, skin model.SkinBuffers, bound, morphed bool, materialKey uint64,
) int32 {
	primitive := &view.Primitives[j]
	mesh, ok := frame.read.Mesh(primitive.Mesh)
	if !ok {
		if primitive.Mesh.Source() != model.MeshNone {
			s.reportMeshOnce(primitive.Mesh, model.ErrMeshUnavailable{Mesh: primitive.Mesh.ID()})
		}
		return -1
	}
	skin.Bound, skin.Morphed = bound, morphed
	b := batch{mesh: mesh, skin: skin}
	var own material
	if override, ok := frame.materials.Of(e); ok {
		// A replacement material unbinds the file's record along with its
		// bindings: the draw takes glTF's own defaults instead.
		own = s.copyMaterial(&override)
		b.pbr = model.PaintPbrRecord(m.NewColorLinear(1, 1, 1, 1), false)
	} else {
		// The file's own material names no pass, so it is wrapped as the
		// forward one, and its key is the one the load took.
		owned := &view.Materials[primitive.Material]
		own = s.forwardMaterial(owned.Forward[variant])
		b.pbr = owned.Record
	}
	if p, ok := frame.params.Of(e); ok {
		b.params = s.copyParams(&p)
	}
	// A Model's Params merge by name over the properties record as well as
	// riding on the draw's gfx parameters, because the record is a range the
	// recording packs itself and gfx never sees a name in it.
	if len(b.params) > 0 {
		b.pbr.Override(b.params)
	}
	b.interned = s.materials.intern(s.k, true, own, materialKey, variant)
	s.batches = append(s.batches, b)
	return int32(len(s.batches) - 1)
}

// addMesh buckets one Mesh Entity as one entry.
func (s *scratch) addMesh(frame *frameInputs, e ecs.Entity, it *meshQuery) {
	keyed, ok := frame.keys.mesh(e)
	if !ok {
		return
	}
	key := bucketKey{key: keyed, variant: model.VariantStatic}
	b, ok := s.bucket[key]
	if !ok {
		b = s.newMeshBatch(frame, e, it.Mesh.Ref, keyed.material)
		s.bucket[key] = b
	}
	if b < 0 {
		return
	}
	world := it.Place.Mat4()
	bounds := it.Mesh.Bounds
	sphere, cullable := prepareDraw(world, it.Mesh.NeverCull,
		m.Sphere{Center: m.Vec3{X: bounds.X, Y: bounds.Y, Z: bounds.Z}, Radius: bounds.W}, &s.batches[b].mesh)
	s.entries = append(s.entries, entry{
		world: world, sphere: sphere, cullable: cullable,
		layers: it.Mesh.Layers, batch: b, anim: model.InstanceAnim{Offset: model.SceneNoAnim},
	})
}

// newMeshBatch resolves a Mesh Batch from its first Entity. A Mesh draws with
// the bundled PBR as white paint unless it names a Material, and its Params
// stop at gfx: they are for what a custom material declares.
func (s *scratch) newMeshBatch(frame *frameInputs, e ecs.Entity, ref model.MeshRef, materialKey uint64) int32 {
	mesh, ok := frame.read.Mesh(ref)
	if !ok {
		// A ref that never named a mesh was the game's zero value, and draws
		// nothing in silence.
		if ref.Source() != model.MeshNone {
			s.reportMeshOnce(ref, model.ErrMeshUnavailable{Mesh: ref.ID()})
		}
		return -1
	}
	b := batch{mesh: mesh, pbr: model.PaintPbrRecord(m.NewColorLinear(1, 1, 1, 1), false)}
	override, hasMaterial := frame.materials.Of(e)
	if !mesh.Standard && !hasMaterial {
		s.reportMeshOnce(ref, ecsscene.ErrMeshCustomLayoutNeedsMaterial{Mesh: ref.ID()})
		return -1
	}
	var own material
	if hasMaterial {
		own = s.copyMaterial(&override)
	}
	if p, ok := frame.params.Of(e); ok {
		b.params = s.copyParams(&p)
	}
	b.interned = s.materials.intern(s.k, hasMaterial, own, materialKey, model.VariantStatic)
	s.batches = append(s.batches, b)
	return int32(len(s.batches) - 1)
}

// reportMeshOnce reports one mesh's failure the first time a Batch hits it
// this frame, keyed by the public id.
func (s *scratch) reportMeshOnce(ref model.MeshRef, err error) {
	if _, seen := s.meshReported[ref.ID()]; seen {
		return
	}
	s.meshReported[ref.ID()] = struct{}{}
	s.k.ReportError(err)
}

// resolveAnimation packs one Model Entity's sceneAnim block, or one per
// primitive for a morphed model, and folds an animated re-root into its view.
// It returns the offset every primitive's instance carries, and for a morphed
// model the index of its per-primitive offsets in modelMorphOffsets, or -1.
//
// A model with nothing to deform packs nothing: every instance of it carries
// SceneNoAnim, whatever an Animation says.
func (s *scratch) resolveAnimation(
	frame *frameInputs, e ecs.Entity, it *modelQuery, view *model.ModelView, skin model.SkinBuffers,
) (offset uint32, morphAt int) {
	if !skin.Bound && !skin.Morphed {
		return model.SceneNoAnim, -1
	}
	anim := view.Animation
	plays := s.plays[:0]
	if animation, ok := frame.animations.Of(e); ok {
		for i := range animation.Plays {
			if animation.Plays[i].Clip != "" {
				plays = append(plays, animation.Plays[i])
			}
		}
	}
	s.plays = plays
	path := it.Model.Ref.Path
	s.modelPlays, s.modelWeightFrames = model.ResolvePlays(
		anim, path, plays, s.modelPlays[:0], s.modelWeightFrames[:0], s.once)
	offset, morphAt = model.SceneNoAnim, -1
	// A model with no shapes packs one block for the whole Entity, which is
	// what every primitive of it reads. A morphed one packs a block per
	// primitive instead, because the morph words and the sparse weight list
	// are the primitive's.
	if anim.SlotCount == 0 {
		offset = s.build.appendAnim(s.modelPlays, model.AnimMorph{})
	} else {
		morphAt = s.packMorphs(anim, view, path)
	}
	// The load's re-root inverse is the rest pose's, which is wrong for a
	// subtree hanging off a bone a clip steers; its true place this frame is
	// only in the pose rows.
	if view.Rerooted && view.RerootJoint >= 0 {
		if pose, ok := model.BlendJoint(anim, s.modelPlays, view.RerootJoint); ok {
			if inverse, ok := pose.InverseAffine(); ok {
				view.Reroot = view.Reroot.Mul(view.RerootRest).Mul(inverse)
			}
		}
	}
	return offset, morphAt
}

// packMorphs packs one morphed Entity's per-primitive sceneAnim blocks and
// returns where their offsets start. The blend is once for the whole Entity,
// over the model's flattened slot list; the cull, the cap and the sparse list
// are per primitive. An Entity has no morph-weight override, so the weights
// are the animated ones.
func (s *scratch) packMorphs(anim *model.ResidentAnimation, view *model.ModelView, path string) int {
	s.modelWeights = model.BlendMorphWeights(
		anim, path, s.modelPlays, s.modelWeightFrames, nil, false, s.modelWeights, s.once)
	at := len(s.modelMorphOffsets)
	for j := range view.Primitives {
		morph := view.Primitives[j].Morph
		block := model.AnimMorph{Binding: morph}
		if morph.Morphed() {
			slots := s.modelWeights[morph.SlotBase : morph.SlotBase+morph.Targets]
			block.Targets = model.SelectMorphTargets(slots, s.modelTargets[:0], path, s.once)
			s.modelTargets = block.Targets
		}
		s.modelMorphOffsets = append(s.modelMorphOffsets, s.build.appendAnim(s.modelPlays, block))
	}
	return at
}

// copyParams copies a Params List out into the frame's arena, which is the
// only route out of a List and costs no allocation into a reused backing.
func (s *scratch) copyParams(params *ecsscene.Params) []gfx.ParameterDescr {
	start := len(s.params)
	for _, param := range params.Values.All() {
		s.params = append(s.params, param)
	}
	return s.params[start:len(s.params):len(s.params)]
}

// copyMaterial rebuilds a Material Component as the material it describes, in
// the frame's arenas. Each tag's params are a full-slice window of tagParams,
// so no tag can append into the next one's. A Material with no tags is an
// empty material, which serves no pass.
func (s *scratch) copyMaterial(component *ecsscene.Material) material {
	start := len(s.tags)
	for _, tag := range component.Tags.All() {
		first := len(s.tagParams)
		for _, param := range tag.Params.All() {
			s.tagParams = append(s.tagParams, param)
		}
		own := s.tagParams[first:len(s.tagParams):len(s.tagParams)]
		s.tags = append(s.tags, materialTag{
			tag:   tag.Tag,
			descr: gfx.MaterialWithState(tag.Shader, tag.State, own...),
		})
	}
	return s.tags[start:len(s.tags):len(s.tags)]
}
