package internal

import (
	"cmp"
	"slices"
	"sort"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/libs/m"
)

// OpKind tags the variant of an op.
type OpKind uint8

const (
	OpDraw OpKind = iota
	OpBakeBuffer
	OpReleaseBuffer
	OpBakeTexture
	OpReleaseTexture
	OpReleaseCachedResource
	OpFreeCachedResources
	OpAllocateTexture
	OpUpdateTexture
)

// Op is one high-level command recorded into an OpQueue: a mesh draw, or a
// buffer/texture bake/release.
// Its fields are unexported; the translator reads them.
type Op struct {
	Kind OpKind
	// pass indexes the OpQueue's pass list, and is meaningful for draws only:
	// resource ops belong to no pass, since bakes are hoisted ahead of them all.
	Pass          int32
	Mesh          descriptors.MeshDescr
	Material      descriptors.MaterialDescr
	Params        []descriptors.ParameterDescr
	Instances     int
	FirstInstance int
	color         m.Color
	depth         float32
	BufferID      types.BufferID
	BufferKind    types.BufferKind
	BufferSize    int
	TextureID     types.TextureID
	TexW, TexH    int
	TexLayers     int
	TexLayer      int
	Region        types.Region
	Format        descriptors.TextureFormat
	Mipmaps       bool
	Renderable    bool
	Bytes         []byte
	Path          string
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
	format     descriptors.TextureFormat
	mipmaps    bool
	renderable bool
}

// passRecord is one declared pass and the position that breaks Order ties.
type passRecord struct {
	Desc descriptors.PassDescr
	seq  int
}

// IDMinter is the half of the Backend a recording queue calls: it reserves
// logical resource ids, which is CPU-only and safe from the recording thread.
type IDMinter interface {
	NewTexture() types.TextureID
	NewBuffer() types.BufferID
	Ready() bool
}

// IDSource reaches the Backend adapter. It is read when an id is needed rather
// than when the queue is built, because a queue is built during registration
// and the adapter is bound only when composition finishes.
type IDSource func() IDMinter

// OpQueue records high-level frame commands. All uploads it owns are temporary
// and may be dropped with the frame. Persistent GPU resources are managed
// separately through ResourceQueue.
type OpQueue struct {
	ids IDSource
	ops []Op
	// passes is the frame's declared passes and current the selected one, or -1
	// before anything selects a pass.
	passes               []passRecord
	current              int
	uploadArena          []byte
	parameterArena       []descriptors.ParameterDescr
	vertexAttrArena      []descriptors.VertexAttr
	temporaryBuffers     []temporaryBuffer
	temporaryNext        []int
	temporarySorted      int
	temporaryTextures    []temporaryTexture
	temporaryTextureFree map[temporaryTextureKey][]int
	// frame counts the queue's frames, advanced by every Reset. A material
	// FrameMaterial recorded is the queue's for the frame it names, and an
	// ordinary material in any other.
	frame uint64
}

// NewOpQueue builds an empty queue that reserves ids through ids.
func NewOpQueue(ids IDSource) *OpQueue {
	return &OpQueue{ids: ids, temporaryTextureFree: map[temporaryTextureKey][]int{}}
}

// Len reports the number of recorded ops.
func (q *OpQueue) Len() int { return len(q.ops) }

