package types

import (
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
)

// The friend functions: what gfx's internal/ reads from a public type's
// unexported state. Only packages under slots/gfx can import this package, so
// these are not public API.

// BufferBytes reads BufferDescr.bytes for gfx's internal/.
func BufferBytes(v *BufferDescr) assets.Blob { return v.bytes }

// BufferSource reads BufferDescr.source for gfx's internal/.
func BufferSource(v *BufferDescr) bufferSource { return v.source }

// DepthKindOf reads DepthDescr.kind for gfx's internal/.
func DepthKindOf(v *DepthDescr) DepthKind { return v.kind }

// DepthTexture reads DepthDescr.texture for gfx's internal/.
func DepthTexture(v *DepthDescr) TextureID { return v.texture }

// MaterialShapeState reads the shape state OpQueue.FrameMaterial took of a
// recorded material's param names, and whether it has one.
func MaterialShapeState(v *MaterialDescr) (uint64, bool) {
	if v.recorded.queue == nil {
		return 0, false
	}
	return v.recorded.shape, true
}

// MeshIndices reads MeshDescr.indices for gfx's internal/.
func MeshIndices(v *MeshDescr) BufferDescr { return v.indices }

// MeshLayout reads MeshDescr.layout for gfx's internal/.
func MeshLayout(v *MeshDescr) []VertexAttr { return v.layout }

// MeshStride calls MeshDescr.stride for gfx's internal/.
func MeshStride(v *MeshDescr) int { return v.stride() }

// MeshVertices reads MeshDescr.vertices for gfx's internal/.
func MeshVertices(v *MeshDescr) BufferDescr { return v.vertices }

// MeshVerticesRef points at MeshDescr.vertices for gfx's internal/.
func MeshVerticesRef(v *MeshDescr) *BufferDescr { return &v.vertices }

// OpQueueBakeTextureIfNeeded calls OpQueue.bakeTextureIfNeeded for gfx's internal/.
func OpQueueBakeTextureIfNeeded(v *OpQueue, a0 TextureDescr) TextureDescr {
	return v.bakeTextureIfNeeded(a0)
}

// OpQueueCurrent reads OpQueue.current for gfx's internal/.
func OpQueueCurrent(v *OpQueue) int { return v.current }

// OpQueueOps reads OpQueue.ops for gfx's internal/.
func OpQueueOps(v *OpQueue) []Op { return v.ops }

// OpQueuePasses reads OpQueue.passes for gfx's internal/.
func OpQueuePasses(v *OpQueue) []passRecord { return v.passes }

// OpQueueSelectedPass calls OpQueue.selectedPass for gfx's internal/.
func OpQueueSelectedPass(v *OpQueue) int { return v.selectedPass() }

// OpQueueTemporaryBuffer calls OpQueue.temporaryBuffer for gfx's internal/.
func OpQueueTemporaryBuffer(v *OpQueue, a0 BufferKind, a1 []byte, a2 bool) BufferDescr {
	return v.temporaryBuffer(a0, a1, a2)
}

// OpQueueTemporaryBuffers reads OpQueue.temporaryBuffers for gfx's internal/.
func OpQueueTemporaryBuffers(v *OpQueue) []temporaryBuffer { return v.temporaryBuffers }

// ParameterBuffer reads ParameterDescr.buffer for gfx's internal/.
func ParameterBuffer(v *ParameterDescr) BufferDescr { return v.buffer }

// ParameterBufferOffset reads ParameterDescr.bufferOffset for gfx's internal/.
func ParameterBufferOffset(v *ParameterDescr) int { return v.bufferOffset }

// ParameterBufferRef points at ParameterDescr.buffer for gfx's internal/.
func ParameterBufferRef(v *ParameterDescr) *BufferDescr { return &v.buffer }

// ParameterBufferSize reads ParameterDescr.bufferSize for gfx's internal/.
func ParameterBufferSize(v *ParameterDescr) int { return v.bufferSize }

// ParameterColor reads ParameterDescr.color for gfx's internal/.
func ParameterColor(v *ParameterDescr) m.Color { return v.color }

// ParameterKind reads ParameterDescr.kind for gfx's internal/.
func ParameterKind(v *ParameterDescr) ParamKind { return v.kind }

// ParameterMat reads ParameterDescr.mat for gfx's internal/.
func ParameterMat(v *ParameterDescr) m.Mat4 { return v.mat }

// ParameterNum reads ParameterDescr.num for gfx's internal/.
func ParameterNum(v *ParameterDescr) float32 { return v.num }

// ParameterRaw reads ParameterDescr.raw for gfx's internal/.
func ParameterRaw(v *ParameterDescr) assets.Blob { return v.raw }

// ParameterSampler reads ParameterDescr.sampler for gfx's internal/.
func ParameterSampler(v *ParameterDescr) SamplerDesc { return v.sampler }

// ParameterTexture reads ParameterDescr.texture for gfx's internal/.
func ParameterTexture(v *ParameterDescr) TextureDescr { return v.texture }

// ParameterTextureRef points at ParameterDescr.texture for gfx's internal/.
func ParameterTextureRef(v *ParameterDescr) *TextureDescr { return &v.texture }

// ParameterVec reads ParameterDescr.vec for gfx's internal/.
func ParameterVec(v *ParameterDescr) m.Vec4 { return v.vec }

// PassHasEffect calls PassDescr.hasEffect for gfx's internal/.
func PassHasEffect(v *PassDescr, a0 int) bool { return v.hasEffect(a0) }

// ResourceQueueFreeCachedResources calls ResourceQueue.freeCachedResources for gfx's internal/.
func ResourceQueueFreeCachedResources(v *ResourceQueue) { v.freeCachedResources() }

// ResourceQueueOps reads ResourceQueue.ops for gfx's internal/.
func ResourceQueueOps(v *ResourceQueue) []Op { return v.ops }

// ResourceQueueReleaseCachedResource calls ResourceQueue.releaseCachedResource for gfx's internal/.
func ResourceQueueReleaseCachedResource(v *ResourceQueue, a0 string) { v.releaseCachedResource(a0) }

// ResourceQueueReset calls ResourceQueue.reset for gfx's internal/.
func ResourceQueueReset(v *ResourceQueue) { v.reset() }

// TargetKindOf reads TargetDescr.kind for gfx's internal/.
func TargetKindOf(v *TargetDescr) TargetKind { return v.kind }

// TargetLayer reads TargetDescr.layer for gfx's internal/.
func TargetLayer(v *TargetDescr) int { return v.layer }

// TargetMip reads TargetDescr.mip for gfx's internal/.
func TargetMip(v *TargetDescr) int { return v.mip }

// TargetTextureOf reads TargetDescr.texture for gfx's internal/.
func TargetTextureOf(v *TargetDescr) TextureID { return v.texture }

// TextureParamsFormat reads TextureDescrParams.format for gfx's internal/. The
// texture loader lives in internal/, because it needs a gfx.Backend, and the
// bake parameters a Load is handed keep their fields unexported.
func TextureParamsFormat(v TextureDescrParams) TextureFormat { return v.format }

// VertexAttrOffset reads VertexAttr.offset for gfx's internal/.
func VertexAttrOffset(v *VertexAttr) int { return v.offset }

// VertexAttrTyp reads VertexAttr.typ for gfx's internal/.
func VertexAttrTyp(v *VertexAttr) VertexType { return v.typ }
