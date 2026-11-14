package rendertest

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/render"
)

// makeInspection answers frame, as gfx's recording adapter saw it, in
// render's terms: each command buffer a command list, in the order gfx
// submitted them, which is their order, with its passes, the views they draw
// into named by what viewTargets says they are.
func makeInspection(frame recording.Frame, viewTargets map[gfx.TextureView]viewTarget) render.Inspection {
	var inspection render.Inspection
	for _, submission := range frame.Submissions {
		for _, buffer := range submission.Buffers {
			var list render.InspectedCommandList
			for _, command := range buffer.Commands {
				if command.Kind == recording.CommandKindBeginRenderPass {
					list.Passes = append(list.Passes, makeInspectedPass(command.Arguments.(gfx.RenderPassDescriptor), viewTargets))
				}
			}
			inspection.CommandLists = append(inspection.CommandLists, list)
		}
	}
	return inspection
}

// makeInspectedPass answers the pass descriptor declares, in render's terms.
func makeInspectedPass(descriptor gfx.RenderPassDescriptor, viewTargets map[gfx.TextureView]viewTarget) render.InspectedPass {
	pass := render.InspectedPass{Label: descriptor.Label}
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
