package rendertest

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/internal/handle"
)

// frameReader reads the frames gfx's recording adapter recorded back in
// render's terms, from what the recording's calls made: what each view drawn
// into is, the material each pipeline draws with, what each compute
// pipeline runs, what each bind group binds, and the handle of each texture
// and buffer render made.
type frameReader struct {
	viewTargets      map[gfx.TextureView]viewTarget
	materials        map[gfx.RenderPipeline]pipelineMaterial
	computePipelines map[gfx.ComputePipeline]computePipelineShader
	bindGroups       *bindGroupReader
	textures         map[gfx.Texture]render.Texture
	buffers          map[gfx.Buffer]render.Buffer
	// submission is the index of the next submission read, among every
	// frame's.
	submission int
}

// newFrameReader answers the reader of the frames recorded with calls, the
// handles of render's textures, buffers and shaders read from the labels of
// the objects behind them, a module render labelled with no shader's
// handle, such as its mip shader's, the zero Shader's; its
// shaders' modules answering their bindings' reflection with
// shaderBindings.
func newFrameReader(calls []recording.Call, shaderBindings map[gfx.ShaderModule][]gfx.ShaderBinding) *frameReader {
	textures := map[gfx.Texture]render.Texture{}
	buffers := map[gfx.Buffer]render.Buffer{}
	shaders := map[gfx.ShaderModule]render.Shader{}
	for _, call := range calls {
		switch arguments := call.Arguments.(type) {
		case recording.CreateTextureArguments:
			if texture, ok := handle.ParseLabel[render.Texture](arguments.Descriptor.Label); ok {
				textures[arguments.Texture] = texture
			}
		case recording.CreateBufferArguments:
			if buffer, ok := handle.ParseLabel[render.Buffer](arguments.Descriptor.Label); ok {
				buffers[arguments.Buffer] = buffer
			}
		case recording.CreateShaderModuleArguments:
			if shader, ok := handle.ParseLabel[render.Shader](arguments.Descriptor.Label); ok {
				shaders[arguments.Module] = shader
			}
		}
	}
	viewTargets := makeViewTargets(calls, textures)
	return &frameReader{
		viewTargets:      viewTargets,
		materials:        makePipelineMaterials(calls, shaders),
		computePipelines: makeComputePipelineShaders(calls, shaders),
		bindGroups:       newBindGroupReader(calls, viewTargets, shaderBindings),
		textures:         textures,
		buffers:          buffers,
	}
}

// makeInspection answers frame, the next frame recorded, in render's terms:
// each command buffer a command list, in the order gfx submitted them, which
// is their order, with its passes, compute passes and copies.
func (r *frameReader) makeInspection(frame recording.Frame) render.Inspection {
	var inspection render.Inspection
	for _, submission := range frame.Submissions {
		for _, buffer := range submission.Buffers {
			var list render.InspectedCommandList
			// position is the position of the next pass, compute pass or copy.
			position := 0
			for _, command := range buffer.Commands {
				switch command.Kind {
				case recording.CommandKindBeginRenderPass:
					list.Passes = append(list.Passes, r.makeInspectedPass(command, position))
				case recording.CommandKindBeginComputePass:
					list.ComputePasses = append(list.ComputePasses, r.makeInspectedComputePass(command, position))
				case recording.CommandKindCopyBufferToBuffer, recording.CommandKindCopyBufferToTexture, recording.CommandKindCopyTextureToBuffer, recording.CommandKindCopyTextureToTexture, recording.CommandKindClearBuffer:
					list.Copies = append(list.Copies, r.makeInspectedCopy(command, position))
				default:
					continue
				}
				position++
			}
			inspection.CommandLists = append(inspection.CommandLists, list)
		}
		r.submission++
	}
	return inspection
}

