package internal

import (
	"encoding/binary"
	"hash/maphash"
	"io/fs"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

// batchKey is what one instanced draw is keyed by: Entities whose draws have
// equal keys share a Batch, and anything that changes the key splits one.
//
//   - A Model primitive is (model, mesh, material, params). The primitive is
//     named by the mesh it draws, whatever Scene or Node selector reached it,
//     because everything else a primitive carries - its Local, its bounds, its
//     skin flags and its morph block - rides in the instance record.
//   - A Mesh is (mesh, material, params), with model zero.
//
// material is the model material's load-time key for a file's own material,
// taken through forwardMaterialKey, or the key of the Entity's Material
// override. Zero is the bundled PBR a Mesh with no Material draws with, and no
// real key is zero. params is the Params hash, zero for no parameters.
//
// Layers, culling and the camera are not in it: they filter a Batch's
// instances per pass.
type batchKey struct {
	model    model.ModelHandle
	mesh     model.MeshRef
	material uint64
	params   uint64
}

// modelKeys is what the load System keeps for one Model Entity: the handle its
// ModelRef resolved to, and one batchKey per primitive of the view its
// selectors resolved to, in the view's order. A model that is not resident, or
// a selector that matched nothing, keeps the zero handle and no keys, and draws
// nothing.
type modelKeys struct {
	handle model.ModelHandle
	keys   []batchKey
}

// keyScratch is ecsscene's scratch for what the load System writes and the
// recording System reads: each drawable Entity's handle and Batch keys, keyed
// by Entity. It is never a Component, because writing a Component would make
// the load System that Component's writer.
//
// It is a resource the plugin owns, so it is in both Systems' lock sets.
type keyScratch struct {
	models map[ecs.Entity]modelKeys
	meshes map[ecs.Entity]batchKey
	// pending is every Entity a Hook named and the load System has not keyed
	// yet, and seen the same Entities as a set, so an Entity several Hooks
	// name is keyed once. They outlive a run only while the backend is not
	// ready, which is the one load failure that does not stick.
	pending []ecs.Entity
	seen    map[ecs.Entity]struct{}
	// spare holds the key slices of Entities that stopped being Models, reused
	// by the next Model Entity so spawn and despawn churn stops allocating.
	spare [][]batchKey
	// params and tagParams are reused backings a List is copied out into
	// before it is hashed.
	params    []gfx.ParameterDescr
	tagParams []gfx.ParameterDescr
	// walked is how many Entities the last run keyed. A steady frame keys
	// none.
	walked int
	// ready is whether this tick's backend is up, and bundled the bundled
	// PBR's four forward materials, which the load System ensures once the
	// backend is: the recording System holds the Lookup only for reading, so
	// it takes both from here rather than baking anything itself.
	ready      bool
	hasBundled bool
	bundled    [model.VariantCount]gfx.MaterialDescr
}

func newKeyScratch() *keyScratch {
	return &keyScratch{
		models: map[ecs.Entity]modelKeys{},
		meshes: map[ecs.Entity]batchKey{},
		seen:   map[ecs.Entity]struct{}{},
	}
}

// model reports a Model Entity's handle and its primitives' Batch keys.
func (s *keyScratch) model(e ecs.Entity) (modelKeys, bool) {
	entry, ok := s.models[e]
	return entry, ok
}

// mesh reports a Mesh Entity's Batch key.
func (s *keyScratch) mesh(e ecs.Entity) (batchKey, bool) {
	key, ok := s.meshes[e]
	return key, ok
}

// touch queues an Entity for keying, once.
func (s *keyScratch) touch(e ecs.Entity) {
	if _, ok := s.seen[e]; ok {
		return
	}
	s.seen[e] = struct{}{}
	s.pending = append(s.pending, e)
}

// loadSystem is the load System. It is the only ecsscene System holding the
// Lookup for writing, so it is the one that loads, and it drives model's bake
// and release queues: nothing else in an ecsscene app drains them, because an
// app runs ecsscene or scene and never both.
//
// It runs on what changed: every addition, change and removal of a Model,
// Mesh, Material or Params since its last run, each Entity once. For each it
// resolves the ModelRef to a ModelHandle, loading the model if needed, and
// computes the Batch keys into keyScratch. In a steady frame no Hook names
// anything, so it walks no Entity and hashes nothing; what is left is the
// drain of two empty queues.
//
// Everything it reads from the Stores is a read, so it keeps no System off
// them. What it writes is exclusive by nature: the Lookup, and the resource
// queue a load uploads through.
func loadSystem(
	k kernel.Kernel,
	modelHooks *ecs.Hooks[ecsscene.Model, ecs.HookAll],
	meshHooks *ecs.Hooks[ecsscene.Mesh, ecs.HookAll],
	materialHooks *ecs.Hooks[ecsscene.Material, ecs.HookAll],
	paramsHooks *ecs.Hooks[ecsscene.Params, ecs.HookAll],
	models *ecs.Get[ecsscene.Model],
	meshes *ecs.Get[ecsscene.Mesh],
	materials *ecs.Get[ecsscene.Material],
	params *ecs.Get[ecsscene.Params],
	lookupResource *ecs.Write[*model.Lookup],
	filesystem *ecs.Read[storage.FileSystem],
	resourceQueue *ecs.Write[*gfx.ResourceQueue],
	work *ecs.Write[*keyScratch],
) {
	s := work.Get()
	s.walked = 0
	for e := range modelHooks.All() {
		s.touch(e)
	}
	for e := range meshHooks.All() {
		s.touch(e)
	}
	for e := range materialHooks.All() {
		s.touch(e)
	}
	for e := range paramsHooks.All() {
		s.touch(e)
	}
	resources := resourceQueue.Get()
	// A load before the backend is up is refused and not cached, so the
	// Entities wait for the first frame that has one. Nothing is drained
	// either, and the recording System skips the frame too.
	s.ready = resources != nil && resources.Ready()
	if !s.ready {
		return
	}
	lookup := lookupResource.Get()
	// The bundled PBR bakes its two default textures the first time, and
	// the Lookup returns the same four materials forever after.
	if !s.hasBundled {
		s.bundled = lookup.EnsureBundled(func(width, height int, format gfx.TextureFormat, pixels []byte) gfx.TextureDescr {
			return resources.BakeTexture(width, height, format, pixels, true, false)
		})
		s.hasBundled = true
	}
	if len(s.pending) > 0 {
		keyer := keyer{
			k: k, lookup: lookup, resources: resources,
			// The one boxed filesystem a run pays for, and only a run that
			// has something to key.
			fsys:      fs.FS(filesystem.Get()),
			models:    models,
			meshes:    meshes,
			materials: materials,
			params:    params,
		}
		for _, e := range s.pending {
			keyer.key(s, e)
		}
		s.walked = len(s.pending)
		clear(s.pending)
		s.pending = s.pending[:0]
		clear(s.seen)
	}
	lookup.DrainMeshes(model.MeshBaker{
		Bake: func(data []byte) gfx.BufferDescr { return resources.BakeBuffer(data, false) },
		Rebake: func(buffer gfx.BufferDescr, data []byte) gfx.BufferDescr {
			return resources.ReBakeBuffer(buffer, data, false)
		},
		Release: resources.ReleaseBuffer,
	})
}

// keyer is one run's handles, bundled so keying one Entity is one call.
type keyer struct {
	k         kernel.Kernel
	lookup    *model.Lookup
	resources *gfx.ResourceQueue
	fsys      fs.FS
	models    *ecs.Get[ecsscene.Model]
	meshes    *ecs.Get[ecsscene.Mesh]
	materials *ecs.Get[ecsscene.Material]
	params    *ecs.Get[ecsscene.Params]
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
		material := override
		if !hasOverride {
			// The file's own material, keyed at load for each shader
			// variant; the variant is what this primitive deforms, exactly as
			// the draw picks it.
			variant := model.VariantFor(
				skin.Bound && primitive.Skinned, skin.Morphed && primitive.Morph.Morphed())
			material = forwardMaterialKey(view.Materials[primitive.Material].Key[variant])
		}
		keys = append(keys, batchKey{
			model: handle, mesh: primitive.Mesh, material: material, params: params,
		})
	}
	return handle, keys
}

