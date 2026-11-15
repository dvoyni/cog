package render

import (
	"fmt"
	"slices"

	"github.com/dvoyni/cog/gfx"
)

// pipelineKey is what a render pipeline is shared by: the variant and the
// vertex and fragment entry points its material names, an empty fragment
// entry point meaning no fragment stage; the material's render state, in
// the form makeKeyState gives every state that draws the same; the numbers
// of its mesh's vertex layouts and of its instance buffers' in the vertex
// layout interner, zero for none, as a draw with no mesh has; the formats
// and sample count of the pass drawn into; and the topology, with the
// format of its indices when it is a strip drawn indexed.
type pipelineKey struct {
	shader                               Shader
	vertexEntryPoint, fragmentEntryPoint string
	state                                State
	vertexLayout, instanceLayout         uint32
	formats                              passFormat
	topology                             PrimitiveTopology
	stripIndexFormat                     IndexFormat
}

// String names k's pipeline in reports.
func (k pipelineKey) String() string {
	return fmt.Sprintf("the pipeline of shader %v, vertex entry point %q and fragment entry point %q", k.shader, k.vertexEntryPoint, k.fragmentEntryPoint)
}

// findShader answers k's shader.
func (k pipelineKey) findShader() Shader {
	return k.shader
}

// makePipeline makes k's pipeline from shader, the slot of k's shader, with
// the layout of the shader's bindings and k's vertex layouts from table, its
// mesh's before its instance buffers'. A shader with no fragment stage drawn
// into colour targets is refused; gfx's refusal, such as ErrVertexMismatch,
// answers its error.
func (k pipelineKey) makePipeline(table *assetTable, device gfx.Device, shader shaderSlot) (gfx.RenderPipeline, error) {
	descriptor := gfx.RenderPipelineDescriptor{
		Label:  "render: a material's pipeline",
		Layout: shader.layout.layout,
		Vertex: gfx.VertexState{
			Module:     shader.module,
			EntryPoint: k.vertexEntryPoint,
			Buffers:    slices.Concat(table.vertexLayouts.findGfxLayouts(k.vertexLayout), table.vertexLayouts.findGfxLayouts(k.instanceLayout)),
		},
		Primitive: gfx.PrimitiveState{
			Topology:         gfx.PrimitiveTopology(k.topology),
			StripIndexFormat: gfx.IndexFormat(k.stripIndexFormat),
			FrontFace:        gfx.FrontFace(k.state.FrontFace),
			CullMode:         gfx.CullMode(k.state.CullMode),
		},
		DepthStencil: k.state.makeDepthStencilState(k.formats.depthStencilFormat),
		Multisample:  gfx.MultisampleState{Count: k.formats.sampleCount, Mask: ^uint64(0), AlphaToCoverageEnabled: k.state.AlphaToCoverage},
	}
	switch {
	case k.fragmentEntryPoint != "":
		descriptor.Fragment = gfx.FragmentState{Module: shader.module, EntryPoint: k.fragmentEntryPoint, Targets: k.state.makeColorTargetStates(k.formats.colorFormats[:k.formats.colorCount])}
	case k.formats.colorCount > 0:
		return gfx.RenderPipeline{}, fmt.Errorf("the shader has no fragment stage, and the pass %d colour targets, which only a pass with none may have", k.formats.colorCount)
	}
	return device.CreateRenderPipeline(descriptor)
}

// passFormat is what a pass's targets fix of the pipelines drawn in it: the
// format of each colour target, the first colorCount of colorFormats, that
// of its depth-stencil target, none for none, and their sample count.
type passFormat struct {
	colorFormats       [maxColorTargetCount]TextureFormat
	colorCount         int
	depthStencilFormat TextureFormat
	sampleCount        uint32
}
