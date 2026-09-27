package internal

import (
	"encoding/binary"
	"hash/maphash"

	"github.com/dvoyni/cog/bundles/ecs"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/slots/gfx"
)

// materialSeed is fixed for the process: a key compares only against keys
// taken in the same process.
var materialSeed = maphash.MakeSeed()

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
// taken through forwardMaterialKey. An Entity's Material overlays that file
// material rather than replacing it, so under one the key is the pair,
// overlaidMaterialKey of the Material's key and the file material's; a Mesh's
// "file" is the bundled PBR's ingredients, which are one for every Mesh, so
// its key is the Material's alone. Zero is the bundled PBR a Mesh with no
// Material draws with, and no real key is zero. params is the Params hash, zero
// for no parameters.
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

// keyScratch is scene's scratch for what the load System writes and the
// recording System reads: each drawable Entity's handle and Batch keys, keyed
// by Entity. It is never a Component, because writing a Component would make
// the load System that Component's writer.
//
// It is a resource the plugin owns, so it is in both Systems' lock sets.
type keyScratch struct {
	models map[ecs.Entity]modelKeys
	meshes map[ecs.Entity]batchKey
	// modelSets is every one of scene's sets a Model Entity's primitives
	// hold, one hold per primitive drawing through one, and meshSets the set
	// each Mesh Entity holds. They are the load System's alone, beside the
	// keys rather than in them, so the recording System's per-instance map
	// reads stay as small as they were.
	modelSets map[ecs.Entity][]setKey
	meshSets  map[ecs.Entity]setKey
	// sets is scene's own draw params, which the keys above hold.
	sets setCache
	// held is scratch for the holds an Entity gave up while it is keyed
	// again, released once its new ones are taken.
	held []setKey
	// pending is every Entity a Hook named and the load System has not keyed
	// yet, and seen the same Entities as a set, so an Entity several Hooks
	// name is keyed once. They outlive a run only while the backend is not
	// ready, which is the one load failure that does not stick.
	pending []ecs.Entity
	seen    map[ecs.Entity]struct{}
	// spare and spareSets hold the key and set slices of Entities that
	// stopped being Models, reused by the next Model Entity so spawn and
	// despawn churn stops allocating.
	spare     [][]batchKey
	spareSets [][]setKey
	// params and tagParams are reused backings a List is copied out into
	// before it is hashed.
	params    []gfx.ShaderParameterDescr
	tagParams []gfx.ShaderParameterDescr
	// walked is how many Entities the last run keyed. A steady frame keys
	// none.
	walked int
	// ready is whether this tick's backend is up, and bundled the bundled
	// PBR's ingredients - a Mesh's "file" - which the load System ensures once
	// the backend is: the recording System holds the Lookup only for reading,
	// so it takes both from here rather than baking anything itself.
	ready      bool
	hasBundled bool
	bundled    model.MaterialIngredients
}

func newKeyScratch() *keyScratch {
	return &keyScratch{
		models:    map[ecs.Entity]modelKeys{},
		meshes:    map[ecs.Entity]batchKey{},
		modelSets: map[ecs.Entity][]setKey{},
		meshSets:  map[ecs.Entity]setKey{},
		sets:      newSetCache(),
		seen:      map[ecs.Entity]struct{}{},
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

// dropModel takes an Entity's model keys out of the scratch, keeping the
// backings for the next Model Entity, and returns the sets it held.
func (s *keyScratch) dropModel(e ecs.Entity) []setKey {
	held := s.modelSets[e]
	delete(s.modelSets, e)
	if cap(held) > 0 {
		s.spareSets = append(s.spareSets, held[:0])
	}
	entry, ok := s.models[e]
	if !ok {
		return held
	}
	delete(s.models, e)
	if cap(entry.keys) > 0 {
		s.spare = append(s.spare, entry.keys[:0])
	}
	return held
}

// spareKeys hands out a kept key backing, or nil when there is none.
func (s *keyScratch) spareKeys() []batchKey {
	return takeSpare(&s.spare)
}

// takeSpare hands out the last backing a spare list keeps, or nil.
func takeSpare[T any](spare *[][]T) []T {
	n := len(*spare)
	if n == 0 {
		return nil
	}
	backing := (*spare)[n-1]
	(*spare)[n-1] = nil
	*spare = (*spare)[:n-1]
	return backing
}

// paramsHash is the Params hash and binding shape: gfx's own fingerprint of
// the parameters, so equal values hash equal whichever List holds them, and
// paramsShape of their names. No parameters is zero for both, the same as no
// Params, because both draw the same; any other hash that lands on zero reads
// as one.
func (s *keyScratch) paramsHash(p *Params) (hash, shape uint64) {
	if p.Values.Len() == 0 {
		return 0, 0
	}
	values := s.params[:0]
	for _, param := range p.Values.All() {
		values = append(values, param)
	}
	s.params = values
	hash, shape = nonZero(gfx.FingerprintParams(values)), paramsShape(values)
	clear(values)
	return hash, shape
}

// materialKey keys a Material override by content: each tag's pass, its params'
// gfx fingerprint, its shader by the whole descriptor, supply included, and its
// state, in order. A Material with no tags is a key too, because it draws
// differently from no Material.
func (s *keyScratch) materialKey(material *Material) uint64 {
	var h maphash.Hash
	h.SetSeed(materialSeed)
	for _, tag := range material.Tags.All() {
		params := s.tagParams[:0]
		for _, param := range tag.Params.All() {
			params = append(params, param)
		}
		s.tagParams = params
		writeMaterialEntry(&h, tag.Tag, gfx.FingerprintParams(params))
		maphash.WriteComparable(&h, tag.Shader)
		maphash.WriteComparable(&h, tag.State)
		clear(params)
	}
	return nonZero(h.Sum64())
}

// forwardMaterialKey keys a file's own material under the forward pass, given
// the key model took of it at load, so a file's own material is keyed without
// fingerprinting anything.
func forwardMaterialKey(fingerprint uint64) uint64 {
	var h maphash.Hash
	h.SetSeed(materialSeed)
	writeMaterialEntry(&h, TagForward, fingerprint)
	return nonZero(h.Sum64())
}

// overlaidMaterialKey keys a Material laid over one file material: the
// Material's key and the file material's key together, because the
// same Material over two file materials resolves to two materials, each with
// its own textures and state.
func overlaidMaterialKey(override, fingerprint uint64) uint64 {
	var h maphash.Hash
	h.SetSeed(materialSeed)
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[:8], override)
	binary.LittleEndian.PutUint64(buf[8:], fingerprint)
	h.Write(buf[:])
	return nonZero(h.Sum64())
}

// writeMaterialEntry hashes one tag of a material: its pass, with an empty tag
// read as the forward pass, then a fingerprint of what it draws.
func writeMaterialEntry(h *maphash.Hash, tag PassTag, fingerprint uint64) {
	if tag == "" {
		tag = TagForward
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
