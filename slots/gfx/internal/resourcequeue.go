package internal

// ResourceQueue records persistent GPU resource operations. Unlike OpQueue,
// it is not triple-buffered or latest-wins: operations remain queued until the
// render thread executes them.
type ResourceQueue struct {
	ids IDSource
	ops []Op
}

// NewResourceQueue builds an empty queue that reserves ids through ids.
func NewResourceQueue(ids IDSource) *ResourceQueue { return &ResourceQueue{ids: ids} }

// Ready reports whether the Backend adapter is ready, so resource IDs can be
// minted. A driver whose device arrives asynchronously is bound at composition
// and is not ready until the device exists.
func (q *ResourceQueue) Ready() bool { return q.ids != nil && q.ids().Ready() }

// BakeBuffer queues a durable bake and returns its baked buffer descriptor.
// copyData snapshots bytes when true; when false, the caller must keep them
// unchanged until the resource queue is consumed by the render thread.
func (q *ResourceQueue) BakeBuffer(data []byte, copyData bool) BufferDescr {
	return q.bakeBuffer(q.ids().NewBuffer(), BufferStorage, len(data), data, copyData)
}

// ReBakeBuffer queues a durable rebake while preserving the buffer descriptor.
// copyData snapshots bytes when true; when false, the caller must keep them
// unchanged until consumed.
func (q *ResourceQueue) ReBakeBuffer(buffer BufferDescr, data []byte, copyData bool) BufferDescr {
	return q.bakeBuffer(buffer.id, BufferStorage, len(data), data, copyData)
}

func (q *ResourceQueue) bakeBuffer(id BufferID, kind BufferKind, size int, data []byte, copyData bool) BufferDescr {
	if copyData {
		data = append([]byte(nil), data...)
	}
	q.ops = append(q.ops, Op{
		Kind: OpBakeBuffer, BufferID: id, BufferKind: kind, BufferSize: size,
		Bytes: data,
	})
	return BakedBuffer(id, len(data))
}

// ReleaseBuffer queues a durable release for buffer.
func (q *ResourceQueue) ReleaseBuffer(buffer BufferDescr) {
	q.ops = append(q.ops, Op{Kind: OpReleaseBuffer, BufferID: buffer.id})
}

// BakeTexture queues a durable bake and returns its baked texture descriptor.
// copyData snapshots pixels when true; when false, the caller must keep them
// unchanged until the resource queue is consumed by the render thread. mipmaps
// generates a full mip chain at bake time.
func (q *ResourceQueue) BakeTexture(width, height int, format TextureFormat, pixels []byte, copyData, mipmaps bool) TextureDescr {
	return q.bakeTexture(q.ids().NewTexture(), width, height, format, pixels, copyData, mipmaps)
}

// AllocateTexture queues allocation of an empty texture to sample from. More
// than one layer creates a 2D-array texture. No pass can render into it: ask
// AllocateRenderTarget for that.
func (q *ResourceQueue) AllocateTexture(width, height, layers int, format TextureFormat) TextureDescr {
	return q.allocateTexture(width, height, layers, format, false)
}

// AllocateRenderTarget queues allocation of an empty texture a pass can render
// into, through TextureTarget, and sample afterwards. More than one layer
// creates a 2D-array texture, and TextureTarget names which layer a pass writes.
//
// It is a separate method rather than a flag on AllocateTexture because the
// render-attachment usage is not free: a backend may keep a sampled-only
// texture in a compressed layout it cannot render into, so a texture that says
// it might be a target pays for the possibility on every frame it is only read.
// Almost every texture in a frame is sampled-only, and the default belongs to
// the cheap case.
//
// This is the durable counterpart of OpQueue.TemporaryTarget. Take it when the
// rendered contents must outlive the frame - a canvas layer baked once and
// sampled by a scene material for many frames after, a cached UI panel, a
// shadow map held across frames. When they need only live until the frame ends,
// TemporaryTarget pools its textures and this one does not: what this returns is
// caller-owned and must be released.
func (q *ResourceQueue) AllocateRenderTarget(width, height, layers int, format TextureFormat) TextureDescr {
	return q.allocateTexture(width, height, layers, format, true)
}

func (q *ResourceQueue) allocateTexture(width, height, layers int, format TextureFormat, renderable bool) TextureDescr {
	id := q.ids().NewTexture()
	q.ops = append(q.ops, Op{
		Kind: OpAllocateTexture, TextureID: id,
		TexW: width, TexH: height, TexLayers: layers, Format: format,
		Renderable: renderable,
	})
	return TextureDescr{Params: TextureDescrParams{
		id: id, width: width, height: height, layers: layers, format: format,
	}}
}

// UpdateTexture queues a pixel upload into one texture layer and region.
func (q *ResourceQueue) UpdateTexture(texture TextureDescr, layer int, region Region, pixels []byte, copyData bool) {
	if copyData {
		pixels = append([]byte(nil), pixels...)
	}
	q.ops = append(q.ops, Op{
		Kind: OpUpdateTexture, TextureID: texture.Params.id,
		TexLayer: layer, Region: region, Bytes: pixels,
	})
}

// ReBakeTexture queues a durable rebake while preserving the texture descriptor.
// copyData snapshots pixels when true; when false, the caller must keep them
// unchanged until consumed. mipmaps generates a full mip chain at bake time.
func (q *ResourceQueue) ReBakeTexture(texture TextureDescr, width, height int, format TextureFormat, pixels []byte, copyData, mipmaps bool) TextureDescr {
	return q.bakeTexture(texture.Params.id, width, height, format, pixels, copyData, mipmaps)
}

func (q *ResourceQueue) bakeTexture(id TextureID, width, height int, format TextureFormat, pixels []byte, copyData, mipmaps bool) TextureDescr {
	if copyData {
		pixels = append([]byte(nil), pixels...)
	}
	q.ops = append(q.ops, Op{
		Kind: OpBakeTexture, TextureID: id, TexW: width, TexH: height,
		Format: format, Mipmaps: mipmaps, Bytes: pixels,
	})
	// A bake is one layer by construction: it takes a single pixel run and no op
	// gives it more. Saying so keeps every path-loaded texture answerable, which
	// is the case a draw against a texture_2d_array binding actually meets.
	return TextureDescr{Params: TextureDescrParams{id: id, width: width, height: height, layers: 1}}
}

// ReleaseTexture queues a durable release for texture.
func (q *ResourceQueue) ReleaseTexture(texture TextureDescr) {
	q.ops = append(q.ops, Op{Kind: OpReleaseTexture, TextureID: texture.Params.id})
}

func (q *ResourceQueue) releaseCachedResource(path string) {
	if path == "" {
		return
	}
	q.ops = append(q.ops, Op{Kind: OpReleaseCachedResource, Path: path})
}

func (q *ResourceQueue) freeCachedResources() {
	q.ops = append(q.ops, Op{Kind: OpFreeCachedResources})
}

func (q *ResourceQueue) reset() {
	clear(q.ops)
	q.ops = q.ops[:0]
}
