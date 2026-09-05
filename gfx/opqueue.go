package gfx

import (
	"cmp"
	"slices"
	"sort"

	"github.com/dvoyni/cog/m"
)

// opKind tags the variant of an op.
type opKind uint8

const (
	opDraw opKind = iota
	opBakeBuffer
	opReleaseBuffer
	opBakeTexture
	opReleaseTexture
	opReleaseCachedResource
	opFreeCachedResources
	opAllocateTexture
	opUpdateTexture
)

// op is one high-level command recorded into an OpQueue: a mesh draw, or a
// buffer/texture bake/release.
// Its fields are unexported; the translator reads them.
type op struct {
	kind opKind
	// pass indexes the OpQueue's pass list, and is meaningful for draws only:
	// resource ops belong to no pass, since bakes are hoisted ahead of them all.
	pass          int32
	mesh          MeshDescr
	material      MaterialDescr
	params        []ParameterDescr
	instances     int
	firstInstance int
	color         m.Color
	depth         float32
	bufferID      BufferID
	bufferKind    BufferKind
	bufferSize    int
	textureID     TextureID
	texW, texH    int
	texLayers     int
	texLayer      int
	region        Region
	format        TextureFormat
	mipmaps       bool
	renderable    bool
	bytes         []byte
	path          string
}

type temporaryBuffer struct {
	id   BufferID
	kind BufferKind
	size int
	used bool
}

type temporaryTexture struct {
	key  temporaryTextureKey
	id   TextureID
	used bool
}

type temporaryTextureKey struct {
	width      int
	height     int
	format     TextureFormat
	mipmaps    bool
	renderable bool
}

// opQueue records high-level frame commands. All uploads it owns are temporary
// and may be dropped with the frame. Persistent GPU resources are managed
// separately through resourceQueue.
type opQueue struct {
	backend Backend
	ops     []op
	// passes is the frame's declared passes and current the selected one, or -1
	// before anything selects a pass.
	passes               []passRecord
	current              int
	uploadArena          []byte
	parameterArena       []ParameterDescr
	vertexAttrArena      []VertexAttr
	temporaryBuffers     []temporaryBuffer
	temporaryNext        []int
	temporarySorted      int
	temporaryTextures    []temporaryTexture
	temporaryTextureFree map[temporaryTextureKey][]int
}

type readList struct{ *OpQueue }

// Len reports the number of recorded ops.
func (q *OpQueue) Len() int { return len(q.ops) }

