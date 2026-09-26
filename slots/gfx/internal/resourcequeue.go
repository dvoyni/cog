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
	ops []ResourceOp
}

// NewResourceQueue builds an empty queue that reserves ids through ids.
func NewResourceQueue(ids IDSource) *ResourceQueue { return &ResourceQueue{ids: ids} }

// Ready reports whether the Backend adapter is ready, so resource IDs can be
// minted. A driver whose device arrives asynchronously is bound at composition
// and is not ready until the device exists.
func (q *ResourceQueue) Ready() bool { return q.ids != nil && q.ids().Ready() }

// NewBuffer reserves a buffer and returns its descriptor. Nothing reaches the
// GPU until UploadBuffer gives it contents, which also fixes its size.
func (q *ResourceQueue) NewBuffer() descriptors.BufferDescr {
	return descriptors.BakedBuffer(q.ids().NewBuffer(), 0)
}

// NewTexture queues allocation of an empty texture to sample from. More than
// one layer creates a 2D-array texture. mipmaps gives it a full mip chain,
// which UploadTexture rebuilds for every upload that covers a whole layer. No
// pass can render into it: ask NewRenderTarget for that.
func (q *ResourceQueue) NewTexture(width, height, layers int, format descriptors.TextureFormat, mipmaps bool) descriptors.TextureDescr {
	return q.newTexture(width, height, layers, format, mipmaps, false)
}

// NewRenderTarget queues allocation of an empty texture a pass can render
// into, through TextureTarget, and sample afterwards. More than one layer
// creates a 2D-array texture, and TextureTarget names which layer a pass writes.
//
// It is a separate method rather than a flag on NewTexture because the
// render-attachment usage is not free: a backend may keep a sampled-only
// texture in a compressed layout it cannot render into, so a texture that says
// it might be a target pays for the possibility on every frame it is only read.
// Almost every texture in a frame is sampled-only, and the default belongs to
// the cheap case.
//
// This is the durable counterpart of OpQueue.NewTemporaryTarget. Take it when the
// rendered contents must outlive the frame - a canvas layer baked once and
// sampled by a scene material for many frames after, a cached UI panel, a
// shadow map held across frames. When they need only live until the frame ends,
// NewTemporaryTarget pools its textures and this one does not: what this returns is
// caller-owned and must be released.
func (q *ResourceQueue) NewRenderTarget(width, height, layers int, format descriptors.TextureFormat) descriptors.TextureDescr {
	return q.newTexture(width, height, layers, format, false, true)
}

func (q *ResourceQueue) newTexture(width, height, layers int, format descriptors.TextureFormat, mipmaps, renderable bool) descriptors.TextureDescr {
	id := q.ids().NewTexture()
	q.ops = append(q.ops, ResourceOp{
		Kind: OpAllocateTexture, TextureID: id,
		TexW: width, TexH: height, TexLayers: layers, Format: format,
		Mipmaps: mipmaps, Renderable: renderable,
	})
	return descriptors.BakedTextureWith(id, width, height, layers, format)
}

// UploadBuffer queues data as buffer's whole contents and returns the
// descriptor with its new size. Uploading again replaces the contents at any
// length and keeps the buffer's id, so descriptors already handed out stay
// valid. copyData snapshots data when true; when false, the caller must keep it
// unchanged until the resource queue is consumed by the render thread.
func (q *ResourceQueue) UploadBuffer(buffer descriptors.BufferDescr, data []byte, copyData bool) descriptors.BufferDescr {
	id := buffer.ID()
	if copyData {
		data = append([]byte(nil), data...)
	}
	q.ops = append(q.ops, ResourceOp{
		Kind: OpBakeBuffer, BufferID: id, BufferKind: types.BufferStorage, BufferSize: len(data),
		Bytes: data,
	})
	return descriptors.BakedBuffer(id, len(data))
}

// UploadTexture queues pixels into one layer of texture, over region, and
// returns texture; the zero region is the whole layer. A texture made with
// mipmaps has that layer's chain rebuilt when the upload covers the whole
// layer, and keeps its old smaller levels otherwise. copyData snapshots pixels
// when true; when false, the caller must keep them unchanged until the
// resource queue is consumed by the render thread.
func (q *ResourceQueue) UploadTexture(texture descriptors.TextureDescr, layer int, region types.Region, pixels []byte, copyData bool) descriptors.TextureDescr {
	if region == (types.Region{}) {
		region.Width, region.Height = texture.Size()
	}
	if copyData {
		pixels = append([]byte(nil), pixels...)
	}
	q.ops = append(q.ops, ResourceOp{
		Kind: OpUpdateTexture, TextureID: texture.ID(),
		TexLayer: layer, Region: region, Bytes: pixels,
	})
	return texture
}

// ReleaseBuffer queues a durable release for buffer.
func (q *ResourceQueue) ReleaseBuffer(buffer descriptors.BufferDescr) {
	q.ops = append(q.ops, ResourceOp{Kind: OpReleaseBuffer, BufferID: buffer.ID()})
}

// ReleaseTexture queues a durable release for texture.
func (q *ResourceQueue) ReleaseTexture(texture descriptors.TextureDescr) {
	q.ops = append(q.ops, ResourceOp{Kind: OpReleaseTexture, TextureID: texture.ID()})
}

func (q *ResourceQueue) releaseCachedResource(path string) {
	if path == "" {
		return
	}
	q.ops = append(q.ops, ResourceOp{Kind: OpReleaseCachedResource, Path: path})
}

func (q *ResourceQueue) freeCachedResources() {
	q.ops = append(q.ops, ResourceOp{Kind: OpFreeCachedResources})
}

func (q *ResourceQueue) reset() {
	clear(q.ops)
	q.ops = q.ops[:0]
}
