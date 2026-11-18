package canvas

// batchKey is what a batch's entries share, a change of which from one
// entry to the next in sort order breaks a batch: their 2D material. Their
// atlas, filter and clip join it as entries gain them.
type batchKey struct {
	material Material
}

// batch is a run of a frame's entries in sort order that share a batch key,
// drawn as one instanced draw: its entries, and the instances they draw.
type batch struct {
	key           batchKey
	entries       []sortedEntry
	firstInstance uint32
	instanceCount uint32
}

// makeBatches answers the batches of entries, the frame's in sort order, of
// shapes, a batch beginning wherever an entry's batch key differs from the
// one before it, and the instance data they draw, in the storage of batches
// and instances. Each batch's entries are part of entries. A fill and a
// line draw one instance, and a stroke four.
func makeBatches(batches []batch, instances []instanceData, entries []sortedEntry, shapes []Shape) ([]batch, []instanceData) {
	batches = batches[:0]
	instances = instances[:0]
	for index, entry := range entries {
		shape := &shapes[entry.shapeIndex]
		entryBatchKey := shape.batchKey()
		if len(batches) == 0 || batches[len(batches)-1].key != entryBatchKey {
			batches = append(batches, batch{key: entryBatchKey, entries: entries[index:index], firstInstance: uint32(len(instances))})
		}
		lastBatch := &batches[len(batches)-1]
		lastBatch.entries = lastBatch.entries[:len(lastBatch.entries)+1]
		instances = appendShapeInstances(instances, shape)
		lastBatch.instanceCount = uint32(len(instances)) - lastBatch.firstInstance
	}
	return batches, instances
}
