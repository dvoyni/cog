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
// A camera's Pivot of its viewport shows its Position, turned by its
// Rotation and scaled by its Zoom; its logical Size, the area of the world it
// is meant to show, is fitted into the viewport by its Fit, expanding,
// inscribing with bars, overlapping or stretching it. A PixelPerfect camera,
// for pixel art, one art pixel a world unit, floors its scale to a whole
// number of pixels a unit, moves each entry whole onto the world-unit grid
// and places its view on whole pixels. A camera of a SampleCount of 4
// smooths its entries' hard edges: it draws them into a multisampled texture of its
// viewport, resolves that, and draws it over the viewport, which leaves the
// rest of its target as it was. A game keeps the cameras it set to
// convert a click through, reading the Targets resource, each target as it
// was last drawn:
//
//	world, inside := camera.ScreenToWorld(click, targets.Get())
//
// canvas begins one command list for each distinct camera order, a camera's
// pass in it for each camera of that order, those into textures first, its
// entries an instanced draw for each batch, a run of entries in sort order
// of one 2D material, one atlas, one filter and one clip, each image and
// tint premultiplied and blended over what the
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
// A Sprite draws an Image, named by ImageFromFile with its options, such as its
// pivot, or a Frame of its sheet, through its transform about that pivot, at its size, its image's
// in texels when zero, tinted, cropped where it lies and read through its
// filter. ImageFromFile loads nothing: the first frame drawing the image loads
// it and packs it into the sprite atlas of its colour space, drawn in that
// frame, and a file render cannot load draws magenta:
//
//	hero := canvas.ImageFromFile("hero.png", canvas.ImageOptions{Pivot: canvas.PivotBottom})
//	list.DrawSprite(canvas.Sprite{Image: hero, Transform: m.Translation2x3(x, y), Layer: layerUnits, Order: canvas.OrderOf(y)})
//
// A sheet's frames, and any rectangle of a file, are packed as cells of
// their own, so filtering and mips never mix two of them, and a file is
// decoded once for all its rectangles; an image that tiles along an axis
// has its gutter wrapped from its opposite edge along it:
//
//	terrain := canvas.ImageFromFile("tiles.png", canvas.ImageOptions{Sheet: canvas.Grid{Columns: 4, Rows: 4}, SourceRect: m.Recti{Width: 64, Height: 64}})
//	list.DrawSprite(canvas.Sprite{Image: terrain, Frame: tile, Transform: m.Translation2x3(x, y)})
//	water := canvas.ImageFromFile("water.png", canvas.ImageOptions{Wrap: canvas.WrapBoth})
//
// An atlas is one 2D-array texture whose layers are pages of
// Config.AtlasPageSize, each image a cell of its own, premultiplied, its
// gutter continuing its edges, or wrapped along the axes it tiles, on a
// atlas grid Config.MipLevels sets, so linear
// filtering never reaches a neighbour at any mip level, a page's mips made
// again each frame an image is packed into it, unless
// Config.DoNotGenerateMipMaps is set. A full atlas doubles its pages, to
// Config.AtlasLayerCeiling, where it evicts the images drawn least lately,
// never one drawn this frame; an image wider or taller than
// Config.OwnTextureSize, or one no page holds, is drawn from a texture of
// its own, exactly its size with no gutter, its sampler clamping at its
// edge. Nothing is refused for good.
// Sprites of any images of one atlas batch together while their filter is
// one, and shapes with them: shapes, and the zero Image, draw the sRGB
// sprite atlas's white texel, which takes no filter.
//
// An entry's Clip is a rectangle of the world it draws nothing outside,
// seen through each camera as the entry is, a scissor of its bounding box
// in the target's pixels, so a scrolled log stays clipped under every
// camera, zoom and DPI; a change of clip breaks a batch. Each camera culls
// the entries whose bounds miss its visible rectangle, or their clip's part
// of it, so an entry no camera sees costs only its recording:
//
//	list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: line, Tint: c, Clip: logBox})
//
// A task loads an image ahead of its first draw, unloads a level's images
// and sizes an image before it is drawn through its Assets parameter:
//
//	assets.Preload(hero)
//	assets.Unload("level1/")
//	size, ok := assets.ImageSize(hero)
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
