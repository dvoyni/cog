package internal

import "github.com/dvoyni/cog/libs/m"

// The friend functions: what the gfx root and gfximpl read from a public
// type's unexported state. Only the root and gfximpl can import this package,
// so these are not public API.

// BufferBytes reads BufferDescr.bytes for the gfx root and gfximpl.
func BufferBytes(v *BufferDescr) m.Blob { return v.bytes }

// BufferSource reads BufferDescr.source for the gfx root and gfximpl.
func BufferSource(v *BufferDescr) bufferSource { return v.source }

// DepthKindOf reads DepthDescr.kind for the gfx root and gfximpl.
func DepthKindOf(v *DepthDescr) DepthKind { return v.kind }

// DepthTexture reads DepthDescr.texture for the gfx root and gfximpl.
func DepthTexture(v *DepthDescr) TextureID { return v.texture }

// GpuQueueBakes reads GpuQueue.bakes for the gfx root and gfximpl.
func GpuQueueBakes(v *GpuQueue) []GpuOp { return v.bakes }

// GpuQueueReleases reads GpuQueue.releases for the gfx root and gfximpl.
func GpuQueueReleases(v *GpuQueue) []GpuOp { return v.releases }

// GpuQueueRender reads GpuQueue.render for the gfx root and gfximpl.
func GpuQueueRender(v *GpuQueue) []GpuOp { return v.render }

// MeshIndices reads MeshDescr.indices for the gfx root and gfximpl.
func MeshIndices(v *MeshDescr) BufferDescr { return v.indices }

// MeshLayout reads MeshDescr.layout for the gfx root and gfximpl.
func MeshLayout(v *MeshDescr) []VertexAttr { return v.layout }

// MeshStride calls MeshDescr.stride for the gfx root and gfximpl.
func MeshStride(v *MeshDescr) int { return v.stride() }

// MeshVertices reads MeshDescr.vertices for the gfx root and gfximpl.
func MeshVertices(v *MeshDescr) BufferDescr { return v.vertices }

// MeshVerticesRef points at MeshDescr.vertices for the gfx root and gfximpl.
func MeshVerticesRef(v *MeshDescr) *BufferDescr { return &v.vertices }

// OpQueueBakeTextureIfNeeded calls OpQueue.bakeTextureIfNeeded for the gfx root and gfximpl.
func OpQueueBakeTextureIfNeeded(v *OpQueue, a0 TextureDescr) TextureDescr {
	return v.bakeTextureIfNeeded(a0)
}

// OpQueueCurrent reads OpQueue.current for the gfx root and gfximpl.
func OpQueueCurrent(v *OpQueue) int { return v.current }

// OpQueueOps reads OpQueue.ops for the gfx root and gfximpl.
func OpQueueOps(v *OpQueue) []Op { return v.ops }

// OpQueuePasses reads OpQueue.passes for the gfx root and gfximpl.
func OpQueuePasses(v *OpQueue) []passRecord { return v.passes }

// OpQueueSelectedPass calls OpQueue.selectedPass for the gfx root and gfximpl.
func OpQueueSelectedPass(v *OpQueue) int { return v.selectedPass() }

// OpQueueTemporaryBuffer calls OpQueue.temporaryBuffer for the gfx root and gfximpl.
func OpQueueTemporaryBuffer(v *OpQueue, a0 BufferKind, a1 []byte, a2 bool) BufferDescr {
	return v.temporaryBuffer(a0, a1, a2)
}

// OpQueueTemporaryBuffers reads OpQueue.temporaryBuffers for the gfx root and gfximpl.
func OpQueueTemporaryBuffers(v *OpQueue) []temporaryBuffer { return v.temporaryBuffers }

// ParameterBuffer reads ParameterDescr.buffer for the gfx root and gfximpl.
func ParameterBuffer(v *ParameterDescr) BufferDescr { return v.buffer }

// ParameterBufferOffset reads ParameterDescr.bufferOffset for the gfx root and gfximpl.
func ParameterBufferOffset(v *ParameterDescr) int { return v.bufferOffset }

// ParameterBufferRef points at ParameterDescr.buffer for the gfx root and gfximpl.
func ParameterBufferRef(v *ParameterDescr) *BufferDescr { return &v.buffer }

// ParameterBufferSize reads ParameterDescr.bufferSize for the gfx root and gfximpl.
func ParameterBufferSize(v *ParameterDescr) int { return v.bufferSize }

