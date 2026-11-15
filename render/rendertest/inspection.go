package rendertest

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/render"
)

// makeInspection answers frame, as gfx's recording adapter saw it, in
// render's terms: each command buffer a command list, in the order gfx
// submitted them, which is their order, with its passes, the views they draw
// into named by what viewTargets says they are, and their draws, each with
// the material materials says its pipeline draws with.
func makeInspection(frame recording.Frame, viewTargets map[gfx.TextureView]viewTarget, materials map[gfx.RenderPipeline]render.InspectedMaterial) render.Inspection {
	var inspection render.Inspection
	for _, submission := range frame.Submissions {
		for _, buffer := range submission.Buffers {
			var list render.InspectedCommandList
			for _, command := range buffer.Commands {
				if command.Kind == recording.CommandKindBeginRenderPass {
					list.Passes = append(list.Passes, makeInspectedPass(command, viewTargets, materials))
				}
			}
			inspection.CommandLists = append(inspection.CommandLists, list)
		}
	}
	return inspection
}

// makeInspectedPass answers the pass command begins, in render's terms, with
// its draws.
func makeInspectedPass(command recording.Command, viewTargets map[gfx.TextureView]viewTarget, materials map[gfx.RenderPipeline]render.InspectedMaterial) render.InspectedPass {
	descriptor := command.Arguments.(gfx.RenderPassDescriptor)
	pass := render.InspectedPass{Label: descriptor.Label, Draws: makeInspectedDraws(command.Commands, materials)}
	for _, attachment := range descriptor.ColorAttachments {
		target := viewTargets[attachment.View]
		resolve := viewTargets[attachment.ResolveTarget]
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
		target := viewTargets[attachment.View]
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
// material materials says the pipeline set before it draws with.
func makeInspectedDraws(commands []recording.Command, materials map[gfx.RenderPipeline]render.InspectedMaterial) []render.InspectedDraw {
	var draws []render.InspectedDraw
	var material render.InspectedMaterial
	for _, command := range commands {
		switch command.Kind {
		case recording.CommandKindSetPipeline:
			if pipeline, ok := command.Arguments.(gfx.RenderPipeline); ok {
				material = materials[pipeline]
			}
		case recording.CommandKindDraw:
			arguments := command.Arguments.(gfx.DrawArgs)
			draws = append(draws, render.InspectedDraw{Material: material, VertexCount: arguments.VertexCount, InstanceCount: arguments.InstanceCount, FirstInstance: arguments.FirstInstance})
		case recording.CommandKindDrawIndexed:
			arguments := command.Arguments.(gfx.DrawIndexedArgs)
			draws = append(draws, render.InspectedDraw{Material: material, IndexCount: arguments.IndexCount, InstanceCount: arguments.InstanceCount, FirstInstance: arguments.FirstInstance})
		}
	}
	return draws
}
