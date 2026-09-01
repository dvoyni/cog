package canvas

import (
	"encoding/binary"
	"math"
	"slices"

	"github.com/dvoyni/cog/m"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/storage"
)

const Name kernel.PluginName = "canvas"

type UpdateEventHandler kernel.Subscription[app.UpdateEvent]

type Plugin struct {
	config       Config
	quad         gfx.MeshDescr
	quadVertices gfx.BufferDescr
	quadIndices  gfx.BufferDescr
	quadReady    bool
	layers       []Layer
	params       []gfx.ParameterDescr
	tileVertices []byte
	textParams   [1]gfx.ParameterDescr
	batch        spriteBatch
	tris         trianglesBatch
}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins canvas requires: gfx (for the draw pipeline)
// and storage (which hosts its shader filesystem mount).
func (p *Plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{gfx.Name, storage.Name}
}

func (p *Plugin) Register(registrar *kernel.Registrar, value any) error {
	config, err := resolveConfig(value)
	if err != nil {
		return err
	}
	p.config = config
	registrar.InitResource(&opQueue{})
	registrar.InitResource(newLookup(config))
	registrar.Subscribe[UpdateEventHandler](p.flush).
		Last().Before[gfx.UpdateEventHandler]()
	return nil
}

// Start mounts the built-in shader filesystem. Startup runs after every plugin
// has registered and before the host loop, so the shaders are in place for the
// first frame without depending on a driver publishing an event.
func (p *Plugin) Start(k kernel.Executioner) error {
	_, err := k.ExecuteCommand[storage.SetReadFSCmd](storage.SetReadFSRequest{Mount: storage.ReadMount{
		Id: shaderMountID, Priority: math.MaxInt, FS: shaderFS,
	}})
	return err
}

func (p *Plugin) flush() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var writeQueue kernel.Write[*OpQueue]
	var gfxQueue kernel.Write[*gfx.OpQueue]
	var gfxResourceQueue kernel.Write[*gfx.ResourceQueue]
	var viewport kernel.Read[*app.Viewport]
	var filesystem kernel.Read[storage.ReadFS]
	var lookupResource kernel.Write[*Lookup]
	return func(access kernel.ResourceAccess) {
			writeQueue = access.GetWrite[*OpQueue]()
			gfxQueue = access.GetWrite[*gfx.OpQueue]()
			gfxResourceQueue = access.GetWrite[*gfx.ResourceQueue]()
			viewport = access.GetRead[*app.Viewport]()
			filesystem = access.GetRead[storage.ReadFS]()
			lookupResource = access.GetWrite[*Lookup]()
		}, func(kernel.Kernel, app.UpdateEvent) error {
			return p.flushFrame(writeQueue.Get(), gfxQueue.Get(), gfxResourceQueue.Get(),
				viewport.Get(), filesystem.Get(), lookupResource.Get())
		}
}

func (p *Plugin) flushFrame(
	write *OpQueue, gfxWrite *gfx.OpQueue, gfxResources *gfx.ResourceQueue,
	view *app.Viewport, filesystem storage.ReadFS, lookup *Lookup,
) error {
	spriteAtlas := lookup.sprites
	fontAtlas := lookup.fonts
	fonts := lookup.fontStore
	defer write.reset()
	if !gfxResources.Ready() || view.Width <= 0 || view.Height <= 0 {
		return nil
	}
	lookup.applyUnloads(gfxResources)
	lookup.invalidateFontsOnResize(gfxResources, view)
	if !p.ensureQuad(gfxResources) {
		return nil
	}
	if write.hasColor {
		gfxWrite.Clear(write.clearColor)
	}
	if write.hasDepth {
		gfxWrite.ClearDepth(write.clearDepth)
	}
	spriteAtlas.beginFrame()
	fontAtlas.beginFrame()
	p.layers = p.layers[:0]
	for layerID, value := range write.ops {
		if len(value.ops) > 0 {
			p.layers = append(p.layers, layerID)
		}
	}
	slices.Sort(p.layers)
	for _, layerID := range p.layers {
		value := write.ops[layerID]
		transform := resolveLayerTransform(value, view)
		for i := range value.ops {
			switch value.ops[i].kind {
			case drawSprite:
				p.tris.flush(gfxWrite)
				p.drawSprite(gfxWrite, spriteAtlas, gfxResources, filesystem, view, transform, value.ops[i].clip, value.ops[i].hasClip, &value.ops[i].sprite)
			case drawText:
				p.tris.flush(gfxWrite)
				p.drawText(gfxWrite, spriteAtlas, fontAtlas, gfxResources, filesystem, view, fonts, transform, value.ops[i].clip, value.ops[i].hasClip, &value.ops[i].text)
			case drawTriangles:
				p.batch.flush(gfxWrite, p.quad)
				op := &value.ops[i].triangles
				p.drawTriangles(gfxWrite, view, transform, value.ops[i].clip, value.ops[i].hasClip, write.layouts[op.layoutID], op)
			}
		}
		p.batch.flush(gfxWrite, p.quad)
		p.tris.flush(gfxWrite)
	}
	return nil
}

