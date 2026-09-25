package internal

import (
	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// PipelineDesc describes a render pipeline to create. Bind group layouts are
// derived by the backend from the shader's reflection; the vertex layout is
// supplied by the mesh via Stride and Attributes.
type PipelineDesc struct {
	Shader      types.ShaderID
	Topology    types.PrimitiveTopology
	State       types.MaterialState
	ColorFormat descriptors.TextureFormat
	DepthFormat descriptors.TextureFormat
	// NoColorTarget builds a pipeline with no colour target at all, which is
	// what a draw inside a depth-only pass needs. A render pass declares its
	// attachments and a pipeline declares its targets, and the two are
	// validated against each other at setPipeline time: a pipeline with one
	// colour target set into a pass with none is rejected, and gfx drops that
	// error along with the rest of the command buffer.
	//
	// It is a bool rather than a FormatNone member of TextureFormat because
	// every member of that enum is a real texel layout and Resolve() is defined
	// over all of them - a non-format in it would put a case into every switch
	// that reads one.
	//
	// A backend that honours this builds no fragment stage, so a depth-only
	// shader may declare no fs_main at all.
	NoColorTarget bool
	// NoDepthTarget is NoColorTarget's depth twin: it builds a pipeline with no
	// depth state, which is what a draw inside a DepthNone pass needs. Such a
	// pass declares no depth attachment, and a pipeline that declares one
	// anyway is rejected at setPipeline for the same reason and with the same
	// silent loss of the frame. DepthFormat is not read when it is set.
	NoDepthTarget bool
	Stride        int
	Attributes    []descriptors.VertexAttribute
	// IndexWidth is the width a strip topology cuts on. WebGPU requires a
	// pipeline to declare that format before an indexed strip draw is legal and
	// forbids it on every other topology, so a backend reads this only when
	// Topology is a strip: a list pipeline never sees the index buffer at all.
	IndexWidth descriptors.IndexWidth
	Label      string
}

// DefaultLimits returns the WebGPU spec floor every browser guarantees. It is
// the comparison target on purpose: a desktop adapter reports its hardware
// limits, where 200 storage buffers is ordinary, so checking a shader against
// the device it happens to run on passes builds that cannot run in a browser.
func DefaultLimits() types.Limits { return defaultLimits }

var defaultLimits = types.Limits{
	MaxBindGroups:                   4,
	MaxStorageBuffersPerShaderStage: 8,
	MaxStorageBufferBindingSize:     128 << 20,
	MaxUniformBuffersPerShaderStage: 12,
	MaxUniformBufferBindingSize:     64 << 10,
	MaxBufferSize:                   256 << 20,
}
