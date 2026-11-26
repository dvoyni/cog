package canvas

import (
	"cmp"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// batchKey is what a batch's entries share, a change of which from one
// entry to the next in sort order breaks a batch: their 2D material, the
// view of the texture their images are drawn from, the whole texture of
// the atlas they are packed into, or a render texture's view, their
// filter, the axes their sampler repeats along, their clip, the per-frame
// mesh they are, one more than its index among the frame's triangles, zero
// for any other entry, so each draws alone, and the mesh file they draw,
// the zero MeshFile for an entry of no file, so meshes of one file batch
// together and with nothing else. With them
// goes their material's stride, which the material decides, so it breaks
// no batch of its own: the stream their instance data goes into, zero for
// the fixed instances of a material taking no argument. The white texel's
// entries take no filter, FilterDefault, which no filter changes, so they
// join a batch of any filter.
type batchKey struct {
	material  Material
	stride    uint32
	texture   render.TextureView
	filter    Filter
	wrap      Wrap
	clip      m.Rect
	triangles uint32
	meshFile  MeshFile
}

// makeImageBatchKey answers what an entry drawing image, as drawn image
// drawn, with material, clipped to clip, shares with the entries it batches
// with: its sampler repeating along the axes the image wraps along where no
// gutter does, its filter defaultFilter when it names none, and none for
// the zero Image, the white texel, which no filter changes, so it batches
// with shapes.
func makeImageBatchKey(material Material, image Image, drawn *drawnImage, clip m.Rect, filter, defaultFilter Filter) batchKey {
	key := batchKey{material: material, texture: drawn.findView(), clip: clip}
	if drawn.wrapsBySampler() {
		key.wrap = drawn.options.Wrap
	}
	if image != (Image{}) {
		key.filter = cmp.Or(filter, defaultFilter)
	}
	return key
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

// appendBatches appends to batches the batches of entries, of list's, in
// sort order, a batch beginning at the first entry and wherever an entry's
// batch key, its drawn entry's of drawn at its index, does not join the
// batch before it, of another material, texture, clip or mesh file, of a
// filter neither is FilterDefault, or either a per-frame mesh, and to
// instances the instance data they draw, as list appends each entry's, its
// images and glyphs as atlases placed them, and answers batches appended
// to. Each batch's entries are part of entries.
func appendBatches(batches []batch, instances *listInstanceData, entries []sortedEntry, list *commandList, drawn []drawnEntry, atlases *atlasSet) []batch {
	firstBatch := len(batches)
	for index, entry := range entries {
		entryKey := drawn[entry.entryIndex].key
		joins := false
		if len(batches) > firstBatch {
			lastKey := batches[len(batches)-1].key
			sharedFilter := lastKey.filter == entryKey.filter || lastKey.filter == FilterDefault || entryKey.filter == FilterDefault
			// Every other field of the keys must match, so a field added to
			// the key breaks batches without a change here.
			lastKey.filter = entryKey.filter
			joins = sharedFilter && lastKey == entryKey
		}
		if !joins {
			batches = append(batches, batch{key: entryKey, entries: entries[index:index], firstInstance: instances.count(entryKey.stride)})
		}
		lastBatch := &batches[len(batches)-1]
		if lastBatch.key.filter == FilterDefault {
			lastBatch.key.filter = entryKey.filter
		}
		lastBatch.entries = lastBatch.entries[:len(lastBatch.entries)+1]
		lastBatch.instanceCount += list.appendInstances(instances, entry.entryIndex, entryKey.stride, atlases)
	}
	return batches
}