// Reset drops all ops and makes temporary buffers available for reuse.
func (q *OpQueue) Reset() {
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
		return cmp.Compare(a.size, b.size)
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
		q.temporaryBuffers[i].used = false
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

// Draw records a draw op. Parameters are matched to reflected shader constants
// by name and override same-named material parameters. Inline geometry is baked
// into queue-pooled BufferIDs using each BufferDescr's copyData policy.
func (q *OpQueue) Draw(mesh MeshDescr, material MaterialDescr, params ...ParameterDescr) {
	q.draw(mesh, material, 0, 1, params)
}

// DrawInstanced records a draw that replays the mesh geometry `instances` times.
// Per-instance data is supplied through a storage-buffer parameter the shader
// indexes by instance_index; the shared parameters apply to every instance.
func (q *OpQueue) DrawInstanced(mesh MeshDescr, material MaterialDescr, instances int, params ...ParameterDescr) {
	q.draw(mesh, material, 0, instances, params)
}

// DrawInstancedFrom records an instanced draw starting at firstInstance.
// WebGPU's instance_index starts at firstInstance, so a batch reads its own
// slice of a shared instance arena with no offset plumbing of its own.
func (q *OpQueue) DrawInstancedFrom(mesh MeshDescr, material MaterialDescr, firstInstance, instances int, params ...ParameterDescr) {
	q.draw(mesh, material, firstInstance, instances, params)
}

func (q *OpQueue) draw(mesh MeshDescr, material MaterialDescr, firstInstance, instances int, params []ParameterDescr) {
	pass := int32(q.selectedPass())
	o := op{
		kind:          opDraw,
		pass:          pass,
		material:      q.bakeMaterialIfNeeded(material),
		mesh:          mesh,
		params:        q.bakeParametersIfNeeded(params),
		instances:     instances,
		firstInstance: firstInstance,
	}
	o.mesh.layout = q.copyVertexAttrs(mesh.layout)
	o.mesh.vertices = q.bakeBufferIfNeeded(mesh.vertices, BufferVertex)
	o.mesh.indices = q.bakeBufferIfNeeded(mesh.indices, BufferIndex)
	q.ops = append(q.ops, o)
}

func (q *OpQueue) bakeMaterialIfNeeded(material MaterialDescr) MaterialDescr {
	material.params = q.bakeParametersIfNeeded(material.params)
	return material
}

func (q *OpQueue) bakeParametersIfNeeded(params []ParameterDescr) []ParameterDescr {
	start := len(q.parameterArena)
	q.parameterArena = append(q.parameterArena, params...)
	baked := q.parameterArena[start:]
	for i := range baked {
		baked[i] = q.bakeParameterIfNeeded(baked[i])
	}
	return baked
}

func (q *OpQueue) copyVertexAttrs(attrs []VertexAttr) []VertexAttr {
	start := len(q.vertexAttrArena)
	q.vertexAttrArena = append(q.vertexAttrArena, attrs...)
	return q.vertexAttrArena[start:]
}

func (q *OpQueue) copyUpload(data []byte) []byte {
	start := len(q.uploadArena)
	q.uploadArena = append(q.uploadArena, data...)
	return q.uploadArena[start:]
}

func (q *OpQueue) bakeParameterIfNeeded(param ParameterDescr) ParameterDescr {
	switch param.kind {
	case paramBuffer:
		param.buffer = q.bakeBufferIfNeeded(param.buffer, BufferStorage)
	case paramTexture:
		param.texture = q.bakeTextureIfNeeded(param.texture)
	}
	return param
}

func (q *OpQueue) bakeBufferIfNeeded(buffer BufferDescr, kind BufferKind) BufferDescr {
	if buffer.source == BufferSourceBaked || len(buffer.bytes) == 0 {
		return buffer
	}
	return q.temporaryBuffer(kind, buffer.bytes, buffer.copyData)
}

func (q *OpQueue) bakeTextureIfNeeded(texture TextureDescr) TextureDescr {
	if texture.source == TextureSourceBaked || texture.source == TextureSourceResource {
		return texture
	}
	if texture.width <= 0 || texture.height <= 0 || len(texture.pixels) == 0 {
		return TextureDescr{}
	}
	return q.temporaryTexture(texture.width, texture.height, texture.format, texture.pixels, texture.copyData, texture.mipmaps)
}

func (q *OpQueue) temporaryBuffer(kind BufferKind, data []byte, copyData bool) BufferDescr {
	start := sort.Search(q.temporarySorted, func(i int) bool {
		buffer := &q.temporaryBuffers[i]
		return buffer.kind > kind || (buffer.kind == kind && buffer.size >= len(data))
	})
	best := q.nextTemporaryBuffer(start)
	if best >= q.temporarySorted || q.temporaryBuffers[best].kind != kind {
		best = -1
		for i := min(start, q.temporarySorted) - 1; i >= 0 && q.temporaryBuffers[i].kind == kind; i-- {
			if !q.temporaryBuffers[i].used {
				best = i
				break
			}
		}
	}
	if best < 0 {
		q.temporaryBuffers = append(q.temporaryBuffers, temporaryBuffer{
			id: q.backend.NewBuffer(), kind: kind, used: true,
		})
		best = len(q.temporaryBuffers) - 1
	} else {
		q.temporaryBuffers[best].used = true
		q.temporaryNext[best] = q.nextTemporaryBuffer(best + 1)
	}
	buffer := &q.temporaryBuffers[best]
	if buffer.size < len(data) {
		buffer.size = len(data)
	}
	return q.bakeBuffer(buffer.id, buffer.kind, buffer.size, data, copyData)
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

func (q *OpQueue) temporaryTexture(width, height int, format TextureFormat, pixels []byte, copyData, mipmaps bool) TextureDescr {
	key := temporaryTextureKey{width: width, height: height, format: format, mipmaps: mipmaps}
	return q.bakeTexture(q.acquireTemporaryTexture(key), width, height, format, pixels, copyData, mipmaps)
}

// TemporaryTarget allocates a frame-lifetime renderable texture and returns a
// target for it, for the passes that render into something they sample later in
// the same frame. Its contents do not survive the frame.
func (q *OpQueue) TemporaryTarget(width, height int, format TextureFormat) TargetDescr {
	key := temporaryTextureKey{width: width, height: height, format: format, renderable: true}
	id := q.acquireTemporaryTexture(key)
	q.ops = append(q.ops, op{
		kind: opAllocateTexture, textureID: id,
		texW: width, texH: height, texLayers: 1, format: format, renderable: true,
	})
	return TextureTarget(TextureDescr{source: TextureSourceBaked, id: id, width: width, height: height}, 0, 0)
}

// acquireTemporaryTexture takes a matching texture from the frame pool, minting
// one when the pool has none free.
func (q *OpQueue) acquireTemporaryTexture(key temporaryTextureKey) TextureID {
	free := q.temporaryTextureFree[key]
	best := -1
	if len(free) > 0 {
		best = free[len(free)-1]
		q.temporaryTextureFree[key] = free[:len(free)-1]
	}
	if best < 0 {
		q.temporaryTextures = append(q.temporaryTextures, temporaryTexture{key: key, id: q.backend.NewTexture()})
		best = len(q.temporaryTextures) - 1
	}
	q.temporaryTextures[best].used = true
	return q.temporaryTextures[best].id
}

func (q *OpQueue) bakeBuffer(id BufferID, kind BufferKind, size int, data []byte, copyData bool) BufferDescr {
	if copyData {
		data = q.copyUpload(data)
	}
	o := op{
		kind: opBakeBuffer, bufferID: id, bufferKind: kind, bufferSize: size,
		bytes: data,
	}
	q.ops = append(q.ops, o)
	return BufferDescr{source: BufferSourceBaked, id: id, size: len(data)}
}

func (q *OpQueue) bakeTexture(id TextureID, width, height int, format TextureFormat, pixels []byte, copyData, mipmaps bool) TextureDescr {
	if copyData {
		pixels = q.copyUpload(pixels)
	}
	o := op{
		kind:      opBakeTexture,
		textureID: id,
		texW:      width,
		texH:      height,
		format:    format,
		mipmaps:   mipmaps,
		bytes:     pixels,
	}
	q.ops = append(q.ops, o)
	return TextureDescr{source: TextureSourceBaked, id: id, width: width, height: height}
}
