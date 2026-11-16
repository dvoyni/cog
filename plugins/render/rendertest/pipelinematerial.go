package rendertest

import (
	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
	"github.com/dvoyni/cog/plugins/render"
)

// pipelineMaterial is the material a render pipeline draws with, in
// render's terms but for its parameters, which each draw binds; the module
// of its vertex stage, whose bindings name them; and the slots of its
// vertex buffers read per instance, in order.
type pipelineMaterial struct {
	material      render.InspectedMaterial
	module        gfx.ShaderModule
	instanceSlots []uint32
}

// makePipelineMaterials answers the material each render pipeline the
// recording's calls made draws with, in render's terms: its shader, by the
// handle its module's label names, its entry points and its render state. A
// pipeline of a module render labelled with no shader's handle, such as its
// mip shader's, draws with the zero Shader.
func makePipelineMaterials(calls []recording.Call, shaders map[gfx.ShaderModule]render.Shader) map[gfx.RenderPipeline]pipelineMaterial {
	materials := map[gfx.RenderPipeline]pipelineMaterial{}
	for _, call := range calls {
		if arguments, ok := call.Arguments.(recording.CreateRenderPipelineArguments); ok {
			descriptor := arguments.Descriptor
			material := pipelineMaterial{
				material: render.InspectedMaterial{
					Shader:             shaders[descriptor.Vertex.Module],
					VertexEntryPoint:   descriptor.Vertex.EntryPoint,
					FragmentEntryPoint: descriptor.Fragment.EntryPoint,
					State:              makeState(descriptor),
				},
				module: descriptor.Vertex.Module,
			}
			for slot, layout := range descriptor.Vertex.Buffers {
				if layout.StepMode == gfx.VertexStepModeInstance {
					material.instanceSlots = append(material.instanceSlots, uint32(slot))
				}
			}
			materials[arguments.Pipeline] = material
		}
	}
	return materials
}

// makeState answers the render state descriptor draws with, each value
// its field's default spelt zero.
func makeState(descriptor gfx.RenderPipelineDescriptor) render.State {
	depthStencil := descriptor.DepthStencil
	state := render.State{
		CullMode:        render.CullMode(descriptor.Primitive.CullMode),
		FrontFace:       render.FrontFace(descriptor.Primitive.FrontFace),
		AlphaToCoverage: descriptor.Multisample.AlphaToCoverageEnabled,
	}
	for index, target := range descriptor.Fragment.Targets {
		state.ColorTargets[index] = render.ColorTargetState{
			Blend:             makeBlendState(target.Blend),
			UnwrittenChannels: render.ColorWriteFlagAll &^ render.ColorWriteFlags(target.WriteMask),
		}
	}
	if depthStencil.Format == gfx.TextureFormatUndefined {
		return state
	}
	state.Depth = render.DepthState{
		Compare:        makeCompareFunction(depthStencil.DepthCompare),
		Write:          depthStencil.DepthWriteEnabled,
		Bias:           depthStencil.DepthBias,
		BiasSlopeScale: depthStencil.DepthBiasSlopeScale,
		BiasClamp:      depthStencil.DepthBiasClamp,
	}
	state.Stencil = render.StencilState{
		Front:         makeStencilFaceState(depthStencil.StencilFront),
		Back:          makeStencilFaceState(depthStencil.StencilBack),
		UnreadBits:    ^depthStencil.StencilReadMask,
		UnwrittenBits: ^depthStencil.StencilWriteMask,
	}
	return state
}

// makeBlendState answers blend in render's terms, each factor and operation
// that is its component's default spelt zero, so blending that replaces
// what a target holds is the zero BlendState.
func makeBlendState(blend gfx.BlendState) render.BlendState {
	makeComponent := func(component gfx.BlendComponent) render.BlendComponent {
		return render.BlendComponent{
			SourceFactor:      render.BlendFactor(spellDefaultZero(component.SrcFactor, gfx.BlendFactorOne)),
			DestinationFactor: render.BlendFactor(spellDefaultZero(component.DstFactor, gfx.BlendFactorZero)),
			Operation:         render.BlendOperation(spellDefaultZero(component.Operation, gfx.BlendOperationAdd)),
		}
	}
	return render.BlendState{Color: makeComponent(blend.Color), Alpha: makeComponent(blend.Alpha)}
}

// makeStencilFaceState answers face in render's terms, its defaults spelt
// zero.
func makeStencilFaceState(face gfx.StencilFaceState) render.StencilFaceState {
	return render.StencilFaceState{
		Compare:            makeCompareFunction(face.Compare),
		FailOperation:      render.StencilOperation(spellDefaultZero(face.FailOp, gfx.StencilOperationKeep)),
		DepthFailOperation: render.StencilOperation(spellDefaultZero(face.DepthFailOp, gfx.StencilOperationKeep)),
		PassOperation:      render.StencilOperation(spellDefaultZero(face.PassOp, gfx.StencilOperationKeep)),
	}
}

// makeCompareFunction answers function in render's terms, passing always
// spelt zero.
func makeCompareFunction(function gfx.CompareFunction) render.CompareFunction {
	return render.CompareFunction(spellDefaultZero(function, gfx.CompareFunctionAlways))
}

// spellDefaultZero answers value, or zero when it is defaultValue.
func spellDefaultZero[T comparable](value, defaultValue T) T {
	var zero T
	if value == defaultValue {
		return zero
	}
	return value
}
