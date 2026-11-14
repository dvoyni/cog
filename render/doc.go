// Package render is the immediate-mode rendering API between gfx and the
// plugins that draw.
//
// An application lists render's plugin after gfx and platform, and each
// plugin that draws after render. Such a plugin subscribes to FrameEvent,
// schedules its recording tasks after the event's Ready, and joins them into
// the out dependency it returns, so gfx flushes the frame only after them:
//
//	func recordScene(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
//		return event, ex.Join(in, ex.Schedule(drawScene, event, event.Ready).Handle())
//	}
//
// The recording task reaches the frame through its Frame parameter, and
// records its passes into a command list at an order of its own:
//
//	func drawScene(k *kernel.Kernel, event render.FrameEvent, frame render.Frame) kernel.NoResult {
//		list := frame.BeginCommandList("scene", sceneOrder)
//		pass := list.BeginPass(render.PassDescriptor{
//			Label:        "scene",
//			ColorTargets: []render.ColorTarget{{Window: window, Clear: true}},
//		})
//		pass.End()
//		list.End()
//		return kernel.NoResult{}
//	}
//
// A mistake render finds at run time is a Report, logged once and carried
// past; its kind names it, so a test built on render/rendertest fails on a
// report it did not expect.
package render