// dropModel takes an Entity's model keys out of the scratch, keeping the
// backing for the next Model Entity.
func (s *keyScratch) dropModel(e ecs.Entity) {
	entry, ok := s.models[e]
	if !ok {
		return
	}
	delete(s.models, e)
	if cap(entry.keys) > 0 {
		s.spare = append(s.spare, entry.keys[:0])
	}
}

// spareKeys hands out a kept key backing, or nil when there is none.
func (s *keyScratch) spareKeys() []batchKey {
	n := len(s.spare)
	if n == 0 {
		return nil
	}
	keys := s.spare[n-1]
	s.spare[n-1] = nil
	s.spare = s.spare[:n-1]
	return keys
}

// paramsHash is the Params hash: gfx's own fingerprint of the parameters, so
// equal values hash equal whichever List holds them. No parameters is zero,
// the same as no Params, because both draw the same; any other hash that lands
// on zero reads as one.
func (s *keyScratch) paramsHash(p *ecsscene.Params) uint64 {
	if p.Values.Len() == 0 {
		return 0
	}
	values := s.params[:0]
	for _, param := range p.Values.All() {
		values = append(values, param)
	}
	s.params = values
	hash := nonZero(gfx.FingerprintParams(values))
	clear(values)
	return hash
}

// materialKey keys a Material override by content: each tag's pass and gfx
// fingerprint, in order. A Material with no tags is a key too, because it
// draws differently from no Material.
func (s *keyScratch) materialKey(material *ecsscene.Material) uint64 {
	var h maphash.Hash
	h.SetSeed(materialSeed)
	for _, tag := range material.Tags.All() {
		params := s.tagParams[:0]
		for _, param := range tag.Params.All() {
			params = append(params, param)
		}
		s.tagParams = params
		descr := gfx.MaterialWithState(tag.Shader, tag.State, params...)
		writeMaterialEntry(&h, tag.Tag, descr.Fingerprint())
		clear(params)
	}
	return nonZero(h.Sum64())
}

// materialSeed is fixed for the process: a key compares only against keys
// taken in the same process.
var materialSeed = maphash.MakeSeed()

// forwardMaterialKey is materialKey of a Material whose one tag is the forward
// pass, given that tag's gfx fingerprint. A model material carries its
// fingerprint from its load, so a file's own material is keyed without
// fingerprinting anything, and an override naming the same forward material
// keys the same.
func forwardMaterialKey(fingerprint uint64) uint64 {
	var h maphash.Hash
	h.SetSeed(materialSeed)
	writeMaterialEntry(&h, ecsscene.TagForward, fingerprint)
	return nonZero(h.Sum64())
}

// writeMaterialEntry hashes one tag of a material: its pass, with an empty tag
// read as the forward pass, then its gfx fingerprint.
func writeMaterialEntry(h *maphash.Hash, tag ecsscene.PassTag, fingerprint uint64) {
	if tag == "" {
		tag = ecsscene.TagForward
	}
	h.WriteString(string(tag))
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], fingerprint)
	h.Write(buf[:])
}

// nonZero reads a hash that lands on zero as one, so zero keeps meaning none.
func nonZero(sum uint64) uint64 {
	if sum == 0 {
		return 1
	}
	return sum
}
