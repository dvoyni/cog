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
	fsys      fs.FS
	models    *ecs.Get[Model]
	meshes    *ecs.Get[Mesh]
	materials *ecs.Get[Material]
	params    *ecs.Get[Params]
}

// key recomputes everything keyScratch holds for one Entity from the live
// Stores. An Entity that no longer has a Model or a Mesh, a despawned one
// included, leaves the scratch.
func (r *keyer) key(s *keyScratch, e ecs.Entity) {
	drawn, isModel := r.models.Of(e)
	mesh, isMesh := r.meshes.Of(e)
	if !isModel {
		s.dropModel(e)
	}
	if !isMesh {
		delete(s.meshes, e)
	}
	if !isModel && !isMesh {
		return
	}
	override, hasOverride := uint64(0), false
	if material, ok := r.materials.Of(e); ok {
		override, hasOverride = s.materialKey(&material), true
	}
	var hash uint64
	if p, ok := r.params.Of(e); ok {
		hash = s.paramsHash(&p)
	}
	if isMesh {
		s.meshes[e] = batchKey{mesh: mesh.Ref, material: override, params: hash}
	}
	if isModel {
		r.keyModel(s, e, drawn.Ref, override, hasOverride, hash)
	}
}

// keyModel resolves a Model's ModelRef, loading the model if needed, and keys
// each primitive of the view its selectors resolve to.
func (r *keyer) keyModel(
	s *keyScratch, e ecs.Entity, ref model.ModelRef,
	override uint64, hasOverride bool, params uint64,
) {
	entry, ok := s.models[e]
	if !ok {
		entry.keys = s.spareKeys()
	}
	entry.handle, entry.keys = r.primitiveKeys(entry.keys[:0], ref, override, hasOverride, params)
	s.models[e] = entry
}

// primitiveKeys appends one Batch key per primitive of the view ref resolves
// to, and returns the model's handle. A model that does not load is reported
// by the Lookup, once, and stays unloaded: every load failure but a missing
// backend is cached, so touching the Component again is what keys it again. It
// and a selector that matches nothing both return the zero handle and no keys.
func (r *keyer) primitiveKeys(
	keys []batchKey, ref model.ModelRef,
	override uint64, hasOverride bool, params uint64,
) (model.ModelHandle, []batchKey) {
	handle, ok := r.lookup.Resolve(r.k, r.fsys, r.resources, ref)
	if !ok {
		return 0, keys
	}
	view, err, ok := model.NewLookupReadAccess(r.lookup).View(handle, ref.Scene, ref.Node)
	if !ok {
		return 0, keys
	}
	if err != nil {
		r.k.ReportErrorOnce(err.ReportKey(), err)
		return 0, keys
	}
	skin := view.Animation.Skin()
	for j := range view.Primitives {
		primitive := &view.Primitives[j]
		// The file's own material, keyed at load for each shader variant; the
		// variant is what this primitive deforms, exactly as the draw picks
		// it. An override is laid over it, so the pair is the key.
		variant := model.VariantFor(
			skin.Bound && primitive.Skinned, skin.Morphed && primitive.Morph.Morphed())
		file := view.Materials[primitive.Material].Key[variant]
		material := forwardMaterialKey(file)
		if hasOverride {
			material = overlaidMaterialKey(override, file)
		}
		keys = append(keys, batchKey{
			model: handle, mesh: primitive.Mesh, material: material, params: params,
		})
	}
	return handle, keys
}
