package internal

import (
	"io/fs"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
)

// keyer is one run's handles, bundled so keying one Entity is one call.
type keyer struct {
	k         kernel.Kernel
	lookup    *model.Lookup
	resources *gfx.ResourceQueue
	compile   gfx.ShaderCompiler
	fsys      fs.FS
	models    *ecs.Get[Model]
	meshes    *ecs.Get[Mesh]
	materials *ecs.Get[Material]
	params    *ecs.Get[Params]
}

// key recomputes everything keyScratch holds for one Entity from the live
// Stores, and the holds it keeps on scene's sets: the new ones are taken
// before the old ones are let go, so a set the Entity still draws through is
// never released and built again. An Entity that no longer has a Model or a
// Mesh, a despawned one included, leaves the scratch.
func (r *keyer) key(s *keyScratch, e ecs.Entity) {
	drawn, isModel := r.models.Of(e)
	mesh, isMesh := r.meshes.Of(e)
	var override *Material
	overrideKey := uint64(0)
	if material, ok := r.materials.Of(e); ok {
		override, overrideKey = &material, s.materialKey(&material)
	}
	var hash, shape uint64
	if p, ok := r.params.Of(e); ok {
		hash, shape = s.paramsHash(&p)
	}
	held, wasHeld := s.meshSets[e]
	if isMesh {
		s.meshes[e] = batchKey{mesh: mesh.Ref, material: overrideKey, params: hash}
		// A Mesh's file is the bundled PBR's ingredients, which the load System
		// ensured before keying anything.
		key := setKey{material: overrideKey, variant: model.VariantStatic, shape: shape}
		r.acquire(s, key, &s.bundled, override)
		s.meshSets[e] = key
	} else {
		delete(s.meshes, e)
		delete(s.meshSets, e)
	}
	if wasHeld {
		r.release(s, held)
	}
	if isModel {
		r.keyModel(s, e, drawn.Ref, override, overrideKey, hash, shape)
	} else {
		for _, key := range s.dropModel(e) {
			r.release(s, key)
		}
	}
}

// keyModel resolves a Model's ModelRef, loading the model if needed, and keys
// each primitive of the view its selectors resolve to.
func (r *keyer) keyModel(
	s *keyScratch, e ecs.Entity, ref model.ModelRef,
	override *Material, overrideKey uint64, params, shape uint64,
) {
	entry, ok := s.models[e]
	if !ok {
		entry.keys = s.spareKeys()
	}
	sets, ok := s.modelSets[e]
	if !ok {
		sets = takeSpare(&s.spareSets)
	}
	s.held = append(s.held[:0], sets...)
	entry.handle, entry.keys, sets = r.primitiveKeys(
		s, entry.keys[:0], sets[:0], ref, override, overrideKey, params, shape)
	s.models[e] = entry
	s.modelSets[e] = sets
	for _, key := range s.held {
		r.release(s, key)
	}
	s.held = s.held[:0]
}

// primitiveKeys appends one Batch key per primitive of the view ref resolves
// to, and one hold on each of scene's sets a primitive draws through, and
// returns the model's handle. A model that does not load is reported by the
// Lookup, once, and stays unloaded: every load failure but a missing backend
// is cached, so touching the Component again is what keys it again. It and a
// selector that matches nothing both return the zero handle and no keys.
func (r *keyer) primitiveKeys(
	s *keyScratch, keys []batchKey, sets []setKey, ref model.ModelRef,
	override *Material, overrideKey uint64, params, shape uint64,
) (model.ModelHandle, []batchKey, []setKey) {
	handle, ok := r.lookup.Resolve(r.k, r.fsys, r.resources, r.compile, ref)
	if !ok {
		return 0, keys, sets
	}
	view, err, ok := model.NewLookupReadAccess(r.lookup).View(handle, ref.Scene, ref.Node)
	if !ok {
		return 0, keys, sets
	}
	if err != nil {
		r.k.ReportErrorOnce(err.ReportKey(), err)
		return 0, keys, sets
	}
	skin := view.Animation.Skin()
	for j := range view.Primitives {
		primitive := &view.Primitives[j]
		// The file's own material, keyed at load for each shader variant; the
		// variant is what this primitive deforms, exactly as the draw picks
		// it. An override is laid over it, so the pair is the key.
		variant := model.VariantFor(
			skin.Bound && primitive.Skinned, skin.Morphed && primitive.Morph.Morphed())
		owned := &view.Materials[primitive.Material]
		file := owned.Key[variant]
		material := forwardMaterialKey(file)
		if override != nil {
			material = overlaidMaterialKey(overrideKey, file)
		}
		keys = append(keys, batchKey{
			model: handle, mesh: primitive.Mesh, material: material, params: params,
		})
		// A file material drawn as the file says draws the set model built at
		// load; the recording System reads that set's bindings off the
		// bundled shader scene compiles for it.
		if s.sets.ownSet(override != nil, shape) {
			r.ensureBundled(&s.sets, variant)
			continue
		}
		key := setKey{material: material, variant: variant, shape: shape}
		r.acquire(s, key, &owned.MaterialIngredients, override)
		sets = append(sets, key)
	}
	return handle, keys, sets
}

// resetSets releases every one of scene's sets and takes back every hold on
// them, because the default scene shader they were built under changed, and
// queues every drawable to be keyed again under the new one. A model's own
// sets are model's, and stay.
func resetSets(k kernel.Kernel, resources *gfx.ResourceQueue, s *keyScratch, current model.SceneShaderDescr) {
	c := &s.sets
	for _, cached := range c.sets {
		for i := range cached.material {
			if set := cached.material[i].set; set != 0 {
				resources.ReleaseDrawParams(k, set)
			}
		}
	}
	clear(c.sets)
	c.defaultShader = current
	c.defaultBundled = current.Source == (gfx.ShaderDescr{}) && len(current.Params) == 0
	for e, sets := range s.modelSets {
		s.modelSets[e] = sets[:0]
	}
	for e := range s.models {
		s.touch(e)
	}
	clear(s.meshSets)
	for e := range s.meshes {
		s.touch(e)
	}
}
