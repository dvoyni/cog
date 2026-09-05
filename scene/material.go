package scene

import (
	"encoding/binary"
	"hash/maphash"

	"github.com/dvoyni/cog/gfx"
)

// MaterialTag binds one pass tag to the gfx material that serves it.
//
// A tag entry is a whole gfx.MaterialDescr rather than a shader, because two
// independent things vary per tag. Pipeline state is strictly per material with
// no pass or draw override, so a shadow pass takes its cull mode from its own
// entry; and a declared-but-unused WGSL binding is still reflected and must be
// bound, so the parameter set is tag-specific too — an alphaMode MASK shadow
// shader declares baseColorTexture and alphaCutoff, an opaque one declares
// neither.
type MaterialTag struct {
	Tag   PassTag // zero reads as TagForward
	Descr gfx.MaterialDescr
}

// Material is a scene material: the gfx materials it serves, one per pass tag.
// A pass whose tag has no entry skips every draw using this material, so tag
// participation is purely a material property — a draw gets no say in which
// passes it appears in.
//
// A nil Material is the bundled PBR, so every draw literal that omits the field
// is untouched, and the hand-written one-entry case is
// Material{{Descr: descr}}. In v1 the only tag is forward; when shadows land
// they add a shadow entry to that same value and every draw that passed nil
// gains shadow casting with no call-site change.
type Material []MaterialTag

// tag reads an unwritten entry tag as the forward pass, matching Pass.
func (t MaterialTag) tag() PassTag {
	if t.Tag == "" {
		return TagForward
	}
	return t.Tag
}

// tagID is a pass tag interned to a dense index. Tags intern once per pass, not
// once per draw, so a draw never compares a string.
type tagID int32

// noEntry marks a tag this material does not serve. It is negative so that the
// per-draw test is one array read and a sign test.
const noEntry int32 = -1

// materialEntry is one resolved (material, tag) pair: the gfx material to draw
// with and the dense id the batch reports.
type materialEntry struct {
	descr *gfx.MaterialDescr
	// index is the entry's position in the caller's Material, kept because a
	// duplicate-tag ruling is only checkable by which entry won.
	index      int32
	materialID uint32
	// blend is the entry's sort class: true for anything that blends against
	// the target and so must draw back to front. Alpha-masked is opaque plus a
	// shader discard, and lands in the opaque class through its state alone.
	blend bool
}

// materialKey identifies one caller material by content: a fingerprint of each
// entry's tag and gfx material - shader source-or-path, pipeline state and
// parameter bytes. A caller-supplied gfx.MaterialDescr has no id of its own,
// and keying by the slice's backing instead would hand the spec's own idiom,
// Material{{Descr: descr}} built inline per draw, a fresh id every draw and so
// never sort two of them together. The hash costs one map probe per draw that
// names a material, once per frame, and nothing for the bundled PBR.
type materialKey uint64

var materialSeed = maphash.MakeSeed()

func (m Material) key() materialKey {
	var h maphash.Hash
	h.SetSeed(materialSeed)
	var buf [8]byte
	for i := range m {
		h.WriteString(string(m[i].tag()))
		binary.LittleEndian.PutUint64(buf[:], m[i].Descr.Fingerprint())
		h.Write(buf[:])
	}
	return materialKey(h.Sum64())
}

// internedMaterial is one material resolved against every tag it serves.
// entries and ids are indexed by tagID, so a draw's per-pass cost is one array
// read plus a negative-means-skip test.
type internedMaterial struct {
	material Material
	entries  []int32
	ids      []uint32
}

// materialTable interns pass tags and caller materials. Tags live for the
// engine's lifetime, because the tag set is a property of the passes an app
// declares rather than of a frame; materials are per frame.
type materialTable struct {
	tags     map[PassTag]tagID
	tagNames []PassTag
	// keys and interned are the frame's materials. Both keep their backing
	// across frames, so a steady frame interns without allocating.
	keys     map[materialKey]int32
	interned []internedMaterial
	nextID   uint32
}

