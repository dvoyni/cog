package gfx

// resourceQueue records persistent GPU resource operations. Unlike OpQueue,
// it is not triple-buffered or latest-wins: operations remain queued until the
// render thread executes them.
type resourceQueue struct {
	backend Backend
	ops     []op
}

// Ready reports whether a backend is installed and resource IDs can be minted.
func (q *resourceQueue) Ready() bool { return q.backend != nil }

// BakeBuffer queues a durable bake and returns its baked buffer descriptor.
// copyData snapshots bytes when true; when false, the caller must keep them
// unchanged until the resource queue is consumed by the render thread.
func (q *resourceQueue) BakeBuffer(data []byte, copyData bool) BufferDescr {
	return q.bakeBuffer(q.backend.NewBuffer(), BufferStorage, len(data), data, copyData)
}

// ReBakeBuffer queues a durable rebake while preserving the buffer descriptor.
// copyData snapshots bytes when true; when false, the caller must keep them
// unchanged until consumed.
func (q *resourceQueue) ReBakeBuffer(buffer BufferDescr, data []byte, copyData bool) BufferDescr {
	return q.bakeBuffer(buffer.id, BufferStorage, len(data), data, copyData)
}

func (q *resourceQueue) bakeBuffer(id BufferID, kind BufferKind, size int, data []byte, copyData bool) BufferDescr {
	if copyData {
		data = append([]byte(nil), data...)
	}
	q.ops = append(q.ops, op{
		kind: opBakeBuffer, bufferID: id, bufferKind: kind, bufferSize: size,
		bytes: data,
	})
	return BufferDescr{source: BufferSourceBaked, id: id, size: len(data)}
}

// ReleaseBuffer queues a durable release for buffer.
func (q *resourceQueue) ReleaseBuffer(buffer BufferDescr) {
	q.ops = append(q.ops, op{kind: opReleaseBuffer, bufferID: buffer.id})
}

// BakeTexture queues a durable bake and returns its baked texture descriptor.
// copyData snapshots pixels when true; when false, the caller must keep them
// unchanged until the resource queue is consumed by the render thread. mipmaps
// generates a full mip chain at bake time.
func (q *resourceQueue) BakeTexture(width, height int, format TextureFormat, pixels []byte, copyData, mipmaps bool) TextureDescr {
	return q.bakeTexture(q.backend.NewTexture(), width, height, format, pixels, copyData, mipmaps)
}

// AllocateTexture queues allocation of an empty texture. More than one layer
// creates a 2D-array texture.
func (q *resourceQueue) AllocateTexture(width, height, layers int, format TextureFormat) TextureDescr {
	id := q.backend.NewTexture()
	q.ops = append(q.ops, op{
		kind: opAllocateTexture, textureID: id,
		texW: width, texH: height, texLayers: layers, format: format,
	})
	return TextureDescr{source: TextureSourceBaked, id: id}
}

// UpdateTexture queues a pixel upload into one texture layer and region.
func (q *resourceQueue) UpdateTexture(texture TextureDescr, layer int, region Region, pixels []byte, copyData bool) {
	if copyData {
		pixels = append([]byte(nil), pixels...)
	}
	q.ops = append(q.ops, op{
		kind: opUpdateTexture, textureID: texture.id,
		texLayer: layer, region: region, bytes: pixels,
	})
}

// ReBakeTexture queues a durable rebake while preserving the texture descriptor.
// copyData snapshots pixels when true; when false, the caller must keep them
// unchanged until consumed. mipmaps generates a full mip chain at bake time.
func (q *resourceQueue) ReBakeTexture(texture TextureDescr, width, height int, format TextureFormat, pixels []byte, copyData, mipmaps bool) TextureDescr {
	return q.bakeTexture(texture.id, width, height, format, pixels, copyData, mipmaps)
}

func (q *resourceQueue) bakeTexture(id TextureID, width, height int, format TextureFormat, pixels []byte, copyData, mipmaps bool) TextureDescr {
	if copyData {
		pixels = append([]byte(nil), pixels...)
	}
	q.ops = append(q.ops, op{
		kind: opBakeTexture, textureID: id, texW: width, texH: height,
		format: format, mipmaps: mipmaps, bytes: pixels,
	})
	return TextureDescr{source: TextureSourceBaked, id: id}
}

// ReleaseTexture queues a durable release for texture.
func (q *resourceQueue) ReleaseTexture(texture TextureDescr) {
	q.ops = append(q.ops, op{kind: opReleaseTexture, textureID: texture.id})
}

func (q *resourceQueue) releaseCachedResource(path string) {
	if path == "" {
		return
	}
	q.ops = append(q.ops, op{kind: opReleaseCachedResource, path: path})
}

func (q *resourceQueue) freeCachedResources() {
	q.ops = append(q.ops, op{kind: opFreeCachedResources})
}

func (q *resourceQueue) reset() {
	clear(q.ops)
	q.ops = q.ops[:0]
}
