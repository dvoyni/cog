package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// batchKey is what a batch's entries share, a change of which from one
// entry to the next in sort order breaks a batch: their 2D material, the
// texture of the atlas their images are packed into, their filter, and
// their clip. The white texel's entries take no filter, FilterDefault,
// which no filter changes, so they join a batch of any filter.
type batchKey struct {
	material Material
	atlas    render.Texture
	filter   Filter
	clip     m.Rect
}

// batch is a run of a frame's entries in sort order that share a batch key,
// drawn as one instanced draw: its key, its filter the first of its
// entries' that is not FilterDefault, its entries, and the instances they
// draw.
type batch struct {
	key           batchKey
	entries       []sortedEntry
	firstInstance uint32
	instanceCount uint32
}

// appendBatches appends to batches the batches of entries, of state's, in
// sort order, a batch beginning at the first entry and wherever an entry's
// batch key, of batchKeys at its index, does not join the batch before it,
// of another material, atlas or clip, or of a filter neither is
// FilterDefault, and to instances the instance data they draw, and answers
// both appended to. Each batch's entries are part of entries. A fill, a
// line and a sprite draw one instance, and a stroke four. A sprite's image
// is drawnImages' at its index, and a shape's the zero Image's.
func appendBatches(batches []batch, instances []instanceData, entries []sortedEntry, state *drawState, batchKeys []batchKey, drawnImages []drawnImage) ([]batch, []instanceData) {
	firstBatch := len(batches)
	for index, entry := range entries {
		entryKey := batchKeys[entry.entryIndex]
		joins := false
		if len(batches) > firstBatch {
			lastKey := batches[len(batches)-1].key
			sharedFilter := lastKey.filter == entryKey.filter || lastKey.filter == FilterDefault || entryKey.filter == FilterDefault
			joins = lastKey.material == entryKey.material && lastKey.atlas == entryKey.atlas && lastKey.clip == entryKey.clip && sharedFilter
		}
		if !joins {
			batches = append(batches, batch{key: entryKey, entries: entries[index:index], firstInstance: uint32(len(instances))})
		}
		lastBatch := &batches[len(batches)-1]
		if lastBatch.key.filter == FilterDefault {
			lastBatch.key.filter = entryKey.filter
		}
		lastBatch.entries = lastBatch.entries[:len(lastBatch.entries)+1]
		if recorded := state.entries[entry.entryIndex]; recorded.kind == EntryKindShape {
			instances = appendShapeInstances(instances, &state.shapes[recorded.index], &drawnImages[0])
		} else {
			sprite := &state.sprites[recorded.index]
			instances = appendSpriteInstance(instances, sprite, &drawnImages[sprite.Image.index])
		}
		lastBatch.instanceCount = uint32(len(instances)) - lastBatch.firstInstance
	}
	return batches, instances
}