// reset starts a frame, interning the bundled PBR first so that a draw naming
// no material of its own resolves with no map probe at all.
func (t *materialTable) reset(bundled Material) {
	if t.keys == nil {
		t.keys = map[materialKey]int32{}
		t.tags = map[PassTag]tagID{}
	}
	clear(t.keys)
	t.interned = t.interned[:0]
	t.nextID = 0
	t.add(discardBundledReports, bundled)
}

// internTag interns one pass tag. It is called once per pass, and an empty tag
// is the forward tag rather than a second name for it.
func (t *materialTable) internTag(tag PassTag) tagID {
	if tag == "" {
		tag = TagForward
	}
	if t.tags == nil {
		t.tags = map[PassTag]tagID{}
	}
	if id, ok := t.tags[tag]; ok {
		return id
	}
	id := tagID(len(t.tagNames))
	t.tagNames = append(t.tagNames, tag)
	t.tags[tag] = id
	return id
}

// entry reports the gfx material one interned material uses in one pass, and
// whether it serves that pass at all. This is what a draw pays per pass: one
// array read and a negative-means-skip test, with no string compare and no map
// probe - the probe happened once this frame, in intern.
func (t *materialTable) entry(interned int32, tag tagID) (materialEntry, bool) {
	material := &t.interned[interned]
	if int(tag) >= len(material.entries) {
		return materialEntry{}, false
	}
	index := material.entries[tag]
	if index < 0 {
		return materialEntry{}, false
	}
	descr := &material.material[index].Descr
	return materialEntry{
		descr:      descr,
		index:      index,
		materialID: material.ids[tag],
		blend:      descr.State().Blend != gfx.BlendOpaque,
	}, true
}

// intern returns the frame-local index of one caller material, interning it and
// checking its tags the first time the frame sees it. It is called once per
// recorded draw per frame, before any pass walks them, which is what keeps the
// fingerprint and the map probe out of the per-pass path.
func (t *materialTable) intern(report func(error), material Material) int32 {
	if material == nil {
		return 0
	}
	key := material.key()
	if index, ok := t.keys[key]; ok {
		return index
	}
	index := t.add(report, material)
	t.keys[key] = index
	return index
}

// add resolves one material's tags into the dense entry and id arrays. Every
// tag the material names is interned here, so a tag first seen later in the
// frame is one this material does not serve — which is why resolve can treat a
// tag past the end of entries as a skip rather than a recheck.
//
// The duplicate check runs here, once per material per frame, over a slice of
// one or two entries; it is not a per-draw cost.
func (t *materialTable) add(report func(error), material Material) int32 {
	record := t.allocate()
	record.material = material
	for i := range material {
		t.internTag(material[i].tag())
	}
	record.entries = grow(record.entries, len(t.tagNames))
	record.ids = grow(record.ids, len(t.tagNames))
	for i := range record.entries {
		record.entries[i] = noEntry
	}
	for i := range material {
		tag := t.internTag(material[i].tag())
		if record.entries[tag] != noEntry {
			report(ErrMaterialTagAlreadyServed{Tag: material[i].tag()})
			continue
		}
		record.entries[tag] = int32(i)
		t.nextID++
		record.ids[tag] = t.nextID
	}
	return int32(len(t.interned) - 1)
}

// allocate takes the next interned slot, reusing the entry and id arrays the
// previous frame left in it.
func (t *materialTable) allocate() *internedMaterial {
	if len(t.interned) < cap(t.interned) {
		t.interned = t.interned[:len(t.interned)+1]
	} else {
		t.interned = append(t.interned, internedMaterial{})
	}
	return &t.interned[len(t.interned)-1]
}

// grow returns a slice of exactly n elements, reusing values' backing when it
// is large enough.
func grow[T any](values []T, n int) []T {
	if cap(values) >= n {
		return values[:n]
	}
	return make([]T, n)
}

// discardBundledReports is the report sink the bundled PBR interns through. It
// is scene's own material, checked by scene's own tests, so a report from it
// would be an engine bug rather than something a caller can act on.
func discardBundledReports(error) {}