// makeInspectedPass answers the pass command begins, at position in its
// list, in render's terms, the views it draws into named by what they are,
// with its draws.
func (r *frameReader) makeInspectedPass(command recording.Command, position int) render.InspectedPass {
	descriptor := command.Arguments.(gfx.RenderPassDescriptor)
	pass := render.InspectedPass{Position: position, Label: descriptor.Label, Draws: r.makeInspectedDraws(command.Commands)}
	for _, attachment := range descriptor.ColorAttachments {
		target := r.viewTargets[attachment.View]
		resolve := r.viewTargets[attachment.ResolveTarget]
		pass.ColorTargets = append(pass.ColorTargets, render.ColorTarget{
			Window:     target.window,
			Texture:    target.texture,
			MipLevel:   target.mipLevel,
			Layer:      target.layer,
			Clear:      attachment.LoadOp == gfx.LoadOpClear,
			ClearColor: render.Color(attachment.ClearValue),
			Discard:    attachment.StoreOp == gfx.StoreOpDiscard,
			Resolve:    render.ResolveTarget{Window: resolve.window, Texture: resolve.texture, MipLevel: resolve.mipLevel, Layer: resolve.layer},
		})
	}
	if attachment := descriptor.DepthStencilAttachment; attachment.View != (gfx.TextureView{}) {
		target := r.viewTargets[attachment.View]
		pass.DepthStencil = render.DepthStencilTarget{
			Texture:      target.texture,
			MipLevel:     target.mipLevel,
			Layer:        target.layer,
			Clear:        attachment.DepthLoadOp == gfx.LoadOpClear || attachment.StencilLoadOp == gfx.LoadOpClear,
			ClearDepth:   attachment.DepthClearValue,
			ClearStencil: attachment.StencilClearValue,
			Discard:      attachment.DepthStoreOp == gfx.StoreOpDiscard || attachment.StencilStoreOp == gfx.StoreOpDiscard,
			ReadOnly:     attachment.DepthReadOnly || attachment.StencilReadOnly,
		}
	}
	return pass
}

// makeInspectedDraws answers the draws of a pass's commands, each with the
// material the pipeline set before it draws with, and the values the bind
// groups set before it bind: its pass parameters those of @group(0), its
// material's those of @group(1) and its draw parameters those of @group(2),
// each at its dynamic offsets.
func (r *frameReader) makeInspectedDraws(commands []recording.Command) []render.InspectedDraw {
	var draws []render.InspectedDraw
	var material pipelineMaterial
	// groups are the bind groups set, by their index.
	var groups [3]recording.SetBindGroupArguments
	makeParameters := func(group uint32) []render.InspectedParameter {
		return r.bindGroups.makeParameters(material.module, group, groups[group].Group, groups[group].DynamicOffsets, r.submission)
	}
	makeDraw := func() render.InspectedDraw {
		draw := render.InspectedDraw{Material: material.material, PassParameters: makeParameters(0), DrawParameters: makeParameters(2)}
		draw.Material.Parameters = makeParameters(1)
		return draw
	}
	for _, command := range commands {
		switch command.Kind {
		case recording.CommandKindSetPipeline:
			if pipeline, ok := command.Arguments.(gfx.RenderPipeline); ok {
				material = r.materials[pipeline]
			}
		case recording.CommandKindSetBindGroup:
			if arguments := command.Arguments.(recording.SetBindGroupArguments); arguments.Index < uint32(len(groups)) {
				groups[arguments.Index] = arguments
			}
		case recording.CommandKindDraw:
			arguments := command.Arguments.(gfx.DrawArgs)
			draw := makeDraw()
			draw.VertexCount = arguments.VertexCount
			draw.InstanceCount = arguments.InstanceCount
			draw.FirstInstance = arguments.FirstInstance
			draws = append(draws, draw)
		case recording.CommandKindDrawIndexed:
			arguments := command.Arguments.(gfx.DrawIndexedArgs)
			draw := makeDraw()
			draw.IndexCount = arguments.IndexCount
			draw.InstanceCount = arguments.InstanceCount
			draw.FirstInstance = arguments.FirstInstance
			draws = append(draws, draw)
		}
	}
	return draws
}