func (p *Plugin) drawTriangles(gfxWrite *gfx.OpQueue, view *app.Viewport, layerTransform m.Mat4, clip m.Rect, hasClip bool, layout []gfx.VertexAttr, op *trianglesOp) {
	if hasClip && (clip.Width <= 0 || clip.Height <= 0) {
		return
	}
	texture, sampler, keyColor, hasTexture, batchable := trianglesBatchKey(op)
	if op.hasMaterial || !batchable {
		p.tris.flush(gfxWrite)
		p.emitTrianglesDirect(gfxWrite, view, layerTransform, clip, hasClip, layout, op)
		return
	}
	p.tris.add(gfxWrite, m.Vec2{X: view.Width, Y: view.Height}, op.layoutID, layout,
		texture, hasTexture, sampler, keyColor, layerTransform, clip, hasClip, op.vertices)
}

// trianglesBatchKey extracts the texture/sampler/keying a default-material
// triangle op samples. batchable is false when the op carries any parameter the
// batch path does not fold into its key, so it falls back to a direct draw.
func trianglesBatchKey(op *trianglesOp) (texture gfx.TextureDescr, sampler gfx.SamplerDesc, keyColor m.Color, hasTexture bool, batchable bool) {
	keyColor = m.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}
	batchable = true
	for i := range op.params {
		switch op.params[i].Name() {
		case TextureSlot:
			if t, ok := op.params[i].TextureValue(); ok {
				texture, hasTexture = t, true
			}
		case SamplerSlot:
			if s, ok := op.params[i].SamplerValue(); ok {
				sampler = s
			}
		case "keyColor":
			if c, ok := op.params[i].ColorValue(); ok {
				keyColor = c
			}
		default:
			batchable = false
		}
	}
	return texture, sampler, keyColor, hasTexture, batchable
}

// emitTrianglesDirect draws one triangle op without batching (custom material or
// unbatchable parameters).
func (p *Plugin) emitTrianglesDirect(gfxWrite *gfx.OpQueue, view *app.Viewport, layerTransform m.Mat4, clip m.Rect, hasClip bool, layout []gfx.VertexAttr, op *trianglesOp) {
	clipEnabled := float32(0)
	if hasClip {
		clipEnabled = 1
	}
	material := defaultTrianglesMaterial
	if op.hasMaterial {
		material = op.material
	}
	p.params = p.params[:0]
	p.params = append(p.params,
		gfx.VecParam("canvasViewport", m.Vec4{X: view.Width, Y: view.Height, Z: clipEnabled}),
		gfx.MatParam("canvasLayer", layerTransform),
		gfx.VecParam("canvasClip", m.Vec4{X: clip.X, Y: clip.Y, Z: clip.X + clip.Width, W: clip.Y + clip.Height}),
	)
	p.params = append(p.params, op.params...)
	mesh := gfx.Mesh(
		gfx.BufferWithBytes(op.vertices, true),
		gfx.TopologyTriangleList,
		layout...,
	)
	gfxWrite.Draw(mesh, material, p.params...)
}

