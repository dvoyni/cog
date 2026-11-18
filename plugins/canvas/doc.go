// Package canvas is the plugin on render that draws 2D: the entries a frame
// records, sorted and drawn through 2D materials.
//
// An application lists canvas's plugin after render and platform, and each
// plugin that records into canvas after canvas. Such a plugin subscribes to
// FrameEvent, schedules its recording tasks after the event's Ready, and
// joins them into the out dependency it returns, so canvas draws the frame
// only after them:
//
//	func recordUnits(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.FrameEvent) (canvas.FrameEvent, kernel.JobHandle) {
//		return event, ex.Join(in, ex.Schedule(drawUnits, event, event.Ready).Handle())
//	}
//
// The recording task records its entries through its DrawList parameter, a
// draw list of its own, so recording tasks run together:
//
//	func drawUnits(k *kernel.Kernel, event canvas.FrameEvent, list canvas.DrawList) kernel.NoResult {
//		list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{Width: 32, Height: 32}, Tint: m.Color{R: 1, A: 1}})
//		return kernel.NoResult{}
//	}
//
// canvas merges the frame's draw lists in registration order once every
// recording task has run, sorts their entries, draws them, and forgets them.
// Entries are drawn by layer, then by order, then by their draw list's
// registration order, then in the order recorded, unless their layer is in
// Config.RegroupedLayers, which may regroup entries of equal keys by batch.
// OrderOf maps a y position onto an order, for y-sorting:
//
//	list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: r, Tint: c, Layer: layerUnits, Order: canvas.OrderOf(position.Y)})
//
// A frame is drawn through the default camera: into the main window, the
// first window opened of those still open, cleared to opaque black, the
// world's origin at its centre, one world unit to a point and y down. It is
// one command list at order 0 of one pass, its entries an instanced draw
// for each batch, a run of entries in sort order of one 2D material, each
// tint premultiplied and blended over what the window holds, its colour
// encoded as sRGB. A frame that recorded no entry, or with no main window
// open, begins no list.
//
// A tool asks for an inspection of the next frame through its Assets
// parameter, and reads it in a task off InspectedEvent: each camera the frame
// was seen through, its batches and their entries, with render's own
// inspection of the frame beside it. Every other frame pays one check:
//
//	func inspect(k *kernel.Kernel, event canvas.InspectedEvent, assets canvas.Assets) kernel.NoResult {
//		inspection, err := assets.ReadInspection(event)
//		...
//	}
//
// A mistake canvas finds at run time is a Report, logged once for its
// subject through the kernel's LogErrOnce and carried past.
package canvas
