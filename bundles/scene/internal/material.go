package internal

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal/types"
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
	material scene.Material
	entries  []int32
	ids      []uint32
}

// materialTable interns pass tags and caller materials. Tags live for the
// engine's lifetime, because the tag set is a property of the passes an app
// declares rather than of a frame; materials are per frame.
type materialTable struct {
	tags     map[scene.PassTag]tagID
	tagNames []scene.PassTag
	// keys and interned are the frame's materials. Both keep their backing
	// across frames, so a steady frame interns without allocating.
	keys     map[types.MaterialKey]int32
	interned []internedMaterial
	nextID   uint32
}

// reset starts a frame, interning the bundled PBR's four variants first and in
// variant order, so that a draw naming no material of its own resolves to its
// variant with no map probe at all.
func (t *materialTable) reset(bundled [model.VariantCount]scene.Material) {
	if t.keys == nil {
		t.keys = map[types.MaterialKey]int32{}
		t.tags = map[scene.PassTag]tagID{}
	}
	clear(t.keys)
	t.interned = t.interned[:0]
	t.nextID = 0
	for _, material := range bundled {
		t.add(discardBundledReports, material)
	}
}

// internTag interns one pass tag. It is called once per pass, and an empty tag
// is the forward tag rather than a second name for it.
func (t *materialTable) internTag(tag scene.PassTag) tagID {
	if tag == "" {
		tag = scene.TagForward
	}
	if t.tags == nil {
		t.tags = map[scene.PassTag]tagID{}
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
// key is the material's content key when the recording already took it, and
// zero when nothing did - a model's own materials - in which case it is taken
// here. A draw naming a material pays the fingerprint once either way.
func (t *materialTable) intern(
	report func(error), material scene.Material, key types.MaterialKey, variant model.ShaderVariant,
) int32 {
	if material == nil {
		return int32(variant)
	}
	if key == 0 {
		key = types.MaterialKeyOf(material)
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
func (t *materialTable) add(report func(error), material scene.Material) int32 {
	record := t.allocate()
	record.material = material
	for i := range material {
		t.internTag(types.MaterialTagOf(material[i]))
	}
	record.entries = grow(record.entries, len(t.tagNames))
	record.ids = grow(record.ids, len(t.tagNames))
	for i := range record.entries {
		record.entries[i] = noEntry
	}
	for i := range material {
		tag := t.internTag(types.MaterialTagOf(material[i]))
		if record.entries[tag] != noEntry {
			report(scene.ErrMaterialTagAlreadyServed{Tag: types.MaterialTagOf(material[i])})
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