func resolveLayerTransform(value layer, view *app.Viewport) m.Mat4 {
	scale, offset := LayerTransform(value.window, value.aspect, m.Vec2{X: view.Width, Y: view.Height})
	return m.Translation4(offset.X, offset.Y, 0).Mul(m.Scaling4(scale.X, scale.Y, 1))
}

func (p *Plugin) ensureQuad(resources *gfx.ResourceQueue) bool {
	if p.quadReady {
		return true
	}
	vertices, indices := unitQuadBytes()
	p.quadVertices = resources.BakeBuffer(vertices, true)
	p.quadIndices = resources.BakeBuffer(indices, true)
	p.quad = gfx.MeshIndexed(p.quadVertices, p.quadIndices, gfx.TopologyTriangleList, gfx.Attr(0, gfx.Float32x2))
	p.quadReady = true
	return true
}

// clearFontFaces closes baked faces but keeps parsed sources for re-baking.
func clearFontFaces(fonts *fontStore) {
	for _, cached := range fonts.fonts {
		_ = cached.face.Close()
	}
	clear(fonts.fonts)
}

func (p *Plugin) drawSprite(gfxWrite *gfx.OpQueue, atlas *atlas, gfxResources *gfx.ResourceQueue, filesystem storage.ReadFS, view *app.Viewport, layerTransform m.Mat4, clip m.Rect, hasClip bool, op *spriteOp) {
	t := op.transform
	if t.TileX || t.TileY {
		if op.path == "" {
			t.TileX, t.TileY = false, false
		} else {
			p.batch.flush(gfxWrite, p.quad)
			p.drawTiledSprite(gfxWrite, atlas, gfxResources, filesystem, view, t, layerTransform, clip, hasClip, op)
			return
		}
	}
	entry, ok := atlas.resolveSprite(op.path, filesystem, gfxResources)
	if !ok {
		return
	}
	if t.NineSlice != (SpriteFrame{}) {
		p.drawNineSlice(gfxWrite, view, entry, t, layerTransform, clip, hasClip, op)
		return
	}
	if op.hasMaterial {
		p.batch.flush(gfxWrite, p.quad)
		material := op.material
		p.drawEntry(gfxWrite, view, entry, t, layerTransform, clip, hasClip, &material, op.params)
		return
	}
	tint := paramColorOr(op.params, "tint", m.Color{R: 1, G: 1, B: 1, A: 1})
	keyColor := paramColorOr(op.params, "keyColor", m.Color{R: 0.5, G: 0.5, B: 0.5, A: 1})
	p.batchEntry(gfxWrite, view, entry, t, layerTransform, clip, hasClip, tint, keyColor)
}

func (p *Plugin) drawNineSlice(gfxWrite *gfx.OpQueue, view *app.Viewport, entry atlasEntry, transform SpriteTransform, layerTransform m.Mat4, clip m.Rect, hasClip bool, op *spriteOp) {
	insets := transform.NineSlice
	if insets.Left < 0 || insets.Right < 0 || insets.Top < 0 || insets.Bottom < 0 ||
		insets.Left+insets.Right >= entry.width || insets.Top+insets.Bottom >= entry.height {
		return
	}
	size := entrySize(entry, transform)
	if size.X <= 0 || size.Y <= 0 {
		return
	}
	scale := transform.NineSliceScale
	if scale == 0 {
		scale = 1
	}
	destinationX := splitNineSliceAxis(size.X, float32(insets.Left)*scale, float32(insets.Right)*scale)
	destinationY := splitNineSliceAxis(size.Y, float32(insets.Top)*scale, float32(insets.Bottom)*scale)
	sourceX := [4]int{0, insets.Left, entry.width - insets.Right, entry.width}
	sourceY := [4]int{0, insets.Top, entry.height - insets.Bottom, entry.height}
	tint := paramColorOr(op.params, "tint", m.Color{R: 1, G: 1, B: 1, A: 1})
	keyColor := paramColorOr(op.params, "keyColor", m.Color{R: 0.5, G: 0.5, B: 0.5, A: 1})
	for row := 0; row < 3; row++ {
		for column := 0; column < 3; column++ {
			if transform.NineSliceNoCenter && row == 1 && column == 1 {
				continue
			}
			width := destinationX[column+1] - destinationX[column]
			height := destinationY[row+1] - destinationY[row]
			if width <= 0 || height <= 0 {
				continue
			}
			part := transform
			part.Position = m.Vec2{X: transform.Position.X + destinationX[column], Y: transform.Position.Y + destinationY[row]}
			part.Size = m.Vec2{X: width, Y: height}
			part.Origin = m.Vec2{}
			part.Rotation = 0
			part.Frame = SpriteFrame{
				Left: sourceX[column], Top: sourceY[row],
				Right: entry.width - sourceX[column+1], Bottom: entry.height - sourceY[row+1],
			}
			part.NineSlice = SpriteFrame{}
			if op.hasMaterial {
				p.batch.flush(gfxWrite, p.quad)
				material := op.material
				p.drawEntry(gfxWrite, view, entry, part, layerTransform, clip, hasClip, &material, op.params)
			} else {
				p.batchEntry(gfxWrite, view, entry, part, layerTransform, clip, hasClip, tint, keyColor)
			}
		}
	}
}

