package internal

import (
	"cmp"
	"github.com/dvoyni/cog/libs/m"
	"slices"
	"sort"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// OpKind tags the variant of a resource op.
type OpKind uint8

const (
	OpBakeBuffer OpKind = iota
	OpReleaseBuffer
	OpReleaseTexture
	OpReleaseCachedResource
	OpFreeCachedResources
	OpAllocateTexture
	OpUpdateTexture
	OpUploadProgram
	OpReleaseShader
)

// DrawOp is one mesh draw recorded into an OpQueue's pass.
type DrawOp struct {
	Mesh          types.MeshDescr
	Instances     int
	FirstInstance int
	// Set is the draw params the draw names. Version is where the set's version
	// for this draw starts in the queue's version slots, plus one, and zero
	// draws the set's own values.
	Set     types.DrawStateID
	Version int32
}

// ResourceOp is one resource command recorded into an OpQueue or a
// ResourceQueue: a buffer bake, a texture allocation or upload, a shader's
// program upload, or a release.
// Resource ops belong to no pass: the translator replays every one of them, in
// the order they were recorded, ahead of the frame's first pass, so their order
// against draws never matters. Their order among themselves does - a texture is
// allocated before it is uploaded, and a durable one released before its id is
// seen again.
type ResourceOp struct {
	Kind       OpKind
	BufferID   types.BufferID
	BufferKind types.BufferKind
	BufferSize int
	TextureID  types.TextureID
	TexW, TexH int
	TexLayers  int
	TexLayer   int
	Region     m.Recti
	Format     types.TextureFormat
	Mipmaps    bool
	Renderable bool
	Bytes      []byte
	Path       string
	// ShaderID and Program describe a shader's upload or release. Program is
	// one word, a handle to the immutable compiled module.
	ShaderID types.ShaderID
	Program  shader.ShaderProgram
}

type temporaryBuffer struct {
	id   types.BufferID
	kind types.BufferKind
	Size int
	Used bool
}

type temporaryTexture struct {
	key  temporaryTextureKey
	id   types.TextureID
	used bool
}

type temporaryTextureKey struct {
	width      int
	height     int
	format     types.TextureFormat
	mipmaps    bool
	renderable bool
}

// passRecord is one declared pass and the draws recorded into it, in the order
// they were recorded. Its place in the pass list breaks Order ties.
type passRecord struct {
	Desc  types.PassDescr
	Draws []DrawOp
}

// OpQueue records high-level frame commands. All uploads it owns are temporary
// and may be dropped with the frame. Persistent GPU resources are managed
// separately through ResourceQueue.
type OpQueue struct {
	ids IDSource
	// resources is the frame's temporary bakes, allocations and uploads, which
	// the translator replays ahead of every draw.
	resources []ResourceOp
	// passes is the frame's declared passes; a PassId is an index into it,
	// plus one. Records past its length keep their draw lists' backing for the
	// passes later frames declare.
	passes []passRecord
	// strayDraws counts the frame's draws that named no pass declared this
	// frame. They are dropped, and only counted, so the frame can report them.
	strayDraws           int
	uploadArena          []byte
	vertexAttrArena      []types.VertexAttribute
	temporaryBuffers     []temporaryBuffer
	temporaryNext        []int
	temporarySorted      int
	temporaryTextures    []temporaryTexture
	temporaryTextureFree map[temporaryTextureKey][]int
	// frame counts the queue's frames, advanced by every Reset. A set's cursor
	// belongs to the frame it names, and is empty in any other.
	frame uint64

	// sets is what the ResourceQueue publishes of every set, which is all
	// SetDrawParams reads of one; see drawParamsRegistry.
	sets *drawParamsRegistry
	// cursors is each set's current version this frame, indexed by set id.
	cursors []setCursor
	// versionValues, versionBytes and versionPaths are the frame's versions:
	// their binding tables, their uniforms' bytes and the texture paths they
	// name. Reset truncates them.
	versionValues []bindingValue
	versionBytes  []byte
	versionPaths  []string
}

// NewOpQueue builds an empty queue that reserves ids through ids. It knows no
// set of draw params; the plugin's queues are built by newOpQueue, beside the
// ResourceQueue whose sets they version.
func NewOpQueue(ids IDSource) *OpQueue { return newOpQueue(ids, nil) }

func newOpQueue(ids IDSource, sets *drawParamsRegistry) *OpQueue {
	return &OpQueue{ids: ids, sets: sets, temporaryTextureFree: map[temporaryTextureKey][]int{}}
}

// reset drops all ops and makes temporary buffers available for reuse.
func (q *OpQueue) reset() {
	q.frame++
	clear(q.resources)
	q.resources = q.resources[:0]
	for i := range q.passes {
		pass := &q.passes[i]
		clear(pass.Draws)
		pass.Draws = pass.Draws[:0]
		pass.Desc = types.PassDescr{}
	}
	q.passes = q.passes[:0]
	q.strayDraws = 0
	q.uploadArena = q.uploadArena[:0]
	q.vertexAttrArena = q.vertexAttrArena[:0]
	q.versionValues = q.versionValues[:0]
	q.versionBytes = q.versionBytes[:0]
	clear(q.versionPaths)
	q.versionPaths = q.versionPaths[:0]
	slices.SortFunc(q.temporaryBuffers, func(a, b temporaryBuffer) int {
		if a.kind != b.kind {
			return cmp.Compare(a.kind, b.kind)
		}
		return cmp.Compare(a.Size, b.Size)
	})
	q.temporarySorted = len(q.temporaryBuffers)
	if cap(q.temporaryNext) < q.temporarySorted+1 {
		q.temporaryNext = make([]int, q.temporarySorted+1)
	} else {
		q.temporaryNext = q.temporaryNext[:q.temporarySorted+1]
	}
	for i := range q.temporaryNext {
		q.temporaryNext[i] = i
	}
	for i := range q.temporaryBuffers {
		q.temporaryBuffers[i].Used = false
	}
	if q.temporaryTextureFree == nil {
		q.temporaryTextureFree = map[temporaryTextureKey][]int{}
	} else {
		clear(q.temporaryTextureFree)
	}
	for i := range q.temporaryTextures {
		q.temporaryTextures[i].used = false
	}
	for i := len(q.temporaryTextures) - 1; i >= 0; i-- {
		key := q.temporaryTextures[i].key
		q.temporaryTextureFree[key] = append(q.temporaryTextureFree[key], i)
	}
}

// NewPass declares a pass for this frame and returns the reference every Draw
// into it names. Passes run in Order, not in the order they were declared, and
// a pass's draws run in the order they were recorded, so draws into several
// passes may be recorded interleaved. The reference is valid until the frame
// ends; hand it to another System to let it draw into the same pass.
func (q *OpQueue) NewPass(desc types.PassDescr) types.PassID {
	if n := len(q.passes); n < cap(q.passes) {
		q.passes = q.passes[:n+1]
		q.passes[n].Desc = desc
	} else {
		q.passes = append(q.passes, passRecord{Desc: desc})
	}
	return types.PassID(len(q.passes))
}

// passIndex returns the index of the pass ref names, or -1 when it names none
// declared this frame. Every draw names a pass: there is no implicit one,
// because a default screen pass would silently absorb draws that belonged in a
// camera's target, and it would have to guess an Order.
func (q *OpQueue) passIndex(ref types.PassID) int {
	if ref < 1 || int(ref) > len(q.passes) {
		return -1
	}
	return int(ref) - 1
}

func (q *OpQueue) copyVertexAttrs(attrs []types.VertexAttribute) []types.VertexAttribute {
	start := len(q.vertexAttrArena)
	q.vertexAttrArena = append(q.vertexAttrArena, attrs...)
	return q.vertexAttrArena[start:]
}

func (q *OpQueue) copyUpload(data []byte) []byte {
	start := len(q.uploadArena)
	q.uploadArena = append(q.uploadArena, data...)
	return q.uploadArena[start:]
}

func (q *OpQueue) bakeBufferIfNeeded(buffer types.BufferDescr, kind types.BufferKind) types.BufferDescr {
	bytes := buffer.Bytes
	if buffer.ID != 0 || bytes.Len() == 0 {
		return buffer
	}
	return q.temporaryBuffer(kind, bytes.Data(), buffer.CopyData)
}

func (q *OpQueue) bakeTextureIfNeeded(texture types.TextureDescr) types.TextureDescr {
	// A baked texture already carries its id, and a path names one the render
	// thread resolves against its own cache. Only inline pixels are this
	// queue's to upload.
	if texture.Params.ID != 0 || texture.Name != "" {
		return texture
	}
	width, height := texture.Params.Width, texture.Params.Height
	if width <= 0 || height <= 0 || texture.Blob.Len() == 0 {
		return types.TextureDescr{}
	}
	return q.temporaryTexture(
		width, height, texture.Params.Format,
		texture.Blob.Data(), texture.Params.CopyData, texture.Params.Mipmaps,
	)
}

// NewTemporaryBuffer uploads one frame-lifetime storage buffer and returns the
// baked descriptor for it, so every draw that binds a range of it shares one
// upload. It is the arena counterpart of NewTemporaryTarget: BufferDescrWithBlob
// re-bakes wherever it is recorded, which is right for a buffer one draw owns
// and wrong for one the whole frame reads.
//
// copyData snapshots the bytes when true; when false the caller must keep them
// unchanged until the recorded frame is consumed or dropped. Its contents do
// not survive the frame.
func (q *OpQueue) NewTemporaryBuffer(data []byte, copyData bool) types.BufferDescr {
	if len(data) == 0 {
		return types.BufferDescr{}
	}
	return q.temporaryBuffer(types.BufferStorage, data, copyData)
}

func (q *OpQueue) temporaryBuffer(kind types.BufferKind, data []byte, copyData bool) types.BufferDescr {
	start := sort.Search(q.temporarySorted, func(i int) bool {
		buffer := &q.temporaryBuffers[i]
		return buffer.kind > kind || (buffer.kind == kind && buffer.Size >= len(data))
	})
	best := q.nextTemporaryBuffer(start)
	if best >= q.temporarySorted || q.temporaryBuffers[best].kind != kind {
		best = -1
		for i := min(start, q.temporarySorted) - 1; i >= 0 && q.temporaryBuffers[i].kind == kind; i-- {
			if !q.temporaryBuffers[i].Used {
				best = i
				break
			}
		}
	}
	if best < 0 {
		q.temporaryBuffers = append(q.temporaryBuffers, temporaryBuffer{
			id: q.ids().NewBuffer(), kind: kind, Used: true,
		})
		best = len(q.temporaryBuffers) - 1
	} else {
		q.temporaryBuffers[best].Used = true
		q.temporaryNext[best] = q.nextTemporaryBuffer(best + 1)
	}
	buffer := &q.temporaryBuffers[best]
	if buffer.Size < len(data) {
		buffer.Size = len(data)
	}
	return q.bakeBuffer(buffer.id, buffer.kind, buffer.Size, data, copyData)
}

func (q *OpQueue) nextTemporaryBuffer(index int) int {
	if index >= q.temporarySorted {
		return q.temporarySorted
	}
	next := q.temporaryNext[index]
	if next != index {
		q.temporaryNext[index] = q.nextTemporaryBuffer(next)
	}
	return q.temporaryNext[index]
}

// NewTemporaryTexture uploads one frame-lifetime texture and returns the baked
// descriptor for it, so every draw that samples it shares one upload. It is the
// texture counterpart of NewTemporaryBuffer, and for the same reason:
// TextureWithBytes re-bakes wherever it is bound, because the queue cannot tell
// the same pixels bound twice from the same slice refilled between draws. Only
// the caller knows, and taking this handle is how it says so.
//
// mipmaps has the backend build the chain from pixels. copyData snapshots the
// pixels when true; when false the caller must keep them unchanged until the
// recorded frame is consumed or dropped. Its contents do not survive the frame.
func (q *OpQueue) NewTemporaryTexture(width, height int, format types.TextureFormat, pixels []byte, copyData, mipmaps bool) types.TextureDescr {
	if width <= 0 || height <= 0 || len(pixels) == 0 {
		return types.TextureDescr{}
	}
	return q.temporaryTexture(width, height, format, pixels, copyData, mipmaps)
}

// temporaryTexture allocates a pooled texture and uploads pixels as its one
// whole layer, the same allocate-then-upload pair ResourceQueue records, so a
// mipmapped one has its chain rebuilt by the backend from that upload.
func (q *OpQueue) temporaryTexture(width, height int, format types.TextureFormat, pixels []byte, copyData, mipmaps bool) types.TextureDescr {
	texture := q.allocateTemporaryTexture(temporaryTextureKey{width: width, height: height, format: format, mipmaps: mipmaps})
	if copyData {
		pixels = q.copyUpload(pixels)
	}
	q.resources = append(q.resources, ResourceOp{
		Kind: OpUpdateTexture, TextureID: texture.Params.ID,
		Region: m.Recti{Width: width, Height: height}, Bytes: pixels,
	})
	return texture
}

// NewTemporaryTarget allocates a frame-lifetime renderable texture and returns
// both handles onto it: the target a pass renders into, and the texture a later
// pass samples. Its contents do not survive the frame.
//
// Both come back because a TargetDescr is write-only - it names an attachment
// and answers nothing about the texture behind it - and sampling the result is
// the entire reason this allocation exists. Split-screen, minimap,
// picture-in-picture, render scale and post-processing are all spelled as one
// temporary target per camera composited later in the same frame.
//
// A draw still may not sample the target its own pass renders into; that is
// ErrDrawSamplesAttachment, and it is the guard that makes handing the texture
// back safe.
func (q *OpQueue) NewTemporaryTarget(width, height int, format types.TextureFormat) (types.TargetDescr, types.TextureDescr) {
	texture := q.allocateTemporaryTexture(temporaryTextureKey{width: width, height: height, format: format, renderable: true})
	return types.TargetDescrTexture(texture, 0, 0), texture
}

// allocateTemporaryTexture takes a matching texture from the frame pool and
// records its allocation. The allocation is recorded every frame the texture is
// taken, and a backend that already holds the id at that description keeps it,
// so a settled pool allocates nothing.
func (q *OpQueue) allocateTemporaryTexture(key temporaryTextureKey) types.TextureDescr {
	id := q.acquireTemporaryTexture(key)
	q.resources = append(q.resources, ResourceOp{
		Kind: OpAllocateTexture, TextureID: id,
		TexW: key.width, TexH: key.height, TexLayers: 1, Format: key.format,
		Mipmaps: key.mipmaps, Renderable: key.renderable,
	})
	return types.BakedTextureWith(id, key.width, key.height, 1, key.format)
}

// acquireTemporaryTexture takes a matching texture from the frame pool, minting
// one when the pool has none free.
func (q *OpQueue) acquireTemporaryTexture(key temporaryTextureKey) types.TextureID {
	free := q.temporaryTextureFree[key]
	best := -1
	if len(free) > 0 {
		best = free[len(free)-1]
		q.temporaryTextureFree[key] = free[:len(free)-1]
	}
	if best < 0 {
		q.temporaryTextures = append(q.temporaryTextures, temporaryTexture{key: key, id: q.ids().NewTexture()})
		best = len(q.temporaryTextures) - 1
	}
	q.temporaryTextures[best].used = true
	return q.temporaryTextures[best].id
}

func (q *OpQueue) bakeBuffer(id types.BufferID, kind types.BufferKind, size int, data []byte, copyData bool) types.BufferDescr {
	if copyData {
		data = q.copyUpload(data)
	}
	q.resources = append(q.resources, ResourceOp{
		Kind: OpBakeBuffer, BufferID: id, BufferKind: kind, BufferSize: size,
		Bytes: data,
	})
	return types.BufferDescrWithId(id, len(data))
}
