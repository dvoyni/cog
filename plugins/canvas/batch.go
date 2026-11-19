package canvas

import "github.com/dvoyni/cog/libs/m"

// batchKey is what a batch's entries share, a change of which from one
// entry to the next in sort order breaks a batch: their 2D material, their
// image, drawn from a texture of its own, the white texel's for shapes,
// their filter, FilterDefault for the white texel, which no filter changes,
// and their clip.
type batchKey struct {
	material Material
	image    Image
	filter   Filter
	clip     m.Rect
}

// batch is a run of a frame's entries in sort order that share a batch key,
// drawn as one instanced draw: its entries, and the instances they draw.
type batch struct {
	key           batchKey
	entries       []sortedEntry
	firstInstance uint32
	instanceCount uint32
}

// appendBatches appends to batches the batches of entries, of state's, in
// sort order, a batch beginning at the first entry and wherever an entry's
// batch key differs from the one before it, and to instances the instance
// data they draw, and answers both appended to. Each batch's entries are
// part of entries. A fill, a line and a sprite draw one instance, and a
// stroke four. A sprite's image is drawnImages' at its index.
func appendBatches(batches []batch, instances []instanceData, entries []sortedEntry, state *drawState, drawnImages []drawnImage) ([]batch, []instanceData) {
	firstBatch := len(batches)
	for index, entry := range entries {
		entryBatchKey := state.batchKeys[entry.entryIndex]
		if len(batches) == firstBatch || batches[len(batches)-1].key != entryBatchKey {
			batches = append(batches, batch{key: entryBatchKey, entries: entries[index:index], firstInstance: uint32(len(instances))})
		}
		lastBatch := &batches[len(batches)-1]
		lastBatch.entries = lastBatch.entries[:len(lastBatch.entries)+1]
		if recorded := state.entries[entry.entryIndex]; recorded.kind == EntryKindShape {
			instances = appendShapeInstances(instances, &state.shapes[recorded.index])
		} else {
			sprite := &state.sprites[recorded.index]
			image := &drawnImages[sprite.Image.index]
			instances = appendSpriteInstance(instances, sprite, image.size, image.pivot)
		}
		lastBatch.instanceCount = uint32(len(instances)) - lastBatch.firstInstance
	}
	return batches, instances
}
