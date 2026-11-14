package rendertest

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
)

// makeInspection answers frame, as gfx's recording adapter saw it, in
// render's terms: each command buffer a command list, in the order gfx
// submitted them, which is their order, with its passes, the framebuffer
// views they draw into named by the windows framebufferWindows says they are
// of.
func makeInspection(frame recording.Frame, framebufferWindows map[gfx.TextureView]platform.Window) render.Inspection {
	var inspection render.Inspection
	for _, submission := range frame.Submissions {
		for _, buffer := range submission.Buffers {
			var list render.InspectedCommandList
			for _, command := range buffer.Commands {
				if command.Kind == recording.CommandKindBeginRenderPass {
					list.Passes = append(list.Passes, makeInspectedPass(command.Arguments.(gfx.RenderPassDescriptor), framebufferWindows))
				}
			}
			inspection.CommandLists = append(inspection.CommandLists, list)
		}
	}
	return inspection
}

// makeInspectedPass answers the pass descriptor declares, in render's terms.
func makeInspectedPass(descriptor gfx.RenderPassDescriptor, framebufferWindows map[gfx.TextureView]platform.Window) render.InspectedPass {
	pass := render.InspectedPass{Label: descriptor.Label}
	for _, attachment := range descriptor.ColorAttachments {
		pass.ColorTargets = append(pass.ColorTargets, render.ColorTarget{
			Window:     framebufferWindows[attachment.View],
			Clear:      attachment.LoadOp == gfx.LoadOpClear,
			ClearColor: render.Color(attachment.ClearValue),
			Discard:    attachment.StoreOp == gfx.StoreOpDiscard,
		})
	}
	return pass
}

// makeFramebufferWindows answers the window of each framebuffer view the
// recording's acquires were given.
func makeFramebufferWindows(calls []recording.Call) map[gfx.TextureView]platform.Window {
	framebufferWindows := map[gfx.TextureView]platform.Window{}
	for _, call := range calls {
		if call.Kind == recording.CallKindAcquireFramebuffer {
			arguments := call.Arguments.(recording.AcquireFramebufferArguments)
			framebufferWindows[arguments.View] = arguments.Window
		}
	}
	return framebufferWindows
}
