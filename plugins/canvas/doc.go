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
// The recording task records its entries through its CommandList parameter, a
// command list of its own, so recording tasks run together:
//
//	func drawUnits(k *kernel.Kernel, event canvas.FrameEvent, list canvas.CommandList) kernel.NoResult {
//		list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{Width: 32, Height: 32}, Tint: m.Color{R: 1, A: 1}})
//		return kernel.NoResult{}
//	}
//
// canvas merges the frame's command lists in registration order once every
// recording task has run, sorts their entries, draws them, and forgets them.
// Entries are drawn by layer, then by order, then by their command list's
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
// canvas begins one of render's command lists for each distinct camera
// order, a camera's pass in it for each camera of that order, those into
// textures first, its entries an instanced draw for each batch, a run of
// entries in sort order of one 2D material, one atlas, one filter and one
// clip, each image and tint premultiplied and blended over what the target
// holds, its colour encoded as sRGB where the target's format does
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
// A sprite's DrawMode stretches its image over its Size, slices it into
// nine by its image's Slice insets, keeping its corners at their texels'
// size at any size, turned or mirrored, or tiles it, without seams along
// the axes it wraps, each at the sprite's TexelScale:
//
//	panel := canvas.ImageFromFile("ui/panel.png", canvas.ImageOptions{Slice: canvas.Insets{Left: 12, Top: 12, Right: 12, Bottom: 12}})
//	list.DrawSprite(canvas.Sprite{Image: panel, Size: m.Vec2{X: 320, Y: 150}, DrawMode: canvas.Sliced, TexelScale: 2})
//	list.DrawSprite(canvas.Sprite{Image: water, Size: m.Vec2{X: 800, Y: 200}, DrawMode: canvas.Tiled})
//
// An image's Convert converts each of its frames on the CPU before it is
// packed, canvas reading and decoding its file itself in the job that first
// draws it, into texels of the format the Converter names, packed into the
// atlas canvas keeps for that converter and format, so the images of equal
// converters share an atlas and batch together. The output's bounds place
// it within the frame, while the
// image's size and pivot measure the frame, so a trimmed sprite stays where
// it was, and a DistanceField of a sprite, drawn with the sprite's
// transform and size, lines up with it, its field reaching Spread texels
// beyond the art. SDFMaterial draws a field through a Profile, its
// argument: an Outline, a Halo, a Glow or a DropShadow, its offset in world
// space, lying under the sprite drawn over it. Drawn with it, a shape reads
// its distance from its rectangle and radius instead, its quad grown to
// hold the band:
//
//	field := canvas.ImageFromFile("hero.png", canvas.ImageOptions{Pivot: canvas.PivotBottom, Convert: canvas.DistanceField{Spread: 8}})
//	list.DrawSpriteWith(canvas.Sprite{Image: field, Material: canvas.SDFMaterial, Transform: m.Translation2x3(x, y)}, canvas.Halo(2, 8, gold))
//	list.DrawSprite(canvas.Sprite{Image: hero, Transform: m.Translation2x3(x, y)})
//	list.DrawShapeWith(canvas.Shape{Kind: canvas.Fill, Rect: button, Radius: 6, Tint: m.White, Material: canvas.SDFMaterial}, canvas.Glow(8, gold))
//
// ImageFromTexture makes an Image of a texture made with render, never
// packed, drawn from its mip level 0 and layer 0, so a minimap camera's
// texture drawn as a sprite shows the world it drew. A texture of a format
// encoding nothing that holds encoded colour, as a camera's does, is marked
// ColorSpaceSrgb, the zero, and decoded as it is sampled:
//
//	minimap := canvas.ImageFromTexture(minimapTexture)
//	list.DrawSprite(canvas.Sprite{Image: minimap, Transform: m.Translation2x3(x, y), Layer: layerUI})
//
// DrawTriangles draws a mesh given for one frame, a trail, a rope or a
// skin, copied as it is recorded, so its slices may be reused: Vertex's
// position, its UV over the image's region, repeating along the axes the
// image wraps along, and its colour, zero meaning white, through canvas's
// mesh vertex stage and the material's fragment stage. Its bounds are
// required, so it is culled without vanishing when its origin leaves the
// view:
//
//	trail := canvas.Triangles[canvas.Vertex]{Vertices: vertices, Topology: meshfile.TopologyTriangleStrip, Bounds: meshfile.CalculateBounds(vertices, canvas.Vertex.Position3)}
//	list.DrawTriangles(canvas.DrawTrianglesOptions{Tint: spark, Layer: layerEffects}, trail)
//
// DrawMesh draws a mesh file named by a path, loaded at its first draw
// through platform's FS and kept until its path is unloaded, sampling its
// image as DrawTriangles does. A game puts the files it builds with
// meshfile.Encode into a memory mount of its own, or ships them; meshes of
// one file recorded in one DrawMeshes are one instanced draw:
//
//	tree := canvas.MeshFileFromFile("meshes/tree.mesh")
//	list.DrawMeshes(forest) // each canvas.Mesh{File: tree, Image: bark, Transform: m.Translation2x3(x, y), Layer: layerWorld}
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
// sprite atlas's white texel, which takes no filter. canvas's atlases are
// values of the public package atlas, which a plugin uses for a cache of
// its own, such as a UI's rasterised text.
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
// An entry is drawn with its Material, the zero one the default material,
// its image times its tint. A task makes a 2D material through its Assets
// parameter from a whole WGSL file that includes canvas's include,
// "cog.canvas/canvas.wgsl", and declares only @group(1), its parameters: a
// shader of a fragment stage alone draws through canvas's vertex stage, so
// it serves sprites and shapes alike. Its Blend is premultiplied, zero
// meaning BlendNormal(), with BlendAdditive(), BlendMultiply(),
// BlendScreen() and BlendOpaque() ready. Its parameters are set by name, and
// a draw uses them as they stand when canvas draws the frame, so time is a
// parameter set from the clock that owns it:
//
//	glow := assets.MakeMaterial(canvas.MaterialDescriptor{Label: "glow", Shader: "shaders/glow.wgsl", Blend: canvas.BlendAdditive()})
//	assets.SetMaterialUniform(glow, "time", gameTime)
//	list.DrawSprite(canvas.Sprite{Image: spark, Material: glow})
//
// A material may take an argument, a value of its own type each entry gives
// it, read by its fragment stage with the include's canvas_argument from the
// shader's struct Argument, which it mirrors; entries of one material batch
// whatever their arguments, and one giving none gives the zero value:
//
//	dissolve := assets.MakeMaterial(canvas.MaterialDescriptor{Label: "dissolve", Shader: "shaders/dissolve.wgsl", Argument: canvas.ArgumentOf[Dissolve]()})
//	list.DrawSpriteWith(canvas.Sprite{Image: unit, Material: dissolve}, Dissolve{Amount: 0.4})
//	list.DrawSpritesWith(sprites, dissolves)
//
// A task loads an image ahead of its first draw, unloads a level's images,
// sizes an image before it is drawn, and reloads every changed image, font
// file, mesh file and 2D material's shader through its Assets parameter:
//
//	assets.Preload(hero)
//	assets.Unload("level1/")
//	size, ok := assets.ImageSize(hero)
//	assets.ReloadChanged()
//
// Text names a font set and a size, never a font. A task makes a set on
// purpose, at a start or behind a loading screen, through FontLibrary's writing
// side, which only a write handle gives, so a task reading FontLibrary cannot:
// every font file the set names loads then, each language served by its
// own fonts and the rest by the default font. The zero FontSet is the
// default set, Config.DefaultFontSet, JetBrains Mono alone unless it names
// others. A font's FontMetrics at a size are read through FontLibrary, and
// text laid out, shaped, broken into lines and measured, with no frame:
//
//	func loadFonts(k *kernel.Kernel, _ kernel.NoArg, libraryHandle kernel.Write[canvas.FontLibrary], files kernel.Read[platform.FS]) kernel.NoResult {
//		library := canvas.ElevateFontLibrary(libraryHandle, files)
//		title = library.MakeFontSet(canvas.FontSetDescriptor{
//			Label:     "title",
//			Default:   canvas.Font{Path: "fonts/inter-variable.ttf", Variations: [4]canvas.Variation{{Axis: canvas.AxisTagFromName("wght"), Value: 800}}},
//			Languages: []canvas.LanguageFonts{{Languages: []string{"ja", "zh-Hans"}, Fonts: []canvas.Font{{Path: "fonts/noto-sans-cjk.ttc"}}}},
//		})
//		library.SetLanguage("ja")
//		return kernel.NoResult{}
//	}
//
//	metrics := libraryHandle.Get().Metrics(title, 24, "")
//	layout := libraryHandle.Get().Layout(canvas.Text{String: "Continue", Font: title, Size: 24, WrapWidth: 200, Align: canvas.TextAlignCenter})
//	buttonSize := layout.Size()
//
// A run of text is drawn as one entry, through a transform about a pivot of
// its layout's box, laid out through the layout cache Layout answers from,
// so a label drawn every frame is shaped once, each glyph made a distance
// field at its first draw, or ahead of it by FontLibrary.Preload, and packed
// into the glyph atlas, which at its ceiling evicts the glyphs drawn least
// lately, never one drawn this frame. TextMaterial draws its fill, and a Profile,
// its argument, under it, so every label of a frame batches:
//
//	list.DrawTextWith(canvas.Text{String: unit.Name, Size: 12, Pivot: canvas.PivotBottom, Transform: m.Translation2x3(x, y-36)}, canvas.Outline(2, m.Black))
//
// A tool asks for an inspection of the next frame through its Assets
// parameter, and reads it in a task off InspectedEvent: each camera the frame
// was seen through, its batches and their entries, and what it culled; the
// frame's first uses and reports; how long canvas took to sort and draw it;
// with render's own inspection of the frame beside it. Its String writes it
// as text for an agent to log. Every other frame pays one check:
//
//	func inspect(k *kernel.Kernel, event canvas.InspectedEvent, assets canvas.Assets) kernel.NoResult {
//		inspection, err := assets.ReadInspection(event)
//		if err == nil {
//			log.Print(inspection)
//		}
//		...
//	}
//
// A mistake canvas finds at run time is a Report, logged once for its
// subject through the kernel's LogErrOnce and carried past.
package canvas