func splitNineSliceAxis(length, leading, trailing float32) [4]float32 {
	leading = min(max(leading, 0), length)
	trailing = min(max(trailing, 0), max(length-leading, 0))
	return [4]float32{0, leading, length - trailing, length}
}

// drawTiledSprite renders a sprite that repeats on one or both axes. It samples a
// standalone repeat texture through the textured-triangle path, so it ignores the
// sprite material and Frame; Scale controls logical tile size and tint becomes vertex color.
func (p *Plugin) drawTiledSprite(gfxWrite *gfx.OpQueue, atlas *atlas, gfxResources *gfx.ResourceQueue, filesystem storage.ReadFS, view *app.Viewport, t SpriteTransform, layerTransform m.Mat4, clip m.Rect, hasClip bool, op *spriteOp) {
	entry, ok := atlas.resolveStandalone(op.path, filesystem, gfxResources)
	if !ok {
		return
	}
	size := t.Size
	tileScale := t.Scale
	if tileScale == 0 {
		tileScale = 1
	}
	if !t.TileX && size.X == 0 {
		size.X = float32(entry.width) * tileScale
	}
	if !t.TileY && size.Y == 0 {
		size.Y = float32(entry.height) * tileScale
	}
	if size.X == 0 || size.Y == 0 {
		return
	}
	clipEnabled := float32(0)
	if hasClip {
		if clip.Width <= 0 || clip.Height <= 0 {
			return
		}
		clipEnabled = 1
	}
	spanX := float32(1)
	if t.TileX {
		spanX = size.X / (float32(entry.width) * tileScale)
	}
	spanY := float32(1)
	if t.TileY {
		spanY = size.Y / (float32(entry.height) * tileScale)
	}
	tint := spriteTint(op.params)
	sine := float32(math.Sin(float64(t.Rotation)))
	cosine := float32(math.Cos(float64(t.Rotation)))
	corners := [4]m.Vec2{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1}}
	uvs := [4]m.Vec2{{X: 0, Y: 0}, {X: spanX, Y: 0}, {X: spanX, Y: spanY}, {X: 0, Y: spanY}}
	var positions [4]m.Vec2
	for i, q := range corners {
		sx := (q.X - t.Origin.X) * size.X
		sy := (q.Y - t.Origin.Y) * size.Y
		positions[i] = m.Vec2{
			X: t.Position.X + sx*cosine - sy*sine,
			Y: t.Position.Y + sx*sine + sy*cosine,
		}
	}
	p.tileVertices = p.tileVertices[:0]
	for _, i := range [6]int{0, 1, 2, 0, 2, 3} {
		p.tileVertices = appendTileVertex(p.tileVertices, positions[i], tint, uvs[i])
	}
	p.params = p.params[:0]
	p.params = append(p.params,
		gfx.VecParam("canvasViewport", m.Vec4{X: view.Width, Y: view.Height, Z: clipEnabled}),
		gfx.MatParam("canvasLayer", layerTransform),
		gfx.VecParam("canvasClip", m.Vec4{X: clip.X, Y: clip.Y, Z: clip.X + clip.Width, W: clip.Y + clip.Height}),
		gfx.TextureParam(TextureSlot, entry.texture),
		gfx.SamplerParam(SamplerSlot, tileAddressMode(t), t.Filter),
	)
	p.params = append(p.params, op.params...)
	mesh := gfx.Mesh(gfx.BufferWithBytes(p.tileVertices, true), gfx.TopologyTriangleList, triangleVertexLayout[:]...)
	gfxWrite.Draw(mesh, defaultTrianglesMaterial, p.params...)
}