// Reset drops all ops and makes temporary buffers available for reuse.
func (q *OpQueue) Reset() {
	q.frame++
	clear(q.ops)
	q.ops = q.ops[:0]
	clear(q.passes)
	q.passes = q.passes[:0]
	q.current = -1
	q.uploadArena = q.uploadArena[:0]
	q.parameterArena = q.parameterArena[:0]
	q.vertexAttrArena = q.vertexAttrArena[:0]
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

// Pass declares a pass and selects it: every op recorded afterwards appends to
// it, until another Pass or SetPass call. Passes run in Order, not in the order
// they were declared.
func (q *OpQueue) Pass(desc descriptors.PassDescr) descriptors.PassRef {
	q.passes = append(q.passes, passRecord{Desc: desc, seq: len(q.passes)})
	q.current = len(q.passes) - 1
	return descriptors.PassRef(len(q.passes))
}

// SetPass re-selects a pass declared earlier this frame. An unknown reference
// is ignored.
func (q *OpQueue) SetPass(ref descriptors.PassRef) {
	if ref > 0 && int(ref) <= len(q.passes) {
		q.current = int(ref) - 1
	}
}

// selectedPass returns the index of the pass ops are appended to, or -1 when no
// pass is selected. Every draw names a pass: there is no implicit one, because
// a default screen pass would silently absorb draws that belonged in a camera's
// target, and it would have to guess an Order.
func (q *OpQueue) selectedPass() int {
	if q.current < 0 || q.current >= len(q.passes) {
		return -1
	}
	return q.current
}

// Draw records a draw op. Parameters are matched to reflected shader constants
// by name and override same-named material parameters. Inline geometry is baked
// into queue-pooled BufferIDs using each BufferDescr's copyData policy.
func (q *OpQueue) Draw(mesh descriptors.MeshDescr, material descriptors.MaterialDescr, params ...descriptors.ParameterDescr) {
	q.draw(mesh, material, 0, 1, params)
}

// DrawInstanced records a draw that replays the mesh geometry `instances` times.
// Per-instance data is supplied through a storage-buffer parameter the shader
// indexes by instance_index; the shared parameters apply to every instance.
func (q *OpQueue) DrawInstanced(mesh descriptors.MeshDescr, material descriptors.MaterialDescr, instances int, params ...descriptors.ParameterDescr) {
	q.draw(mesh, material, 0, instances, params)
}

// DrawInstancedFrom records an instanced draw starting at firstInstance.
// WebGPU's instance_index starts at firstInstance, so a batch reads its own
// slice of a shared instance arena with no offset plumbing of its own.
func (q *OpQueue) DrawInstancedFrom(mesh descriptors.MeshDescr, material descriptors.MaterialDescr, firstInstance, instances int, params ...descriptors.ParameterDescr) {
	q.draw(mesh, material, firstInstance, instances, params)
}

func (q *OpQueue) draw(mesh descriptors.MeshDescr, material descriptors.MaterialDescr, firstInstance, instances int, params []descriptors.ParameterDescr) {
	pass := int32(q.selectedPass())
	o := Op{
		Kind:          OpDraw,
		Pass:          pass,
		Material:      q.bakeMaterialIfNeeded(material),
		Mesh:          mesh,
		Params:        q.bakeParametersIfNeeded(params),
		Instances:     instances,
		FirstInstance: firstInstance,
	}
	o.Mesh = descriptors.WithMeshBuffers(mesh,
		q.copyVertexAttrs(descriptors.MeshLayout(&mesh)),
		q.bakeBufferIfNeeded(descriptors.MeshVertices(&mesh), types.BufferVertex),
		q.bakeBufferIfNeeded(descriptors.MeshIndices(&mesh), types.BufferIndex))
	q.ops = append(q.ops, o)
}

// FrameMaterial records a material's params into the queue once, for the rest
// of this frame, and returns a material every draw of it can name without the
// queue copying and baking them again.
//
// Draw copies a material's params on every draw, because gfx owns nothing a
// caller passes and the caller may reuse its slice when Draw returns. A caller
// drawing one material many times in a frame - a renderer's Batches, which
// share a material and differ in their own params - pays that copy, and the
// translator's hash of every name, once a draw. Recording pays both once: the
// params are copied here, and the shape state of their names is taken here, so
// the translator hashes only a draw's own params to find its plan.
//
// The returned material is this queue's for this frame. The caller may reuse
// its own slice at once, as after Draw. In a later frame, or on another queue,
// it draws as the material it was recorded from - copied from the params the
// caller passed here, as every material is - so a recording held too long costs
// the copy again and nothing else.
func (q *OpQueue) FrameMaterial(material descriptors.MaterialDescr) descriptors.MaterialDescr {
	if q.recordedHere(&material) {
		return material
	}
	start := len(q.parameterArena)
	baked := q.bakeParametersIfNeeded(material.Params())
	descriptors.SetMaterialRecording(&material, descriptors.FrameRecording{Queue: q, Frame: q.frame, Start: start, Shape: descriptors.ParameterShapeState(baked)})
	return material
}

// recordedHere reports whether a material was recorded by this queue in this
// frame, so its params are a window of this frame's arena.
func (q *OpQueue) recordedHere(material *descriptors.MaterialDescr) bool {
	recording := descriptors.MaterialRecording(material)
	return recording.Queue == any(q) && recording.Frame == q.frame
}

func (q *OpQueue) bakeMaterialIfNeeded(material descriptors.MaterialDescr) descriptors.MaterialDescr {
	if q.recordedHere(&material) {
		// The window is found by its start rather than kept as a slice,
		// because the arena may have grown into a new backing since, and the
		// params belong in the op as the arena the frame hands over holds them.
		start := descriptors.MaterialRecording(&material).Start
		count := len(material.Params())
		descriptors.SetMaterialParams(&material, q.parameterArena[start:start+count:start+count])
		return material
	}
	// Unrecorded, stale or another queue's: its params are the caller's, and
	// the draw copies them as it copies every material's.
	descriptors.SetMaterialParams(&material, q.bakeParametersIfNeeded(material.Params()))
	descriptors.SetMaterialRecording(&material, descriptors.FrameRecording{})
	return material
}

func (q *OpQueue) bakeParametersIfNeeded(params []descriptors.ParameterDescr) []descriptors.ParameterDescr {
	start := len(q.parameterArena)
	q.parameterArena = append(q.parameterArena, params...)
	baked := q.parameterArena[start:]
	for i := range baked {
		baked[i] = q.bakeParameterIfNeeded(baked[i])
	}
	return baked
}

func (q *OpQueue) copyVertexAttrs(attrs []descriptors.VertexAttr) []descriptors.VertexAttr {
	start := len(q.vertexAttrArena)
	q.vertexAttrArena = append(q.vertexAttrArena, attrs...)
	return q.vertexAttrArena[start:]
}

func (q *OpQueue) copyUpload(data []byte) []byte {
	start := len(q.uploadArena)
	q.uploadArena = append(q.uploadArena, data...)
	return q.uploadArena[start:]
}

func (q *OpQueue) bakeParameterIfNeeded(param descriptors.ParameterDescr) descriptors.ParameterDescr {
	switch descriptors.ParameterKind(&param) {
	case descriptors.ParamBuffer:
		return descriptors.WithParameterBuffer(param, q.bakeBufferIfNeeded(descriptors.ParameterBuffer(&param), types.BufferStorage))
	case descriptors.ParamTexture:
		return descriptors.WithParameterTexture(param, q.bakeTextureIfNeeded(descriptors.ParameterTexture(&param)))
	}
	return param
}

func (q *OpQueue) bakeBufferIfNeeded(buffer descriptors.BufferDescr, kind types.BufferKind) descriptors.BufferDescr {
	bytes := descriptors.BufferBytes(&buffer)
	if descriptors.BufferSource(&buffer) == descriptors.BufferSourceBaked || bytes.Len() == 0 {
		return buffer
	}
	return q.temporaryBuffer(kind, bytes.Data(), descriptors.BufferCopyData(&buffer))
}

func (q *OpQueue) bakeTextureIfNeeded(texture descriptors.TextureDescr) descriptors.TextureDescr {
	// A baked texture already carries its id, and a path names one the render
	// thread resolves against its own cache. Only inline pixels are this
	// queue's to upload.
	if texture.ID() != 0 || texture.Name != "" {
		return texture
	}
	width, height := texture.Size()
	if width <= 0 || height <= 0 || texture.Blob.Len() == 0 {
		return descriptors.TextureDescr{}
	}
	return q.temporaryTexture(
		width, height, texture.Format(),
		texture.Blob.Data(), descriptors.TextureCopyData(&texture), texture.Mipmaps(),
	)
}

// TemporaryBuffer uploads one frame-lifetime storage buffer and returns the
// baked descriptor for it, so every draw that binds a range of it shares one
// upload. It is the arena counterpart of TemporaryTarget: BufferWithBytes
// re-bakes wherever it is recorded, which is right for a buffer one draw owns
// and wrong for one the whole frame reads.
//
// copyData snapshots the bytes when true; when false the caller must keep them
// unchanged until the recorded frame is consumed or dropped. Its contents do
// not survive the frame.
func (q *OpQueue) TemporaryBuffer(data []byte, copyData bool) descriptors.BufferDescr {
	if len(data) == 0 {
		return descriptors.BufferDescr{}
	}
	return q.temporaryBuffer(types.BufferStorage, data, copyData)
}

func (q *OpQueue) temporaryBuffer(kind types.BufferKind, data []byte, copyData bool) descriptors.BufferDescr {
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

func (q *OpQueue) temporaryTexture(width, height int, format descriptors.TextureFormat, pixels []byte, copyData, mipmaps bool) descriptors.TextureDescr {
	key := temporaryTextureKey{width: width, height: height, format: format, mipmaps: mipmaps}
	return q.bakeTexture(q.acquireTemporaryTexture(key), width, height, format, pixels, copyData, mipmaps)
}

// TemporaryTarget allocates a frame-lifetime renderable texture and returns
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
func (q *OpQueue) TemporaryTarget(width, height int, format descriptors.TextureFormat) (descriptors.TargetDescr, descriptors.TextureDescr) {
	key := temporaryTextureKey{width: width, height: height, format: format, renderable: true}
	id := q.acquireTemporaryTexture(key)
	q.ops = append(q.ops, Op{
		Kind: OpAllocateTexture, TextureID: id,
		TexW: width, TexH: height, TexLayers: 1, Format: format, Renderable: true,
	})
	texture := descriptors.BakedTextureWith(id, width, height, 1, format)
	return descriptors.TextureTarget(texture, 0, 0), texture
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

func (q *OpQueue) bakeBuffer(id types.BufferID, kind types.BufferKind, size int, data []byte, copyData bool) descriptors.BufferDescr {
	if copyData {
		data = q.copyUpload(data)
	}
	o := Op{
		Kind: OpBakeBuffer, BufferID: id, BufferKind: kind, BufferSize: size,
		Bytes: data,
	}
	q.ops = append(q.ops, o)
	return descriptors.BakedBuffer(id, len(data))
}

func (q *OpQueue) bakeTexture(id types.TextureID, width, height int, format descriptors.TextureFormat, pixels []byte, copyData, mipmaps bool) descriptors.TextureDescr {
	if copyData {
		pixels = q.copyUpload(pixels)
	}
	o := Op{
		Kind:      OpBakeTexture,
		TextureID: id,
		TexW:      width,
		TexH:      height,
		Format:    format,
		Mipmaps:   mipmaps,
		Bytes:     pixels,
	}
	q.ops = append(q.ops, o)
	return descriptors.BakedTexture(id, width, height)
}
