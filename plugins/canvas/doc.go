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
// A frame is drawn through the cameras its recording tasks set, each a plain
// value for that frame alone, which draws the entries of its layers into its
// target, a window or a texture, over its viewport, in fractions of the
// target, cleared there alone when it clears, the world's origin at the
// viewport's centre, one world unit to a point of a window or a pixel of a
// texture, and y down:
//
//	list.SetCamera(canvas.Camera{Layers: canvas.MakeLayers(canvas.LayerRange{From: layerGround, To: layerUnits + 1}), Viewport: m.Rect{Width: 0.5, Height: 1}, Clear: true, ClearColor: sky})
//
// canvas begins one command list for each distinct camera order, a camera's
// pass in it for each camera of that order, those into textures first, its
// entries an instanced draw for each batch, a run of entries in sort order
// of one 2D material, each tint premultiplied and blended over what the
// target holds, its colour encoded as sRGB where the target's format does
// not encode it. A frame that sets no camera is seen through the default
// one, which draws only a frame that recorded entries: every layer, into the
// whole main window, the first window opened of those still open, cleared
// to opaque black, at order 0.
//
// A Shape fills or strokes a rectangle, or draws a line. A stroke lies
// inside its rectangle as four fills that do not overlap, so a translucent
// one does not darken its corners, and a line is centred on its segment. A
// zero Width is a hairline one target pixel wide at any zoom, and Radius
// rounds a fill's or a stroke's corners:
//
//	list.DrawShape(canvas.Shape{Kind: canvas.Stroke, Rect: r, Width: 2, Radius: 6, Tint: c})
//	list.DrawShape(canvas.Shape{Kind: canvas.Line, From: a, To: b, Tint: c})
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
