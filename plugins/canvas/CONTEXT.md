# Canvas

Canvas draws 2D on render: the sprites, shapes, text and meshes a frame records, sorted and drawn through 2D materials. It keeps nothing of a frame once drawn but its atlas, its glyphs and the assets it loaded, and it imports no ECS: an ECS plugin records into it as any user does. This glossary records its domain language.

## Language

**Entry**:
One thing recorded into a draw list for a frame: a sprite, a shape, a run of text or a mesh, with its sort key. A frame's entries are forgotten once drawn.
_Avoid_: Mark, draw (render's), item, op, command, sprite object

**Sort key**:
An entry's layer and its order within that layer. Entries are drawn in sort-key order, and those with equal keys in the order they were recorded, unless their layer regroups them.
_Avoid_: Order (render's), z, depth

**Layer**:
The first part of a sort key: every entry of a lower layer is drawn under every entry of a higher one. A layer may regroup its entries of equal keys so that they batch, where their order among themselves does not matter. A camera draws the layers in its ranges, so one layer may be drawn by several cameras.
_Avoid_: Sorting layer, canvas layer, pass, z index

**Draw list**:
One recording task's entries for a frame. Each task records into its own, and the lists are merged by sort key when the frame is drawn, the order the tasks were registered in breaking ties.
_Avoid_: Canvas list, command list (render's), queue, op queue

**Atlas**:
Where canvas packs the images its sprites and glyphs draw, so that entries over different images can be drawn together. An atlas holds images of one format, on as many pages as it needs.
_Avoid_: Sprite sheet, texture page

**Page**:
One layer of an atlas. Entries over any page of one atlas can still be drawn together.
_Avoid_: Texture page, sheet, layer (canvas's own term, for sorting)

**Image**:
A picture canvas can draw: a file, or a render texture, and the options it is drawn with, such as how its edges fill and whether it is a sheet. Naming an image loads nothing; an image of a file is packed into an atlas when it is first drawn, and stays until it is unloaded or evicted, and an image of a render texture is drawn from the texture itself.
_Avoid_: Sprite, texture, asset, handle

**Sheet**:
An image of several frames laid out in a grid, each packed as a region of its own.
_Avoid_: Sprite sheet, atlas, flipbook

**Pivot**:
The point that a transform or a camera places, turns and scales about: a point of an image, of a layout's box, or of a camera's viewport, its centre unless another is named. An image's is measured in the image's own space, and a sheet's frames share one, each measured within its frame.
_Avoid_: Origin, anchor, hotspot, offset

**Region**:
A rectangle of an atlas: an image packed there, or one frame of a sheet.
_Avoid_: Frame (render's and gfx's), sub-texture, UV rect

**Gutter**:
The border packed around a region, filled from its own edges, so filtering and mips never reach a neighbour: its edge continued, or its opposite edge where the image tiles.
_Avoid_: Padding, margin, bleed, extrusion

**Atlas grid**:
The spacing an atlas's regions are aligned to, set by how many mip levels its pages have, so each mip level halves every region's place exactly and never mixes two regions. A sheet's grid is its columns and rows of frames, not this.
_Avoid_: Grid alone, alignment, block size

**Sprite**:
An entry that draws a region through a transform about its image's pivot, with a tint and a 2D material.
_Avoid_: Quad, image, sprite renderer

**Draw mode**:
How a sprite fills its rectangle with its region: **stretched** over it, **sliced** into nine so its corners keep their size, or **tiled**, repeating the region by wrapping it. A sliced sprite's corners and a tiled sprite's tiles are drawn at one texel scale, the sprite's own.
_Avoid_: Image mode, sprite mode, nine-patch

**Tint**:
The colour a sprite or shape is multiplied by: white leaves a sprite as it is, and a shape's colour is its tint.
_Avoid_: Color, modulate, colour multiplier

**Crop**:
The part of a sprite's rectangle it draws, the rest left out where it lies, as a fill bar empties from one side.
_Avoid_: Sub-region, cutout, fill amount, clip (canvas's own term, for a rectangle of the world)

**Filter**:
How a sprite's texels are read between their centres: linear, blending its neighbours, or nearest, for pixel art.
_Avoid_: Sampler, smoothing, interpolation

**Camera**:
How a frame's entries are seen: which layers, from what position, at what zoom and rotation, into which target and which rectangle of it. A frame's cameras are set with its entries and forgotten once it is drawn; a frame that sets none is seen through the default camera.
_Avoid_: View (mux's), viewport (the rectangle), 3D camera (drawbridge's)

**Viewport**:
The rectangle of a target a camera draws into, as fractions of the target, so split screen is two cameras with a viewport each. render's viewport is the pixel rectangle a pass draws through, which canvas sets from it.
_Avoid_: Screen rect, region, rect

**World unit**:
What entries are placed and sized in. At zoom 1 it is one point of a window, so a display of scale 2 draws it 2 pixels across, or one pixel of a texture, unless the camera fits a logical size into its viewport.
_Avoid_: Pixel, point (platform's), metre

**Logical size**:
The area of the world, in world units, that a camera is meant to show, fitted into its viewport. One axis may be left to follow the viewport's shape; leaving both makes it the viewport's own size, one unit to a point.
_Avoid_: Design resolution, reference resolution, virtual resolution

**Fit**:
How a camera's logical size fills a viewport of another shape: expanded, showing more of the world along the longer axis; inscribed, with bars; overlapping, its edges cut off; or stretched.
_Avoid_: Stretch mode, aspect mode, scale mode, viewport mode

**Pixel-perfect**:
A camera that draws a whole number of target pixels to each world unit and snaps every entry to the world-unit grid, so pixel art keeps its pixels square and still. It is a camera's setting, not a kind of camera.
_Avoid_: Pixel snapping, integer scaling, pixel camera

**Shape**:
An entry that fills or strokes a rectangle, or draws a line.
_Avoid_: Primitive, gizmo

**Mesh**:
An entry that draws triangles through a transform, with a tint and a 2D material, sampling an image as a sprite does: the triangles of a mesh file, or vertices given with the draw for that frame alone.
_Avoid_: Polygon, geometry, triangles, mesh instance, mesh (render's)

**Mesh file**:
Triangles kept with their vertex layout and bounds, named by a path as an image is and loaded when a mesh first draws them. A game puts its own into a memory mount, or ships them as files; they stay until unloaded.
_Avoid_: Mesh asset, model, made mesh

**Clip**:
A rectangle of the world outside which an entry draws nothing, seen through each camera as the entry is.
_Avoid_: Mask, scissor (render's)

**2D material**:
How an entry is drawn: a shader that includes canvas's modules, its blend, the type of its material argument, and its parameters. canvas makes a render material for it, which its users never hold.
_Avoid_: Material (render's), shader, effect, look

**Material argument**:
The value one entry gives its 2D material, of the one type the material declares, such as an outline's colour and width. A render *material parameter* is shared by every draw of a material; a material argument belongs to one entry.
_Avoid_: Material parameter (render's), tail, style, instance data, instance property

**Converter**:
A comparable value that turns each frame of an image into other texels on the CPU before it is packed, such as a trim or a distance field's bake. Its output's bounds place it within the frame; the image's size and pivot are measured on the frame.
_Avoid_: Filter, processor, image effect

**Distance field**:
An image whose alpha holds each texel's distance to an edge, baked from a sprite's alpha or kept by distance-field glyphs. The SDF material draws outlines, halos, glows and drop shadows from it.
_Avoid_: SDF image, signed distance texture

**Profile**:
How the SDF material maps a distance to a colour: an outline's hard band, a halo's plateau and falloff, a glow's falloff, or a drop shadow's offset band. It is the SDF material's material argument.
_Avoid_: Effect, halo profile, style

**Font**:
A font file in a font set and the options it is drawn with, such as whether its glyphs are distance fields or bitmaps. It is loaded when its set is made.
_Avoid_: Face, typeface, font asset

**Font set**:
The fonts text draws with: a default font, and the fonts serving each language. A character is drawn by the language's fonts that have it, or else by the default font. Text names a font set and a size, never a font. A set is made and destroyed on purpose, never loaded by drawing.
_Avoid_: Font family, font stack, fallback chain, font asset

**Language**:
What text is written in, which picks a font set's fonts and the forms a font draws: the current language, unless a run names its own. Languages are not mixed: a character the language's fonts lack is drawn by the default font.
_Avoid_: Locale, culture, script

**Glyph**:
One shape a font draws, packed into the glyph atlas the first time it is drawn: a distance field, or a bitmap for a bitmap font.
_Avoid_: Character, rune, letter (a glyph can draw several characters, and a character can take several glyphs)

**Layout**:
Text shaped into glyphs, broken into lines and measured. Drawing and measuring read the same layout, so they never disagree.
_Avoid_: Text block, paragraph, label

**Span**:
A part of a run of text with a tint of its own, or an icon. A run's spans are shaped and wrapped together, so a coloured word wraps with its paragraph.
_Avoid_: Segment, fragment, chunk, style run

**Icon**:
A picture registered under a name, which text places like a glyph: on the baseline, as tall as the font's capitals unless the icon scales itself. Registering a name again replaces its picture everywhere it is used.
_Avoid_: Inline image, sprite, emoji, glyph

**Markup**:
The string form of a run's spans, which canvas reads only when the run asks it to, so that text a player typed draws as it was written.
_Avoid_: Rich text, tags, BBCode
