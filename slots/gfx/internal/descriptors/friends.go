package descriptors

import (
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// BufferBytes reads BufferDescr.bytes for gfx's internal/.
func BufferBytes(v *BufferDescr) assets.Blob { return v.bytes }

// BufferSource reads BufferDescr.source for gfx's internal/.
func BufferSource(v *BufferDescr) bufferSource { return v.source }

// DepthKindOf reads DepthDescr.kind for gfx's internal/.
func DepthKindOf(v *DepthDescr) DepthKind { return v.kind }

// DepthTexture reads DepthDescr.texture for gfx's internal/.
func DepthTexture(v *DepthDescr) types.TextureID { return v.texture }

// MaterialShapeState reads the shape state OpQueue.FrameMaterial took of a
// recorded material's param names, and whether it has one.
func MaterialShapeState(v *MaterialDescr) (uint64, bool) {
	if v.recorded.Queue == nil {
		return 0, false
	}
	return v.recorded.Shape, true
}

// MaterialRecording reads MaterialDescr.recorded for gfx's internal/.
func MaterialRecording(v *MaterialDescr) FrameRecording { return v.recorded }

// SetMaterialRecording writes MaterialDescr.recorded for OpQueue.FrameMaterial.
func SetMaterialRecording(v *MaterialDescr, recording FrameRecording) { v.recorded = recording }

// SetMaterialParams writes MaterialDescr.params, which is how OpQueue points a
// recorded material at its window of the frame's parameter arena.
func SetMaterialParams(v *MaterialDescr, params []ParameterDescr) { v.params = params }

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

// ParameterBuffer reads ParameterDescr.buffer for gfx's internal/.
func ParameterBuffer(v *ParameterDescr) BufferDescr { return v.buffer }

// ParameterBufferOffset reads ParameterDescr.bufferOffset for gfx's internal/.
func ParameterBufferOffset(v *ParameterDescr) int { return v.bufferOffset }

// ParameterBufferRef points at ParameterDescr.buffer for gfx's internal/.
func ParameterBufferRef(v *ParameterDescr) *BufferDescr { return &v.buffer }

// ParameterBufferSize reads ParameterDescr.bufferSize for gfx's internal/.
func ParameterBufferSize(v *ParameterDescr) int { return v.bufferSize }

// ParameterBytes reads a bytes parameter's value for gfx's internal/. It
// aliases the descriptor and must not be written to.
func ParameterBytes(v *ParameterDescr) []byte { return v.bytes() }

// ParameterForm reads ParameterDescr.form for gfx's internal/.
func ParameterForm(v *ParameterDescr) ParamForm { return v.form }

// ParameterKind reads ParameterDescr.kind for gfx's internal/.
func ParameterKind(v *ParameterDescr) ParamKind { return v.kind }

// ParameterKindName names a parameter the way an inspection view spells its
// kind: a bytes parameter by the constructor that built it, since an agent
// reading a snapshot knows a value as a color or a mat4 rather than as bytes.
func ParameterKindName(v *ParameterDescr) string {
	if v.kind == ParamBytes {
		return v.form.String()
	}
	return v.kind.String()
}

// ParameterSampler reads ParameterDescr.sampler for gfx's internal/.
func ParameterSampler(v *ParameterDescr) types.SamplerDesc { return v.sampler }

// ParameterTexture reads ParameterDescr.texture for gfx's internal/.
func ParameterTexture(v *ParameterDescr) TextureDescr { return v.texture }

// ParameterTextureRef points at ParameterDescr.texture for gfx's internal/.
func ParameterTextureRef(v *ParameterDescr) *TextureDescr { return &v.texture }

// PassHasEffect calls PassDescr.hasEffect for gfx's internal/.
func PassHasEffect(v *PassDescr, a0 int) bool { return v.hasEffect(a0) }

// TargetKindOf reads TargetDescr.kind for gfx's internal/.
func TargetKindOf(v *TargetDescr) TargetKind { return v.kind }

// TargetLayer reads TargetDescr.layer for gfx's internal/.
func TargetLayer(v *TargetDescr) int { return v.layer }

// TargetMip reads TargetDescr.mip for gfx's internal/.
func TargetMip(v *TargetDescr) int { return v.mip }

// TargetTextureOf reads TargetDescr.texture for gfx's internal/.
func TargetTextureOf(v *TargetDescr) types.TextureID { return v.texture }

// TextureParamsFormat reads TextureDescrParams.format for gfx's internal/. The
// texture loader lives in internal/, because it needs a gfx.Backend, and the
// bake parameters a Load is handed keep their fields unexported.
func TextureParamsFormat(v TextureDescrParams) TextureFormat { return v.format }

// VertexAttrOffset reads VertexAttr.offset for gfx's internal/.
func VertexAttrOffset(v *VertexAttr) int { return v.offset }

// VertexAttrTyp reads VertexAttr.typ for gfx's internal/.
func VertexAttrTyp(v *VertexAttr) VertexType { return v.typ }

// BufferCopyData reads BufferDescr.copyData for gfx's internal/.
func BufferCopyData(v *BufferDescr) bool { return v.copyData }

// TextureCopyData reads the texture's copyData for gfx's internal/.
func TextureCopyData(v *TextureDescr) bool { return v.Params.copyData }

// WithMeshBuffers returns mesh with its layout, vertices and indices replaced,
// which is how OpQueue points a recorded draw at its baked copies.
func WithMeshBuffers(mesh MeshDescr, layout []VertexAttr, vertices, indices BufferDescr) MeshDescr {
	mesh.layout, mesh.vertices, mesh.indices = layout, vertices, indices
	return mesh
}

// WithParameterBuffer returns param with its buffer replaced by a baked one.
func WithParameterBuffer(param ParameterDescr, buffer BufferDescr) ParameterDescr {
	param.buffer = buffer
	return param
}

// WithParameterTexture returns param with its texture replaced by a baked one.
func WithParameterTexture(param ParameterDescr, texture TextureDescr) ParameterDescr {
	param.texture = texture
	return param
}

// DrawParamsOf is the handle of set id, for gfx's internal/.
func DrawParamsOf(id uint32) DrawParams { return DrawParams{id: id} }

// DrawParamsIndex reads DrawParams.id for gfx's internal/.
func DrawParamsIndex(set DrawParams) uint32 { return set.id }
