package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
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
// first Entity bucketed into it: its mesh, its material and its Params. It
// returns -1 for a Batch that cannot draw.
//
// The primitive's own material is the base whatever the Entity says: an
// Entity's Material is laid over it, tag by tag, rather than put in its place.
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
	if p, ok := frame.params.Of(e); ok {
		b.params = s.copyParams(&p)
	}
	owned := &view.Materials[primitive.Material]
	override, hasMaterial := frame.materials.Of(e)
	if b.interned, ok = s.materials.lookup(materialKey); !ok {
		var own material
		switch {
		case hasMaterial:
			own = s.resolveMaterial(&override, &owned.MaterialIngredients, variant)
		case s.bundledDefault:
			// The file's own material under the bundled shader is what the
			// load already built, so it is drawn untouched.
			own = s.forwardMaterial(owned.Forward[variant])
		default:
			own = s.forwardMaterial(s.resolveDescr(
				&owned.MaterialIngredients, gfx.ShaderDescr{}, gfx.MaterialState{}, nil, variant))
		}
		b.interned = s.materials.intern(s.k, s.recorded(own), materialKey)
	}
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

// newMeshBatch resolves a Mesh Batch from its first Entity. A Mesh's "file" is
// the bundled PBR's ingredients - white and flat in every slot, opaque, white
// paint - and it takes the same path a Model primitive does: with no Material
// it draws the default scene shader over them, and a Material is laid over
// them tag by tag.
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
	override, hasMaterial := frame.materials.Of(e)
	if !mesh.Standard && !hasMaterial {
		s.reportMeshOnce(ref, ErrMeshCustomLayoutNeedsMaterial{Mesh: ref.ID()})
		return -1
	}
	b := batch{mesh: mesh}
	if p, ok := frame.params.Of(e); ok {
		b.params = s.copyParams(&p)
	}
	bundled := &frame.keys.bundled
	b.interned = int32(model.VariantStatic)
	if !hasMaterial && !s.bundledRecorded[model.VariantStatic] {
		// The table interned this material at begin, and the window it holds
		// is the one recorded here, in place.
		s.recorded(s.bundled[model.VariantStatic])
		s.bundledRecorded[model.VariantStatic] = true
	}
	if hasMaterial {
		if b.interned, ok = s.materials.lookup(materialKey); !ok {
			b.interned = s.materials.intern(s.k, s.recorded(s.resolveMaterial(&override, bundled, model.VariantStatic)), materialKey)
		}
	}
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
func (s *scratch) copyParams(params *Params) []gfx.ParameterDescr {
	start := len(s.params)
	for _, param := range params.Values.All() {
		s.params = append(s.params, param)
	}
	return s.params[start:len(s.params):len(s.params)]
}

// recorded records every tag of a material the frame interns into gfx's queue,
// once, so that every draw of it names the queue's copy: gfx copies a
// material's params into its queue on every draw it is not recorded for, and a
// Batch's material is drawn once per Batch per pass.
func (s *scratch) recorded(material material) material {
	for i := range material {
		material[i].descr = s.queue.FrameMaterial(material[i].descr)
	}
	return material
}

// resolveMaterial resolves a Material Component over one file material's
// ingredients, one gfx material per tag, in the frame's arenas. Each tag's
// params are a full-slice window of tagParams, so no tag can append into the
// next one's. A Material with no tags is an empty material, which serves no
// pass.
func (s *scratch) resolveMaterial(
	component *Material, file *model.MaterialIngredients, variant model.ShaderVariant,
) material {
	start := len(s.tags)
	for _, tag := range component.Tags.All() {
		s.tags = append(s.tags, materialTag{
			tag:   tag.Tag,
			descr: s.resolveDescr(file, tag.Shader, tag.State, &tag.Params, variant),
		})
	}
	return s.tags[start:len(s.tags):len(s.tags)]
}

// resolveDescr is the one resolution every draw's material goes through, for
// one tag or for none:
//
//   - the shader is the tag's, or the default scene shader where it names
//     none, under the variant's defines whichever it is;
//   - the params are the file's - its textures and samplers, and its numbers,
//     which the shader in effect reads as uniform members if it declares
//     them - overlaid by name with the default scene shader's and then the
//     tag's;
//   - the state is the tag's, or the file's where it names none.
//
// The Params Component is not merged here: it rides on the draw, and gfx lays
// a draw's params over its material's by name. Nothing here knows what any
// param means - a Batch binds whatever the shader in effect declares.
func (s *scratch) resolveDescr(
	file *model.MaterialIngredients, shader gfx.ShaderDescr, state gfx.MaterialState,
	own *m.List[gfx.ParameterDescr], variant model.ShaderVariant,
) gfx.MaterialDescr {
	if shader == (gfx.ShaderDescr{}) {
		shader = s.defaultShader.Source
	}
	if state == (gfx.MaterialState{}) {
		state = file.State
	}
	start := len(s.tagParams)
	s.tagParams = append(s.tagParams, file.Params...)
	s.tagParams = overlayParams(s.tagParams, start, s.defaultShader.Params)
	if own != nil {
		for _, param := range own.All() {
			s.tagParams = overlayParam(s.tagParams, start, param)
		}
	}
	params := s.tagParams[start:len(s.tagParams):len(s.tagParams)]
	return gfx.MaterialWithState(s.variantShader(shader, variant), state, params...)
}

// variantShader is model.VariantShader, kept across frames: see
// scratch.variants.
func (s *scratch) variantShader(shader gfx.ShaderDescr, variant model.ShaderVariant) gfx.ShaderDescr {
	if variant == model.VariantStatic && shader != (gfx.ShaderDescr{}) {
		return shader
	}
	key := variantShaderKey{shader: shader, variant: variant}
	if resolved, ok := s.variants[key]; ok {
		return resolved
	}
	resolved := model.VariantShader(shader, variant)
	s.variants[key] = resolved
	return resolved
}

// overlayParams lays params over the window arena[start:] by name, in order:
// a param whose name the window holds replaces that one in place, and any
// other is appended. The window's order is kept, so the file's params stay
// where they were and nothing is bound twice.
func overlayParams(arena []gfx.ParameterDescr, start int, params []gfx.ParameterDescr) []gfx.ParameterDescr {
	for _, param := range params {
		arena = overlayParam(arena, start, param)
	}
	return arena
}

// overlayParam is overlayParams for one param.
func overlayParam(arena []gfx.ParameterDescr, start int, param gfx.ParameterDescr) []gfx.ParameterDescr {
	name := param.Name()
	for i := start; i < len(arena); i++ {
		if arena[i].Name() == name {
			arena[i] = param
			return arena
		}
	}
	return append(arena, param)
}
