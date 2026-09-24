package internal

import (
	"encoding/binary"
	"hash/maphash"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/slots/gfx"
)

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
	keys     map[MaterialKey]int32
	interned []internedMaterial
	nextID   uint32
	// queue is gfx's queue the frame is recorded into. Every material the
	// frame interns is recorded for it once, so every draw of the material
	// names the queue's copy of its params rather than gfx copying them per
	// draw.
	queue *gfx.OpQueue
	// bundledRecorded is which bundled variants this frame has recorded. A
	// variant is recorded the first time a draw resolves to it rather than at
	// reset, because recording bakes its ten textures and a frame drawing no
	// bundled PBR would pay that for nothing.
	bundledRecorded [model.VariantCount]bool
}

// reset starts a frame, interning the bundled PBR's four variants first and in
// variant order, so that a draw naming no material of its own resolves to its
// variant with no map probe at all.
func (t *materialTable) reset(bundled [model.VariantCount]Material, queue *gfx.OpQueue) {
	t.queue = queue
	if t.keys == nil {
		t.keys = map[MaterialKey]int32{}
		t.tags = map[PassTag]tagID{}
	}
	clear(t.keys)
	t.interned = t.interned[:0]
	t.nextID = 0
	t.bundledRecorded = [model.VariantCount]bool{}
	for _, material := range bundled {
		t.addUnrecorded(discardBundledReports, material)
	}
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
//
// key is the material's content key when something already took it, and zero
// when nothing did, in which case it is taken here. A draw naming a material
// was keyed as it recorded, and a model's own material arrives keyed from the
// fingerprint its load took, so the fallback is left to what neither keyed.
func (t *materialTable) intern(
	report func(error), material Material, key MaterialKey, variant model.ShaderVariant,
) int32 {
	if material == nil {
		if !t.bundledRecorded[variant] {
			t.record(t.interned[variant].material)
			t.bundledRecorded[variant] = true
		}
		return int32(variant)
	}
	if key == 0 {
		key = MaterialKeyOf(material)
	}
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
	t.record(material)
	return t.addUnrecorded(report, material)
}

// record records every tag of one material for the frame's queue, in place.
// The material is the frame's own copy - a recorded draw's, or a model
// material wrapped in the frame's arena - so recording it in place changes
// nothing a caller holds, and the table's entries, which share its backing,
// name the recorded descriptors. A table with no queue - a unit test's -
// records nothing.
func (t *materialTable) record(material Material) {
	for i := 0; t.queue != nil && i < len(material); i++ {
		material[i].Descr = t.queue.FrameMaterial(material[i].Descr)
	}
}

// addUnrecorded is add for a material recorded later or never: the bundled
// variants, which are recorded on first use.
func (t *materialTable) addUnrecorded(report func(error), material Material) int32 {
	record := t.allocate()
	record.material = material
	for i := range material {
		t.internTag(MaterialTagOf(material[i]))
	}
	record.entries = grow(record.entries, len(t.tagNames))
	record.ids = grow(record.ids, len(t.tagNames))
	for i := range record.entries {
		record.entries[i] = noEntry
	}
	for i := range material {
		tag := t.internTag(MaterialTagOf(material[i]))
		if record.entries[tag] != noEntry {
			report(ErrMaterialTagAlreadyServed{Tag: MaterialTagOf(material[i])})
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

// MaterialKey identifies one Material by content: a fingerprint of each entry's
// tag and gfx material - shader source-or-path, pipeline state and parameter
// bytes. A caller-supplied gfx.MaterialDescr has no id of its own, and keying
// by the slice's backing instead would hand the spec's own idiom,
// Material{{Descr: descr}} built inline per draw, a fresh id every draw and so
// never sort two of them together.
//
// Zero is never a key. It is what a draw record carries when nothing keyed its
// material at record, which tells the flush to key it there.
type MaterialKey uint64

var materialSeed = maphash.MakeSeed()

// MaterialKeyOf keys material by content. A fingerprint that lands on zero is
// read as one, which folds it into the same one-in-2^64 collision class every
// other key already risks.
func MaterialKeyOf(material Material) MaterialKey {
	var h maphash.Hash
	h.SetSeed(materialSeed)
	for i := range material {
		writeMaterialEntry(&h, material[i].tag(), material[i].Descr.Fingerprint())
	}
	return materialKeyOfSum(h.Sum64())
}

// ForwardMaterialKey is MaterialKeyOf of the one-entry forward material
// Material{{Tag: TagForward, Descr: descr}}, given descr's gfx fingerprint
// rather than descr. A model material carries that fingerprint from its load,
// so a draw of a file's own material is keyed without fingerprinting anything:
// what is left is hashing the tag and eight bytes. The key is the one
// MaterialKeyOf would give, so the file's material batches exactly as it did
// when the flush keyed it.
func ForwardMaterialKey(fingerprint uint64) MaterialKey {
	var h maphash.Hash
	h.SetSeed(materialSeed)
	writeMaterialEntry(&h, TagForward, fingerprint)
	return materialKeyOfSum(h.Sum64())
}

// writeMaterialEntry hashes one entry of a material: its tag, then its gfx
// fingerprint.
func writeMaterialEntry(h *maphash.Hash, tag PassTag, fingerprint uint64) {
	h.WriteString(string(tag))
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], fingerprint)
	h.Write(buf[:])
}

// materialKeyOfSum reads a sum that lands on zero as one: zero is never a key.
func materialKeyOfSum(sum uint64) MaterialKey {
	if key := MaterialKey(sum); key != 0 {
		return key
	}
	return 1
}
