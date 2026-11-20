package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// batchKey is what a batch's entries share, a change of which from one
// entry to the next in sort order breaks a batch: their 2D material, the
// view of the texture their images are drawn from, the whole texture of
// the atlas they are packed into, or a render texture's view, their
// filter, the axes their sampler repeats along, and their clip. With them
// goes their material's stride, which the material decides, so it breaks
// no batch of its own: the stream their instance data goes into, zero for
// the fixed instances of a material taking no argument. The white texel's
// entries take no filter, FilterDefault, which no filter changes, so they
// join a batch of any filter.
type batchKey struct {
	material Material
	stride   uint32
	texture  render.TextureView
	filter   Filter
	wrap     Wrap
	clip     m.Rect
}

// batch is a run of a frame's entries in sort order that share a batch key,
// drawn as one instanced draw: its key, its filter the first of its
// entries' that is not FilterDefault, its entries, and the instances they
// draw, of the instance data of its material's stride.
type batch struct {
	key           batchKey
	entries       []sortedEntry
	firstInstance uint32
	instanceCount uint32
}

// appendBatches appends to batches the batches of entries, of state's, in
// sort order, a batch beginning at the first entry and wherever an entry's
// batch key, of batchKeys at its index, does not join the batch before it,
// of another material, texture or clip, or of a filter neither is
// FilterDefault, and to instances the instance data they draw, and answers
// batches appended to. Each batch's entries are part of entries. A fill, a
// line and a sprite draw one instance, and a stroke four, each followed,
// for a material taking an argument, by the argument its entry gives, or
// the zero value. A sprite's image is its frame of drawnImages, a
// renderer's, and a shape's the zero Image's.
func appendBatches(batches []batch, instances *listInstanceData, entries []sortedEntry, state *drawState, batchKeys []batchKey, drawnImages [][]drawnImage) []batch {
	firstBatch := len(batches)
	for index, entry := range entries {
		entryKey := batchKeys[entry.entryIndex]
		joins := false
		if len(batches) > firstBatch {
			lastKey := batches[len(batches)-1].key
			sharedFilter := lastKey.filter == entryKey.filter || lastKey.filter == FilterDefault || entryKey.filter == FilterDefault
			joins = lastKey.material == entryKey.material && lastKey.texture == entryKey.texture && lastKey.wrap == entryKey.wrap && lastKey.clip == entryKey.clip && sharedFilter
		}
		if !joins {
			batches = append(batches, batch{key: entryKey, entries: entries[index:index], firstInstance: instances.count(entryKey.stride)})
		}
		lastBatch := &batches[len(batches)-1]
		if lastBatch.key.filter == FilterDefault {
			lastBatch.key.filter = entryKey.filter
		}
		lastBatch.entries = lastBatch.entries[:len(lastBatch.entries)+1]
		recorded := state.entries[entry.entryIndex]
		entryStart := len(instances.fixed)
		if recorded.kind == EntryKindShape {
			instances.fixed = appendShapeInstances(instances.fixed, &state.shapes[recorded.index], &drawnImages[0][0])
		} else {
			sprite := &state.sprites[recorded.index]
			instances.fixed = appendSpriteInstance(instances.fixed, sprite, findDrawnFrame(drawnImages, sprite))
		}
		lastBatch.instanceCount += uint32(len(instances.fixed) - entryStart)
		if entryKey.stride != 0 {
			instances.moveToStream(entryStart, entryKey.stride, state.findEntryArgument(recorded))
		}
	}
	return batches
}
