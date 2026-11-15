package rendertest

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/render"
)

// frameReader reads the frames gfx's recording adapter recorded back in
// render's terms, from what the recording's calls made: what each view drawn
// into is, the material each pipeline draws with, and what each bind group
// binds.
type frameReader struct {
	viewTargets map[gfx.TextureView]viewTarget
	materials   map[gfx.RenderPipeline]pipelineMaterial
	bindGroups  *bindGroupReader
	// submission is the index of the next submission read, among every
	// frame's.
	submission int
}

// newFrameReader answers the reader of the frames recorded with calls, its
// shaders' modules answering their bindings' reflection with
// shaderBindings.
func newFrameReader(calls []recording.Call, shaderBindings map[gfx.ShaderModule][]gfx.ShaderBinding) *frameReader {
	viewTargets := makeViewTargets(calls)
	return &frameReader{
		viewTargets: viewTargets,
		materials:   makePipelineMaterials(calls),
		bindGroups:  newBindGroupReader(calls, viewTargets, shaderBindings),
	}
}

// makeInspection answers frame, the next frame recorded, in render's terms:
// each command buffer a command list, in the order gfx submitted them, which
// is their order, with its passes.
func (r *frameReader) makeInspection(frame recording.Frame) render.Inspection {
	var inspection render.Inspection
	for _, submission := range frame.Submissions {
		for _, buffer := range submission.Buffers {
			var list render.InspectedCommandList
			for _, command := range buffer.Commands {
				if command.Kind == recording.CommandKindBeginRenderPass {
					list.Passes = append(list.Passes, r.makeInspectedPass(command))
				}
			}
			inspection.CommandLists = append(inspection.CommandLists, list)
		}
		r.submission++
	}
	return inspection
}

// makeInspectedPass answers the pass command begins, in render's terms, the
// views it draws into named by what they are, with its draws.
func (r *frameReader) makeInspectedPass(command recording.Command) render.InspectedPass {
	descriptor := command.Arguments.(gfx.RenderPassDescriptor)
	pass := render.InspectedPass{Label: descriptor.Label, Draws: r.makeInspectedDraws(command.Commands)}
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