// pbrDefaults are the 1x1 textures every absent texture slot binds. WGSL
// requires every declared binding bound and gfx does no preprocessing, so
// omitting a texture is not available without shader variants: scene owns the
// defaults and always binds all five.
//
// There are only two, because a single white texel serves baseColor,
// metallic-roughness, occlusion and emissive: 1.0 is a fixed point of the sRGB
// transfer curve, so the sRGB-format and linear-format slots both read 1.0 from
// it. The factor parameters multiply through unchanged.
type pbrDefaults struct {
	white      gfx.TextureDescr
	flatNormal gfx.TextureDescr
}

// pbrSampler is the sampler every default slot binds: glTF's own default wrap,
// which is repeat, filtered linearly. SamplerDesc stays comparable so
// gfx dedupes the five identical descriptors down to one GPU object.
var pbrSampler = gfx.SamplerDesc{AddressU: gfx.AddressRepeat, AddressV: gfx.AddressRepeat}

// pbrSlot is one texture slot of the bundled material: the glTF-verbatim
// parameter name of its texture and the scene-owned name of its sampler.
type pbrSlot struct {
	texture string
	sampler string
}

// pbrSlots are the five slots, in record order. Five samplers rather than one
// shared: glTF references a sampler per texture and two slots of one material
// can legitimately differ — a tiling ground beside a clamped decal — so a
// shared sampler would silently mis-sample a legal file.
var pbrSlots = [...]pbrSlot{
	{texture: "baseColorTexture", sampler: "baseColorSampler"},
	{texture: "metallicRoughnessTexture", sampler: "metallicRoughnessSampler"},
	{texture: "normalTexture", sampler: "normalSampler"},
	{texture: "occlusionTexture", sampler: "occlusionSampler"},
	{texture: "emissiveTexture", sampler: "emissiveSampler"},
}

// normalSlot is the one slot whose default is the flat normal rather than the
// white texel.
const normalSlot = 2

// bundledPbr builds the bundled PBR material: one forward entry and nothing
// else. Every draw that names no material of its own uses it.
//
// It carries no numeric parameters. The factors, the texture transforms and the
// alpha cutoff all live in the scenePbrMaterial record, which is a range of a
// per-frame arena scene packs itself and binds per batch — the binding is the
// addressing, so no index has to agree across the update/render thread
// boundary. What is left here is what gfx binds by name: the five textures and
// the five samplers.
//
// The scene-prefixed parameter name space is reserved for engine-supplied
// bindings, mirroring canvas's canvasTexture and canvasSampler. There is no
// separate system-bindings channel to keep in sync: sceneFrame, sceneInstances
// and scenePbrMaterial are injected as ordinary per-draw parameters and the
// existing matcher binds them exactly like a material texture. A caller
// material that names a scene* parameter is an app bug; gfx does not police it,
// and the material simply loses.
func bundledPbr(defaults pbrDefaults) Material {
	params := make([]gfx.ParameterDescr, 0, 2*len(pbrSlots))
	for i, slot := range pbrSlots {
		texture := defaults.white
		if i == normalSlot {
			texture = defaults.flatNormal
		}
		params = append(params,
			gfx.TextureParam(slot.texture, texture),
			gfx.SamplerParam(slot.sampler, pbrSampler),
		)
	}
	return Material{{
		Tag: TagForward,
		Descr: gfx.MaterialWithState(
			gfx.ShaderWithResource(sceneShaderPath),
			pbrState(alphaOpaque, false),
			params...,
		),
	}}
}

// alphaMode is glTF's alphaMode, which selects fixed-function state and, for
// alphaMask, a shader discard.
type alphaMode uint8

const (
	alphaOpaque alphaMode = iota
	alphaMask
	alphaBlend
)

// pbrState maps glTF's alphaMode and doubleSided onto pipeline state.
//
// MASK is fixed-function-identical to OPAQUE — it writes depth and batches with
// the opaque geometry — because the cutoff is entirely a fragment-shader
// concern: the shader discards against alphaCutoff, which is zero for an opaque
// material and therefore a no-op there. It cannot be alpha-to-coverage, which
// needs MSAA.
func pbrState(alpha alphaMode, doubleSided bool) gfx.MaterialState {
	state := gfx.StateOpaque3D
	if alpha == alphaBlend {
		state = gfx.StateTransparent3D
	}
	if doubleSided {
		state.Cull = gfx.CullNone
	} else {
		state.Cull = gfx.CullBack
	}
	return state
}
