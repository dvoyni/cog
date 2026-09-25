package internal

import (
	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

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
func (q *ResourceQueue) BakeBuffer(data []byte, copyData bool) descriptors.BufferDescr {
	return q.bakeBuffer(q.ids().NewBuffer(), types.BufferStorage, len(data), data, copyData)
}

// ReBakeBuffer queues a durable rebake while preserving the buffer descriptor.
// copyData snapshots bytes when true; when false, the caller must keep them
// unchanged until consumed.
func (q *ResourceQueue) ReBakeBuffer(buffer descriptors.BufferDescr, data []byte, copyData bool) descriptors.BufferDescr {
	return q.bakeBuffer(buffer.ID(), types.BufferStorage, len(data), data, copyData)
}

func (q *ResourceQueue) bakeBuffer(id types.BufferID, kind types.BufferKind, size int, data []byte, copyData bool) descriptors.BufferDescr {
	if copyData {
		data = append([]byte(nil), data...)
	}
	q.ops = append(q.ops, Op{
		Kind: OpBakeBuffer, BufferID: id, BufferKind: kind, BufferSize: size,
		Bytes: data,
	})
	return descriptors.BakedBuffer(id, len(data))
}

// ReleaseBuffer queues a durable release for buffer.
func (q *ResourceQueue) ReleaseBuffer(buffer descriptors.BufferDescr) {
	q.ops = append(q.ops, Op{Kind: OpReleaseBuffer, BufferID: buffer.ID()})
}

// BakeTexture queues a durable bake and returns its baked texture descriptor.
// copyData snapshots pixels when true; when false, the caller must keep them
// unchanged until the resource queue is consumed by the render thread. mipmaps
// generates a full mip chain at bake time.
func (q *ResourceQueue) BakeTexture(width, height int, format descriptors.TextureFormat, pixels []byte, copyData, mipmaps bool) descriptors.TextureDescr {
	return q.bakeTexture(q.ids().NewTexture(), width, height, format, pixels, copyData, mipmaps)
}

// AllocateTexture queues allocation of an empty texture to sample from. More
// than one layer creates a 2D-array texture. No pass can render into it: ask
// AllocateRenderTarget for that.
func (q *ResourceQueue) AllocateTexture(width, height, layers int, format descriptors.TextureFormat) descriptors.TextureDescr {
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
func (q *ResourceQueue) AllocateRenderTarget(width, height, layers int, format descriptors.TextureFormat) descriptors.TextureDescr {
	return q.allocateTexture(width, height, layers, format, true)
}

func (q *ResourceQueue) allocateTexture(width, height, layers int, format descriptors.TextureFormat, renderable bool) descriptors.TextureDescr {
	id := q.ids().NewTexture()
	q.ops = append(q.ops, Op{
		Kind: OpAllocateTexture, TextureID: id,
		TexW: width, TexH: height, TexLayers: layers, Format: format,
		Renderable: renderable,
	})
	return descriptors.BakedTextureWith(id, width, height, layers, format)
}

// UpdateTexture queues a pixel upload into one texture layer and region.
func (q *ResourceQueue) UpdateTexture(texture descriptors.TextureDescr, layer int, region types.Region, pixels []byte, copyData bool) {
	if copyData {
		pixels = append([]byte(nil), pixels...)
	}
	q.ops = append(q.ops, Op{
		Kind: OpUpdateTexture, TextureID: texture.ID(),
		TexLayer: layer, Region: region, Bytes: pixels,
	})
}

// ReBakeTexture queues a durable rebake while preserving the texture descriptor.
// copyData snapshots pixels when true; when false, the caller must keep them
// unchanged until consumed. mipmaps generates a full mip chain at bake time.
func (q *ResourceQueue) ReBakeTexture(texture descriptors.TextureDescr, width, height int, format descriptors.TextureFormat, pixels []byte, copyData, mipmaps bool) descriptors.TextureDescr {
	return q.bakeTexture(texture.ID(), width, height, format, pixels, copyData, mipmaps)
}

func (q *ResourceQueue) bakeTexture(id types.TextureID, width, height int, format descriptors.TextureFormat, pixels []byte, copyData, mipmaps bool) descriptors.TextureDescr {
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
	return descriptors.BakedTextureWith(id, width, height, 1, 0)
}

// ReleaseTexture queues a durable release for texture.
func (q *ResourceQueue) ReleaseTexture(texture descriptors.TextureDescr) {
	q.ops = append(q.ops, Op{Kind: OpReleaseTexture, TextureID: texture.ID()})
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
