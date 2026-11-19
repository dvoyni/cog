package canvas

import "github.com/dvoyni/cog/libs/m"

// batchKey is what a batch's entries share, a change of which from one
// entry to the next in sort order breaks a batch: their 2D material and
// their clip. Their atlas and filter join it as entries gain them.
type batchKey struct {
	material Material
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

// appendBatches appends to batches the batches of entries, of shapes, in
// sort order, a batch beginning at the first entry and wherever an entry's
// batch key differs from the one before it, and to instances the instance
// data they draw, and answers both appended to. Each batch's entries are
// part of entries. A fill and a line draw one instance, and a stroke four.
func appendBatches(batches []batch, instances []instanceData, entries []sortedEntry, shapes []Shape) ([]batch, []instanceData) {
	firstBatch := len(batches)
	for index, entry := range entries {
		shape := &shapes[entry.shapeIndex]
		entryBatchKey := shape.batchKey()
		if len(batches) == firstBatch || batches[len(batches)-1].key != entryBatchKey {
			batches = append(batches, batch{key: entryBatchKey, entries: entries[index:index], firstInstance: uint32(len(instances))})
		}
		lastBatch := &batches[len(batches)-1]
		lastBatch.entries = lastBatch.entries[:len(lastBatch.entries)+1]
		instances = appendShapeInstances(instances, shape)
		lastBatch.instanceCount = uint32(len(instances)) - lastBatch.firstInstance
	}
	return batches, instances
}
