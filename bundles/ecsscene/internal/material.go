package internal

import (
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/slots/gfx"
)

// materialTag is one pass tag of a material as the frame draws it: the tag and
// the gfx material that serves it. material is a whole material, one entry per
// tag it serves; an empty one serves no pass and draws nowhere.
type (
	materialTag struct {
		tag   ecsscene.PassTag
		descr gfx.MaterialDescr
	}
	material []materialTag
)

// tagOf reads an empty tag as the forward one.
func tagOf(tag ecsscene.PassTag) ecsscene.PassTag {
	if tag == "" {
		return ecsscene.TagForward
	}
	return tag
}

// tagID is a pass tag interned to a dense index. Tags intern once per pass, not
// once per draw, so a draw never compares a string.
type tagID int32

// noEntry marks a tag this material does not serve. It is negative so that the
// per-Batch test is one array read and a sign test.
const noEntry int32 = -1

// materialEntry is one resolved (material, tag) pair: the gfx material to draw
// with, and the dense id the opaque sort key groups by.
type materialEntry struct {
	descr      *gfx.MaterialDescr
	materialID uint32
	// blend is the entry's sort class: true for anything that blends against
	// the target and so must draw back to front. Alpha-masked is opaque plus a
	// shader discard, and lands in the opaque class through its state alone.
	blend bool
}

// internedMaterial is one material resolved against every tag it serves.
// entries and ids are indexed by tagID.
type internedMaterial struct {
	material material
	entries  []int32
	ids      []uint32
}

// materialTable interns pass tags and the frame's materials. Tags live for the
// plugin's lifetime, because the tag set is a property of the passes an app
// declares rather than of a frame; materials are per frame.
//
// A material is interned by the material half of its Batch key, which the
// load System took on change, so a frame never fingerprints a material.
type materialTable struct {
	tags     map[ecsscene.PassTag]tagID
	tagNames []ecsscene.PassTag
	keys     map[uint64]int32
	interned []internedMaterial
	nextID   uint32
}

// reset starts a frame, interning the bundled PBR's four variants first and in
// variant order, so that a Batch with no material of its own resolves to its
// variant with no map probe at all.
func (t *materialTable) reset(bundled *[model.VariantCount]material) {
	if t.keys == nil {
		t.keys = map[uint64]int32{}
		t.tags = map[ecsscene.PassTag]tagID{}
	}
	clear(t.keys)
	for i := range t.interned {
		t.interned[i].material = nil
	}
	t.interned = t.interned[:0]
	t.nextID = 0
	for _, material := range bundled {
		t.add(discardReports{}, material)
	}
}

// internTag interns one pass tag. It is called once per pass, and an empty tag
// is the forward tag rather than a second name for it.
func (t *materialTable) internTag(tag ecsscene.PassTag) tagID {
	tag = tagOf(tag)
	if t.tags == nil {
		t.tags = map[ecsscene.PassTag]tagID{}
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
// whether it serves that pass at all.
func (t *materialTable) entry(interned int32, tag tagID) (materialEntry, bool) {
	material := &t.interned[interned]
	if int(tag) >= len(material.entries) {
		return materialEntry{}, false
	}
	index := material.entries[tag]
	if index < 0 {
		return materialEntry{}, false
	}
	descr := &material.material[index].descr
	return materialEntry{
		descr:      descr,
		materialID: material.ids[tag],
		blend:      descr.State().Blend != gfx.BlendOpaque,
	}, true
}

// lookup reports the frame-local index of the material a key names, if the
// frame has resolved it already. A Batch asks before resolving its material,
// so a key the frame has seen resolves nothing twice.
func (t *materialTable) lookup(key uint64) (int32, bool) {
	index, ok := t.keys[key]
	return index, ok
}

// intern interns one Batch's resolved material under its key and returns its
// frame-local index. It is called at most once per key per frame; a Batch with
// no material of its own takes the bundled PBR's variant, which is interned
// first and in variant order, and never calls it.
func (t *materialTable) intern(report errorReporter, material material, key uint64) int32 {
	index := t.add(report, material)
	t.keys[key] = index
	return index
}

// add resolves one material's tags into the dense entry and id arrays. Every
// tag the material names is interned here, so a tag first seen later in the
// frame is one this material does not serve.
func (t *materialTable) add(report errorReporter, material material) int32 {
	record := t.allocate()
	record.material = material
	for i := range material {
		t.internTag(material[i].tag)
	}
	record.entries = grow(record.entries, len(t.tagNames))
	record.ids = grow(record.ids, len(t.tagNames))
	for i := range record.entries {
		record.entries[i] = noEntry
	}
	for i := range material {
		tag := t.internTag(material[i].tag)
		if record.entries[tag] != noEntry {
			report.ReportError(ecsscene.ErrMaterialTagAlreadyServed{Tag: tagOf(material[i].tag)})
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

// discardReports is the report sink the bundled PBR interns through. It is
// model's own material, so a report from it would be an engine bug rather than
// something a caller can act on.
type discardReports struct{}

func (discardReports) ReportError(error) {}