// ParameterColor reads ParameterDescr.color for the gfx root and gfximpl.
func ParameterColor(v *ParameterDescr) m.Color { return v.color }

// ParameterKind reads ParameterDescr.kind for the gfx root and gfximpl.
func ParameterKind(v *ParameterDescr) ParamKind { return v.kind }

// ParameterMat reads ParameterDescr.mat for the gfx root and gfximpl.
func ParameterMat(v *ParameterDescr) m.Mat4 { return v.mat }

// ParameterNum reads ParameterDescr.num for the gfx root and gfximpl.
func ParameterNum(v *ParameterDescr) float32 { return v.num }

// ParameterRaw reads ParameterDescr.raw for the gfx root and gfximpl.
func ParameterRaw(v *ParameterDescr) m.Blob { return v.raw }

// ParameterSampler reads ParameterDescr.sampler for the gfx root and gfximpl.
func ParameterSampler(v *ParameterDescr) SamplerDesc { return v.sampler }

// ParameterTexture reads ParameterDescr.texture for the gfx root and gfximpl.
func ParameterTexture(v *ParameterDescr) TextureDescr { return v.texture }

// ParameterTextureRef points at ParameterDescr.texture for the gfx root and gfximpl.
func ParameterTextureRef(v *ParameterDescr) *TextureDescr { return &v.texture }

// ParameterVec reads ParameterDescr.vec for the gfx root and gfximpl.
func ParameterVec(v *ParameterDescr) m.Vec4 { return v.vec }

// PassHasEffect calls PassDescr.hasEffect for the gfx root and gfximpl.
func PassHasEffect(v *PassDescr, a0 int) bool { return v.hasEffect(a0) }

// ResourceQueueFreeCachedResources calls ResourceQueue.freeCachedResources for the gfx root and gfximpl.
func ResourceQueueFreeCachedResources(v *ResourceQueue) { v.freeCachedResources() }

// ResourceQueueOps reads ResourceQueue.ops for the gfx root and gfximpl.
func ResourceQueueOps(v *ResourceQueue) []Op { return v.ops }

// ResourceQueueReleaseCachedResource calls ResourceQueue.releaseCachedResource for the gfx root and gfximpl.
func ResourceQueueReleaseCachedResource(v *ResourceQueue, a0 string) { v.releaseCachedResource(a0) }

// ResourceQueueReset calls ResourceQueue.reset for the gfx root and gfximpl.
func ResourceQueueReset(v *ResourceQueue) { v.reset() }

// TargetKindOf reads TargetDescr.kind for the gfx root and gfximpl.
func TargetKindOf(v *TargetDescr) TargetKind { return v.kind }

// TargetLayer reads TargetDescr.layer for the gfx root and gfximpl.
func TargetLayer(v *TargetDescr) int { return v.layer }

// TargetMip reads TargetDescr.mip for the gfx root and gfximpl.
func TargetMip(v *TargetDescr) int { return v.mip }

// TargetTextureOf reads TargetDescr.texture for the gfx root and gfximpl.
func TargetTextureOf(v *TargetDescr) TextureID { return v.texture }

// TexturePixels reads TextureDescr.pixels for the gfx root and gfximpl.
func TexturePixels(v *TextureDescr) m.Blob { return v.pixels }

// TextureSource reads TextureDescr.source for the gfx root and gfximpl.
func TextureSource(v *TextureDescr) textureSource { return v.source }

// VertexAttrOffset reads VertexAttr.offset for the gfx root and gfximpl.
func VertexAttrOffset(v *VertexAttr) int { return v.offset }

// VertexAttrTyp reads VertexAttr.typ for the gfx root and gfximpl.
func VertexAttrTyp(v *VertexAttr) VertexType { return v.typ }

// VertexScalarWgsl calls VertexScalar.wgsl for the gfx root and gfximpl.
func VertexScalarWgsl(v VertexScalar, a0 int) string { return v.wgsl(a0) }

// VertexTypeDecode calls VertexType.decode for the gfx root and gfximpl.
func VertexTypeDecode(v VertexType) (VertexScalar, int) { return v.decode() }

// VertexTypeSize calls VertexType.size for the gfx root and gfximpl.
func VertexTypeSize(v VertexType) int { return v.size() }