// tileAddressMode repeats only the axes the transform tiles, so the non-tiled
// axis clamps at its edges instead of wrapping.
func tileAddressMode(t SpriteTransform) gfx.AddressMode {
	address := gfx.AddressClamp
	if t.TileX {
		address |= gfx.AddressRepeatX
	}
	if t.TileY {
		address |= gfx.AddressRepeatY
	}
	return address
}

// spriteTint returns the first "tint" color parameter, defaulting to opaque white.
func spriteTint(params []gfx.ParameterDescr) m.Color {
	for i := range params {
		if params[i].Name() == "tint" {
			if color, ok := params[i].ColorValue(); ok {
				return color
			}
		}
	}
	return m.Color{R: 1, G: 1, B: 1, A: 1}
}

// appendTileVertex writes one position/color/uv vertex in the built-in triangle
// layout (Float32x2, Float32x4, Float32x2).
func appendTileVertex(dst []byte, position m.Vec2, color m.Color, uv m.Vec2) []byte {
	values := [8]float32{position.X, position.Y, color.R, color.G, color.B, color.A, uv.X, uv.Y}
	var word [4]byte
	for _, value := range values {
		binary.LittleEndian.PutUint32(word[:], math.Float32bits(value))
		dst = append(dst, word[:]...)
	}
	return dst
}

func (p *Plugin) drawEntry(gfxWrite *gfx.OpQueue, view *app.Viewport, entry atlasEntry, transform SpriteTransform, layerTransform m.Mat4, clip m.Rect, hasClip bool, material *gfx.MaterialDescr, params []gfx.ParameterDescr) {
	size := entrySize(entry, transform)
	if size.X == 0 || size.Y == 0 {
		return
	}
	uv, ok := entryUV(entry, transform)
	if !ok {
		return
	}
	sine, cosine := sincos(transform.Rotation)
	clipEnabled := float32(0)
	if hasClip {
		if clip.Width <= 0 || clip.Height <= 0 {
			return
		}
		clipEnabled = 1
	}
	p.params = p.params[:0]
	p.params = append(p.params,
		gfx.VecParam("canvasTransform0", m.Vec4{X: transform.Position.X, Y: transform.Position.Y, Z: size.X, W: size.Y}),
		gfx.VecParam("canvasTransform1", m.Vec4{X: transform.Origin.X, Y: transform.Origin.Y, Z: sine, W: cosine}),
		gfx.VecParam("canvasFrame", uv),
		gfx.VecParam("canvasViewport", m.Vec4{X: view.Width, Y: view.Height}),
		gfx.FloatParam("atlasLayer", float32(entry.layer)),
		gfx.FloatParam("clipEnabled", clipEnabled),
		gfx.MatParam("canvasLayer", layerTransform),
		gfx.VecParam("canvasClip", m.Vec4{X: clip.X, Y: clip.Y, Z: clip.X + clip.Width, W: clip.Y + clip.Height}),
		gfx.TextureParam(TextureSlot, entry.texture),
		gfx.SamplerParam(SamplerSlot, gfx.AddressClamp, transform.Filter),
	)
	p.params = append(p.params, params...)
	gfxWrite.Draw(p.quad, *material, p.params...)
}

// entrySize resolves the on-screen size of an atlas entry from a transform's
// explicit size, single-axis size, or uniform scale.
func entrySize(entry atlasEntry, transform SpriteTransform) m.Vec2 {
	size := transform.Size
	switch {
	case size.X != 0 && size.Y != 0:
	case size.X != 0:
		size.Y = size.X * float32(entry.height) / float32(entry.width)
	case size.Y != 0:
		size.X = size.Y * float32(entry.width) / float32(entry.height)
	default:
		scale := transform.Scale
		if scale == 0 {
			scale = 1
		}
		size = m.Vec2{X: float32(entry.width) * scale, Y: float32(entry.height) * scale}
	}
	return size
}

