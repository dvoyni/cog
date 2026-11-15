package render

// ReportKind names what kind of mistake a Report is, for a test to name the
// reports it expects.
type ReportKind uint8

const (
	// ReportKindOrderTaken is a command list begun at an order another list
	// of the frame has.
	ReportKindOrderTaken ReportKind = iota + 1
	// ReportKindOrderReserved is a command list begun at render's own order,
	// math.MinInt32.
	ReportKindOrderReserved
	// ReportKindListOutsideFrame is a command list begun while no frame is
	// recording.
	ReportKindListOutsideFrame
	// ReportKindPassTargetsRefused is a pass refused for its colour targets:
	// none, more than the device allows, one naming no window, or targets of
	// different sizes.
	ReportKindPassTargetsRefused
	// ReportKindLoadFailed is a loaded asset whose file could not be read or
	// decoded, such as a texture that draws magenta in its place.
	ReportKindLoadFailed
	// ReportKindReleaseRefused is a release of an asset no acquire holds: a
	// temporary, one released as often as it was acquired, or the zero
	// handle.
	ReportKindReleaseRefused
	// ReportKindWriteRefused is a write of a buffer or texture that does not
	// fit it: into an asset render does not hold, past its end or its
	// level's edges, unaligned, or of data not as long as the region.
	ReportKindWriteRefused
	// ReportKindShaderRefused is a shader refused for its bindings: one in
	// @group(3), which render reserves, or more than the device's limits
	// allow, such as more than 8 uniform blocks at dynamic offsets.
	ReportKindShaderRefused
	// ReportKindMeshRefused is a mesh refused when made: more vertex buffers
	// than the device allows, a vertex buffer, stride or attribute
	// misaligned, an index buffer of no index format or misaligned, or a
	// topology gfx has none of.
	ReportKindMeshRefused
	// ReportKindMaterialRefused is a material refused when made for its entry
	// points: a name its shader has no function of that stage of, or none
	// named where the shader has several.
	ReportKindMaterialRefused
	// ReportKindPipelineFailed is a pipeline a draw needs that could not be
	// made, such as one whose mesh's vertex buffers do not feed its shader's
	// inputs, or one with no fragment stage drawn into colour targets.
	ReportKindPipelineFailed
	// ReportKindDrawSkipped is a draw skipped for what it names: a mesh or
	// material render does not hold, no material set, a shader that failed
	// or was freed, a buffer that failed, was freed or is too small for its
	// range, or a storage binding left unbound that a function the draw runs
	// reads.
	ReportKindDrawSkipped
	// ReportKindParameterRefused is a value a parameter of a material cannot
	// bind, whose binding draws its default: of the wrong kind, a struct of
	// the wrong layout, a texture of the wrong view dimension, sample type
	// or aspect, or naming mips or layers it lacks, or overlapping a target
	// of the pass it is drawn in, or a buffer too small; or a value set on a
	// material render does not hold.
	ReportKindParameterRefused
	// ReportKindInstanceBufferRefused is an instance buffer refused when
	// set: at a slot past the last, or at an offset or of a stride or
	// attribute misaligned as a mesh's vertex buffer would be refused.
	ReportKindInstanceBufferRefused
	// ReportKindPassStateRefused is a viewport a pass cannot take: its size
	// negative or past the device's largest texture, its position past the
	// range WebGPU allows, or its depth range outside 0 to 1 or reversed.
	ReportKindPassStateRefused
	// ReportKindCopyRefused is a copy or buffer clear that does not fit what
	// it names: an asset render does not hold, a buffer without the copy
	// source usage, an offset or size misaligned, a range or region past
	// its buffer's end or its level's edges, or textures of different
	// formats.
	ReportKindCopyRefused
	// ReportKindDispatchSkipped is a dispatch skipped for what it names: no
	// shader selected, one render does not hold, failed or was freed, or a
	// storage binding left unbound that its compute function reads.
	ReportKindDispatchSkipped
)

// Report is a mistake render finds at run time, such as a refused shader, a
// duplicate order or a draw skipped for a missing binding: logged once through
// the kernel's LogErr, and carried past. Its kind names the mistake, and its
// Err says what went wrong and where.
type Report struct {
	Kind ReportKind
	Err  error
}

// Error answers Err's message.
func (r Report) Error() string { return r.Err.Error() }

// Unwrap answers Err.
func (r Report) Unwrap() error { return r.Err }
