package internal

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/slots/gfx"
)

// materialTag is one pass tag of a material as the frame draws it: the tag,
// the set that serves it and what the frame needs to version that set - its
// state, which of scene's per-Batch bindings its shader declares, the program
// a Params binding is checked against, and the material block's values a
// Params member is laid over. material is a whole material, one entry per tag
// it serves; an empty one serves no pass and draws nowhere.
type (
	materialTag struct {
		tag      PassTag
		set      gfx.DrawParams
		state    gfx.MaterialState
		bindings sceneBindings
		program  gfx.ShaderProgram
		values   model.PbrValues
	}
	material []materialTag
)

// tagOf reads an empty tag as the forward one.
func tagOf(tag PassTag) PassTag {
	if tag == "" {
		return TagForward
	}
	return tag
}

// tagID is a pass tag interned to a dense index. Tags intern once per pass, not
// once per draw, so a draw never compares a string.
type tagID int32

// noEntry marks a tag this material does not serve. It is negative so that the
// per-Batch test is one array read and a sign test.
const noEntry int32 = -1

// materialEntry is one resolved (material, tag) pair: the tag to draw with,
// and the dense id the opaque sort key groups by.
type materialEntry struct {
	tag        *materialTag
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
// A material is interned by the set key of its Batch, which the load System
// took on change, so a frame never fingerprints a material.
type materialTable struct {
	tags     map[PassTag]tagID
	tagNames []PassTag
	keys     map[setKey]int32
	interned []internedMaterial
	nextID   uint32
}

// reset starts a frame.
func (t *materialTable) reset() {
	if t.keys == nil {
		t.keys = map[setKey]int32{}
		t.tags = map[PassTag]tagID{}
	}
	clear(t.keys)
	for i := range t.interned {
		t.interned[i].material = nil
	}
	t.interned = t.interned[:0]
	t.nextID = 0
}

// internTag interns one pass tag. It is called once per pass, and an empty tag
// is the forward tag rather than a second name for it.
func (t *materialTable) internTag(tag PassTag) tagID {
	tag = tagOf(tag)
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

// entry reports the tag one interned material draws with in one pass, and
// whether it serves that pass at all. A tag whose set is zero - its shader did
// not compile, or model had none to build it on - serves nothing.
func (t *materialTable) entry(interned int32, tag tagID) (materialEntry, bool) {
	material := &t.interned[interned]
	if int(tag) >= len(material.entries) {
		return materialEntry{}, false
	}
	index := material.entries[tag]
	if index < 0 {
		return materialEntry{}, false
	}
	resolved := &material.material[index]
	if resolved.set == (gfx.DrawParams{}) {
		return materialEntry{}, false
	}
	return materialEntry{
		tag:        resolved,
		materialID: material.ids[tag],
		blend:      resolved.state.Blend != gfx.BlendOpaque,
	}, true
}

// lookup reports the frame-local index of the material a key names, if the
// frame has resolved it already. A Batch asks before resolving its material,
// so a key the frame has seen resolves nothing twice.
func (t *materialTable) lookup(key setKey) (int32, bool) {
	index, ok := t.keys[key]
	return index, ok
}

// intern interns one Batch's resolved material under its key and returns its
// frame-local index. It is called at most once per key per frame.
func (t *materialTable) intern(report errorReporter, material material, key setKey) int32 {
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
			report.ReportError(ErrMaterialTagAlreadyServed{Tag: tagOf(material[i].tag)})
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