// entryUV resolves the sampled uv rect for an atlas entry, applying an optional
// source frame inset and horizontal/vertical flips. It returns false when the
// frame inset is out of bounds.
func entryUV(entry atlasEntry, transform SpriteTransform) (m.Vec4, bool) {
	uv := entry.uv
	frame := transform.Frame
	if frame != (SpriteFrame{}) {
		if frame.Left < 0 || frame.Top < 0 || frame.Right < 0 || frame.Bottom < 0 ||
			frame.Left+frame.Right >= entry.width || frame.Top+frame.Bottom >= entry.height {
			return m.Vec4{}, false
		}
		uv.X += float32(frame.Left) * entry.texelSize
		uv.Y += float32(frame.Top) * entry.texelSize
		uv.Z -= float32(frame.Right) * entry.texelSize
		uv.W -= float32(frame.Bottom) * entry.texelSize
	}
	if transform.FlipX {
		uv.X, uv.Z = uv.Z, uv.X
	}
	if transform.FlipY {
		uv.Y, uv.W = uv.W, uv.Y
	}
	return uv, true
}

func sincos(rotation float32) (sine, cosine float32) {
	return float32(math.Sin(float64(rotation))), float32(math.Cos(float64(rotation)))
}

// paramColorOr returns the named color parameter, or def when it is absent.
func paramColorOr(params []gfx.ParameterDescr, name string, def m.Color) m.Color {
	for i := range params {
		if params[i].Name() == name {
			if color, ok := params[i].ColorValue(); ok {
				return color
			}
		}
	}
	return def
}

func (p *Plugin) drawGlyphRun(gfxWrite *gfx.OpQueue, atlas *atlas, resources *gfx.ResourceQueue, filesystem storage.ReadFS, view *app.Viewport, fonts *fontStore, layerTransform m.Mat4, clip m.Rect, hasClip bool, op *textOp) {
	if op.draw.Size <= 0 || op.text == "" || op.fontPath == "" {
		return
	}
	// Rasterize glyphs at the on-screen pixel size (layer scale x framebuffer
	// scale), then lay them out in logical units so text stays crisp at any scale.
	px := max(1, int(math.Round(float64(op.draw.Size*textRasterScale(layerTransform, view)))))
	face := fonts.face(filesystem, op.fontPath, px)
	if face == nil {
		return
	}
	toLogical := op.draw.Size / float32(px)
	y := op.draw.Position.Y
	for start := 0; start <= len(op.text); {
		end := start
		for end < len(op.text) && op.text[end] != '\n' {
			end++
		}
		line := op.text[start:end]
		width := p.glyphLineWidth(atlas, op.fontPath, px, face, line, resources) * toLogical
		x := op.draw.Position.X
		switch op.draw.Align {
		case AlignCenter:
			x -= width / 2
		case AlignRight:
			x -= width
		}
		var previous rune
		first := true
		for _, character := range line {
			glyph, ok := p.glyph(atlas, op.fontPath, px, character, face, resources)
			if !ok {
				continue
			}
			if !first {
				x += float32(face.face.Kern(previous, character)) / 64 * toLogical
			}
			if glyph.visible {
				transform := SpriteTransform{
					Position: m.Vec2{X: x + glyph.offset.X*toLogical, Y: y + glyph.offset.Y*toLogical},
					Size:     m.Vec2{X: float32(glyph.entry.width) * toLogical, Y: float32(glyph.entry.height) * toLogical},
				}
				p.batchEntry(gfxWrite, view, glyph.entry, transform, layerTransform, clip, hasClip, op.draw.Color, m.Color{R: 0.5, G: 0.5, B: 0.5, A: 1})
			}
			x += glyph.advance * toLogical
			previous = character
			first = false
		}
		y += face.lineHeight * toLogical
		if end == len(op.text) {
			break
		}
		start = end + 1
	}
}