// makeInspectedComputePass answers the compute pass command begins, at
// position in its list, in render's terms, with its dispatches, each with
// the shader and function the pipeline set before it runs, and the values
// the bind groups set before it bind to each binding of the shader, by
// group and number, at their dynamic offsets.
func (r *frameReader) makeInspectedComputePass(command recording.Command, position int) render.InspectedComputePass {
	pass := render.InspectedComputePass{Position: position, Label: command.Arguments.(gfx.ComputePassDescriptor).Label}
	var pipeline computePipelineShader
	// groups are the bind groups set, by their index.
	var groups [3]recording.SetBindGroupArguments
	for _, passCommand := range command.Commands {
		switch arguments := passCommand.Arguments.(type) {
		case gfx.ComputePipeline:
			pipeline = r.computePipelines[arguments]
		case recording.SetBindGroupArguments:
			if arguments.Index < uint32(len(groups)) {
				groups[arguments.Index] = arguments
			}
		case recording.DispatchArguments:
			dispatch := render.InspectedDispatch{Shader: pipeline.shader, EntryPoint: pipeline.entryPoint, X: arguments.X, Y: arguments.Y, Z: arguments.Z}
			for index, group := range groups {
				dispatch.Parameters = append(dispatch.Parameters, r.bindGroups.makeParameters(pipeline.module, uint32(index), group.Group, group.DynamicOffsets, r.submission)...)
			}
			pass.Dispatches = append(pass.Dispatches, dispatch)
		}
	}
	return pass
}

// makeInspectedCopy answers the copy or buffer clear command records, at
// position in its list, in render's terms, its buffers and textures by the
// handles their labels name, a buffer's range between a buffer and a
// texture by its offset alone.
func (r *frameReader) makeInspectedCopy(command recording.Command, position int) render.InspectedCopy {
	copied := render.InspectedCopy{Position: position}
	switch arguments := command.Arguments.(type) {
	case recording.CopyBufferToBufferArguments:
		copied.SourceBuffer = render.BufferRange{Buffer: r.buffers[arguments.Source], Offset: arguments.SourceOffset, Size: arguments.Size}
		copied.DestinationBuffer = render.BufferRange{Buffer: r.buffers[arguments.Destination], Offset: arguments.DestinationOffset, Size: arguments.Size}
	case recording.CopyBufferToTextureArguments:
		region := arguments.Regions[0]
		copied.SourceBuffer = render.BufferRange{Buffer: r.buffers[arguments.Source], Offset: region.BufferLayout.Offset}
		copied.DestinationTexture = r.textures[arguments.Destination]
		copied.DestinationRegion = makeTextureRegion(region.TextureBase, region.Size)
	case recording.CopyTextureToBufferArguments:
		region := arguments.Regions[0]
		copied.SourceTexture = r.textures[arguments.Source]
		copied.SourceRegion = makeTextureRegion(region.TextureBase, region.Size)
		copied.DestinationBuffer = render.BufferRange{Buffer: r.buffers[arguments.Destination], Offset: region.BufferLayout.Offset}
	case recording.CopyTextureToTextureArguments:
		region := arguments.Regions[0]
		copied.SourceTexture = r.textures[arguments.Source]
		copied.SourceRegion = makeTextureRegion(region.Source, region.Size)
		copied.DestinationTexture = r.textures[arguments.Destination]
		copied.DestinationRegion = makeTextureRegion(region.Destination, region.Size)
	case recording.ClearBufferArguments:
		copied.DestinationBuffer = render.BufferRange{Buffer: r.buffers[arguments.Buffer], Offset: arguments.Offset, Size: arguments.Size}
	}
	return copied
}

// makeTextureRegion answers the region of size at origin in render's terms.
func makeTextureRegion(origin gfx.ImageCopyTexture, size gfx.Extent3D) render.TextureRegion {
	return render.TextureRegion{MipLevel: origin.MipLevel, Layer: origin.Origin.Z, X: origin.Origin.X, Y: origin.Origin.Y, Width: size.Width, Height: size.Height}
}