// drawText expands inline icons and wraps lines before drawing glyph runs.
func (p *Plugin) drawText(gfxWrite *gfx.OpQueue, spriteAtlas, fontAtlas *atlas, resources *gfx.ResourceQueue, filesystem storage.ReadFS, view *app.Viewport, fonts *fontStore, layerTransform m.Mat4, clip m.Rect, hasClip bool, op *textOp) {
	if op.draw.Size <= 0 || op.text == "" || op.fontPath == "" {
		return
	}
	px := max(1, int(math.Round(float64(op.draw.Size*textRasterScale(layerTransform, view)))))
	face := fonts.face(filesystem, op.fontPath, px)
	if face == nil {
		return
	}
	toLogical := op.draw.Size / float32(px)
	metrics := face.face.Metrics()
	ascent := float32(metrics.Ascent) / 64 * toLogical
	capHeight := float32(metrics.CapHeight) / 64 * toLogical
	lineHeight := face.lineHeight * toLogical
	measure := func(line []inlineSegment) float32 {
		var width float32
		for _, segment := range line {
			if segment.icon {
				width += p.iconWidth(spriteAtlas, segment.text, capHeight, filesystem, resources)
				continue
			}
			width += p.glyphLineWidth(fontAtlas, op.fontPath, px, face, segment.text, resources) * toLogical
		}
		return width
	}
	lines := parseInlineText(op.text)
	if op.draw.WordWrapping && validWrapWidth(op.draw.WrapWidth) {
		lines = wrapInlineText(lines, op.draw.WrapWidth, measure)
	}
	y := op.draw.Position.Y
	for _, line := range lines {
		total := measure(line)
		x := op.draw.Position.X
		switch op.draw.Align {
		case AlignCenter:
			x -= total / 2
		case AlignRight:
			x -= total
		}
		for _, segment := range line {
			if segment.icon {
				entry, ok := spriteAtlas.resolveSprite(normalizeResourcePath(segment.text), filesystem, resources)
				if !ok {
					continue
				}
				width := capHeight * float32(entry.width) / float32(entry.height)
				transform := SpriteTransform{
					Position: m.Vec2{X: x, Y: y + ascent - capHeight},
					Size:     m.Vec2{X: width, Y: capHeight},
				}
				p.batchEntry(gfxWrite, view, entry, transform, layerTransform, clip, hasClip, m.Color{R: 1, G: 1, B: 1, A: 1}, m.Color{R: 0.5, G: 0.5, B: 0.5, A: 1})
				x += width
				continue
			}
			run := textOp{fontPath: op.fontPath, text: segment.text, draw: TextDraw{
				Position: m.Vec2{X: x, Y: y}, Size: op.draw.Size, Color: op.draw.Color, Align: AlignLeft,
			}}
			p.drawGlyphRun(gfxWrite, fontAtlas, resources, filesystem, view, fonts, layerTransform, clip, hasClip, &run)
			x += p.glyphLineWidth(fontAtlas, op.fontPath, px, face, segment.text, resources) * toLogical
		}
		y += lineHeight
	}
}

// iconWidth resolves an inline icon and returns its cap-height-scaled width.
func (p *Plugin) iconWidth(spriteAtlas *atlas, path string, capHeight float32, filesystem storage.ReadFS, resources *gfx.ResourceQueue) float32 {
	entry, ok := spriteAtlas.resolveSprite(normalizeResourcePath(path), filesystem, resources)
	if !ok {
		return 0
	}
	return capHeight * float32(entry.width) / float32(entry.height)
}

// textRasterScale is the world-to-physical-pixel scale for glyph rasterization:
// the layer's uniform scale times the framebuffer/logical ratio. Layer transforms
// are rotation-free, so [0] and [5] are the axis scales.
func textRasterScale(layerTransform m.Mat4, view *app.Viewport) float32 {
	sx, sy := layerTransform[0], layerTransform[5]
	if sx < 0 {
		sx = -sx
	}
	if sy < 0 {
		sy = -sy
	}
	layerScale := max(sx, sy)
	if layerScale <= 0 {
		layerScale = 1
	}
	framebuffer := float32(1)
	if view.Width > 0 && view.FramebufferWidth > 0 {
		framebuffer = view.FramebufferWidth / view.Width
	}
	return layerScale * framebuffer
}
