package internal

import (
	"encoding/binary"
	"math"
	"slices"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/internal/types"
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

// plugin registers canvas's two resources, ArmDrawsCmd, the flush and the two
// draw-snapshot subscriptions, and contributes canvas's mcp Provider. Its
// fields are the flush's scratch, kept across frames so a steady-state frame
// allocates nothing, and the snapshot slot.
type plugin struct {
	config       canvas.Config
	quad         gfx.MeshDescr
	quadVertices gfx.BufferDescr
	quadIndices  gfx.BufferDescr
	quadReady    bool
	layers       []canvas.Layer
	// params is the flush scratch for the two draws that emit their own quad
	// rather than joining a batcher: the tiled sprite and the texture sprite.
	params       []gfx.ParameterDescr
	tileVertices []byte
	batch        spriteBatch
	tris         trianglesBatch

	// arrays and shared split one sprite draw's parameters by frequency. They are
	// scratch for the op being drawn, which is one at a time, and the batcher
	// copies what it keeps.
	arrays []gfx.ParameterDescr
	shared []gfx.ParameterDescr
	// trianglesParams is the same for a triangle draw, which needs one list
	// rather than two because every parameter it names is per material.
	trianglesParams []gfx.ParameterDescr

	// snapshots is canvas's one draw-snapshot slot, plugin-owned and
	// self-synchronizing. See snapshotState for why it is not a kernel
	// resource.
	snapshots snapshotState
}

// New creates the canvas plugin. Configure it with a canvas.Config under
// canvas.Name.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return canvas.Name }

// Dependencies reports the plugins canvas requires: gfx (for the draw pipeline)
// and storage (which hosts its shader filesystem mount).
func (p *plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{gfx.Name, storage.Name}
}

func (p *plugin) Register(registrar *kernel.Registrar, value any) error {
	config, err := resolveConfig(value)
	if err != nil {
		return err
	}
	p.config = config
	registrar.InitResource(&types.OpQueue{})
	registrar.InitResource(types.NewSizedLookup(config))
	registrar.HandleCommand[canvas.ArmDrawsCmd](p.armDrawsCmdImpl)
	registrar.Subscribe[armDrawsOnUpdate](p.armSnapshotOnUpdate).First()
	registrar.Subscribe[drawsOnUpdate](p.snapshotOnUpdate).
		Last().Before[canvas.FlushOnUpdate]()
	registrar.Subscribe[canvas.FlushOnUpdate](p.flush).
		Last().Before[gfx.PresentOnUpdate]()
	registrar.ProvideAdapter[canvas.McpProvider](mcp.Provider(provider{}))
	return nil
}

// Stop completes any snapshot the engine walked away from. A request armed in
// a tick the engine never finishes would otherwise leave its waiter learning
// nothing until its client's idle abort; abandonment is delivered rather than
// merely true.
//
// It touches the snapshot slot directly because by Stop the scheduler has
// stopped and grants no locks, which is also why nothing else can be touching
// it: the host loop has returned and every handler is done.
func (p *plugin) Stop(kernel.Executioner) error {
	p.snapshots.abandon()
	return nil
}

// armSnapshotOnUpdate admits a waiting snapshot to the tick that has just
// begun. It declares no resources: the snapshot slot carries its own lock.
func (p *plugin) armSnapshotOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	return nil, func(kernel.Kernel, app.UpdateEvent) error {
		p.snapshots.beginTick()
		return nil
	}
}

// snapshotOnUpdate renders the tick's recorded operations for whoever armed a
// snapshot, from the Last phase and ordered before canvas's own flush.
//
// That is the only point at which the frame is both complete and still alive.
// ui records into this queue during the earlier phase, so what it drew is
// already here - which is what makes canvas_draws and ui_layout complementary
// rather than redundant - and flushFrame's deferred reset, which empties the
// queue for the next tick, has not run yet.
//
// A Read conflicts only with a writer, and a writer in the earlier phase is
// ordered ahead of this by Last alone. The one recorder it would not see is
// one that itself takes Last and orders only against the flush: the two would
// carry no order between them and merely be serialized by the conflict. That
// is the same shape of gap gfx's own snapshot had to step around, and canvas
// has the better half of it - recording from Last is not how a game records,
// where ui, scene and gameplay all run in the earlier phase.
func (p *plugin) snapshotOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var queue kernel.Read[*canvas.OpQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetRead[*canvas.OpQueue]()
		}, func(_ kernel.Kernel, event app.UpdateEvent) error {
			p.snapshots.record(queue.Get(), event.Tick)
			return nil
		}
}

// Start mounts the built-in filesystem: the shaders and the default font.
// Startup runs after every plugin has registered and before the host loop, so
// both are in place for the first frame without depending on a driver
// publishing an event.
func (p *plugin) Start(k kernel.Executioner) error {
	_, err := k.ExecuteCommand[storage.SetMountCmd](storage.SetMountRequest{Mount: storage.ReadMount{
		Id: builtinMountID, Priority: math.MaxInt, FS: builtinFS,
	}})
	return err
}

func (p *plugin) flush() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var writeQueue kernel.Write[*canvas.OpQueue]
	var gfxQueue kernel.Write[*gfx.OpQueue]
	var gfxResourceQueue kernel.Write[*gfx.ResourceQueue]
	var viewport kernel.Read[*gfx.Viewport]
	var filesystem kernel.Read[storage.FileSystem]
	var lookupResource kernel.Write[*canvas.Lookup]
	return func(access kernel.ResourceAccess) {
			writeQueue = access.GetWrite[*canvas.OpQueue]()
			gfxQueue = access.GetWrite[*gfx.OpQueue]()
			gfxResourceQueue = access.GetWrite[*gfx.ResourceQueue]()
			viewport = access.GetRead[*gfx.Viewport]()
			filesystem = access.GetRead[storage.FileSystem]()
			lookupResource = access.GetWrite[*canvas.Lookup]()
		}, func(kernel.Kernel, app.UpdateEvent) error {
			return p.flushFrame(writeQueue.Get(), gfxQueue.Get(), gfxResourceQueue.Get(),
				viewport.Get(), filesystem.Get(), lookupResource.Get())
		}
}

func (p *plugin) flushFrame(
	write *canvas.OpQueue, gfxWrite *gfx.OpQueue, gfxResources *gfx.ResourceQueue,
	view *gfx.Viewport, filesystem storage.FileSystem, lookup *canvas.Lookup,
) error {
	spriteAtlas := types.LookupSprites(lookup)
	fontAtlas := types.LookupFonts(lookup)
	fonts := types.LookupFontStore(lookup)
	defer types.OpQueueReset(write)
	if !gfxResources.Ready() || view.Width <= 0 || view.Height <= 0 {
		return nil
	}
	types.LookupApplyUnloads(lookup, gfxResources)
	types.LookupInvalidateFontsOnResize(lookup, gfxResources, view)
	if !p.ensureQuad(gfxResources) {
		return nil
	}
	spriteAtlas.BeginFrame()
	fontAtlas.BeginFrame()
	p.layers = p.layers[:0]
	ops := types.OpQueueLayers(write)
	for layerID, value := range ops {
		// A layer that only clears still gets a pass: clearing a target and
		// drawing nothing into it is a legitimate frame, and a clear that
		// migrated to whichever layer happened to draw would land on the wrong
		// attachment.
		if len(value.Ops) > 0 || value.HasColor {
			p.layers = append(p.layers, layerID)
		}
	}
	slices.Sort(p.layers)
	for index, layerID := range p.layers {
		value := ops[layerID]
		first := index == 0 || ops[p.layers[index-1]].Target != value.Target
		last := index == len(p.layers)-1 || ops[p.layers[index+1]].Target != value.Target
		gfxWrite.Pass(canvasPass(layerID, value, first, last))
		surf := layerSurface(value.Target, view)
		transform := resolveLayerTransform(value, surf)
		// The layer's set is read through a pointer into this layer's own copy,
		// so the fingerprints it takes lazily are memoised for the rest of the
		// layer rather than recomputed per draw. At most three, one per slot.
		//
		// A layer that named no set of its own falls through to the queue's,
		// read through a pointer into the queue itself so that its fingerprints
		// are taken once per frame rather than once per layer.
		materials := &value.Materials
		if !value.Materials.Has() {
			materials = types.OpQueueDefaults(write)
		}

		for i := range value.Ops {
			switch value.Ops[i].Kind {
			case types.DrawSpriteKind:
				p.drawSprite(gfxWrite, spriteAtlas, gfxResources, filesystem, surf, transform, value.Ops[i].Clip, value.Ops[i].HasClip, materials, &value.Ops[i].Sprite)
			case types.DrawTextKind:
				p.tris.flush(gfxWrite)
				p.drawText(gfxWrite, spriteAtlas, fontAtlas, gfxResources, filesystem, surf, fonts, transform, value.Ops[i].Clip, value.Ops[i].HasClip, materials, &value.Ops[i].Text)
			case types.DrawTrianglesKind:
				p.batch.flush(gfxWrite, p.quad)
				op := &value.Ops[i].Triangles
				p.drawTriangles(gfxWrite, surf, transform, value.Ops[i].Clip, value.Ops[i].HasClip, types.OpQueueLayout(write, op.LayoutID), materials, op)
			}
		}
		p.batch.flush(gfxWrite, p.quad)
		p.tris.flush(gfxWrite)
	}
	return nil
}

// canvasPass describes the pass one canvas layer draws into. A contiguous run
// of layers sharing a target collapses back to one GPU pass through gfx's merge
// rule, which is why the depth ops are asymmetric rather than uniform: depth
// clears once at the bottom of a run and is discarded once at the top, and every
// pass in between preserves and keeps, which is what the merge predicate
// requires.
//
// first and last bracket the run, not the frame. Depth is per attachment and
// DepthAuto pools one texture per target size, so a layer that preserved depth
// across a target change would z-test against whatever the other target left
// behind. The zero TargetDescr is the screen, which is what a layer nobody gave
// a target means by saying nothing.
func canvasPass(layerID canvas.Layer, value types.LayerOps, first, last bool) gfx.PassDescr {
	desc := gfx.PassDescr{
		Order:  layerID,
		Target: value.Target,
		Depth:  gfx.DepthAuto(),
		Label:  "canvas.layer",
	}
	if first {
		desc.DepthLoad, desc.DepthClear = gfx.LoadClear, 1
	}
	if last {
		// Nothing reads canvas's depth after the run, and discarding saves a
		// tiled GPU the writeback.
		desc.DepthStore = gfx.StoreDiscard
	}
	if value.HasColor {
		desc.Load, desc.Clear = gfx.LoadClear, value.ClearColor
	}
	return desc
}

// drawTriangles adds one triangle op to the batcher. Naming a material is no
// longer a reason to leave it: the material joins the key by fingerprint and the
// parameters join it by value, so two draws that agree on both are one draw and
// an unrecognised parameter name is a key field rather than a bail-out.
func (p *plugin) drawTriangles(gfxWrite *gfx.OpQueue, surf surface, layerTransform m.Mat4, clip m.Rect, hasClip bool, layout []gfx.VertexAttr, materials *types.ScopeMaterials, op *types.TrianglesOp) {
	if hasClip && (clip.Width <= 0 || clip.Height <= 0) {
		return
	}
	shading := p.shadeTriangles(materials, op)
	p.tris.add(gfxWrite, surf.size, op.LayoutID, layout, layerTransform, clip, hasClip, &shading, op.Vertices)
}

// surface is the pixel field one layer draws into: the logical size its
// coordinates map onto, and the physical-to-logical ratio glyph rasterization
// works at.
//
// The screen takes its size from the app viewport and rasterizes text at the
// framebuffer's higher resolution, which is why the two are separate numbers. A
// texture-targeted layer is its own framebuffer: its logical size is the
// texture's own and there is no second ratio, because nothing scales the
// texture on the way to being a texture.
type surface struct {
	size  m.Vec2
	scale float32
}

// layerSurface picks the field a layer measures against. Scene's passAspect
// settles the rule and this follows it: the target's size when the target has
// one, the viewport otherwise. A screen target cannot answer - its swapchain
// view is per-frame and sized on the render thread - and the viewport is the
// number canvas can read on the update thread.
func layerSurface(target gfx.TargetDescr, view *gfx.Viewport) surface {
	if width, height, ok := target.Size(); ok && width > 0 && height > 0 {
		return surface{size: m.Vec2{X: float32(width), Y: float32(height)}, scale: 1}
	}
	scale := float32(1)
	if view.Width > 0 && view.FramebufferWidth > 0 {
		scale = view.FramebufferWidth / view.Width
	}
	return surface{size: m.Vec2{X: view.Width, Y: view.Height}, scale: scale}
}

func resolveLayerTransform(value types.LayerOps, surf surface) m.Mat4 {
	scale, offset := canvas.LayerTransform(value.Window, value.Aspect, surf.size)
	return m.Translation4(offset.X, offset.Y, 0).Mul(m.Scaling4(scale.X, scale.Y, 1))
}

func (p *plugin) ensureQuad(resources *gfx.ResourceQueue) bool {
	if p.quadReady {
		return true
	}
	vertices, indices := unitQuadBytes()
	p.quadVertices = resources.BakeBuffer(vertices, true)
	p.quadIndices = resources.BakeBuffer(indices, true)
	// unitQuadBytes writes uint32 indices, so that is what the descriptor
	// declares. Four vertices would fit in uint16 twice over; canvas's index
	// buffer is six indices long once for the life of the plugin, so there is
	// nothing there to halve.
	p.quad = gfx.MeshIndexed(p.quadVertices, p.quadIndices, gfx.IndexUint32,
		gfx.TopologyTriangleList, gfx.Attr(0, gfx.Float32x2))
	p.quadReady = true
	return true
}

func (p *plugin) drawSprite(gfxWrite *gfx.OpQueue, atlas *types.Atlas, gfxResources *gfx.ResourceQueue, filesystem storage.FileSystem, surf surface, layerTransform m.Mat4, clip m.Rect, hasClip bool, materials *types.ScopeMaterials, op *types.SpriteOp) {
	// Recording order is the contract, so a sprite closes every batch whose
	// pending draw would otherwise land after it. The atlas batch is the only one
	// a path sprite can join; a texture-sourced sprite joins neither, because its
	// draw emits here and now.
	p.tris.flush(gfxWrite)
	if op.HasTexture {
		p.batch.flush(gfxWrite, p.quad)
		p.drawTextureSprite(gfxWrite, surf, layerTransform, clip, hasClip, materials, op)
		return
	}
	t := op.Transform
	if t.TileX || t.TileY {
		if op.Path == "" {
			t.TileX, t.TileY = false, false
		} else {
			p.batch.flush(gfxWrite, p.quad)
			p.drawTiledSprite(gfxWrite, atlas, gfxResources, filesystem, surf, t, layerTransform, clip, hasClip, materials, op)
			return
		}
	}
	entry, ok := atlas.ResolveSprite(op.Path, filesystem, gfxResources)
	if !ok {
		return
	}
	// Naming a material no longer costs a draw: it joins the batch key by
	// fingerprint, so a sprite that names one merges with every sprite that names
	// the same one, and a lone sprite is the one-instance case.
	shading := p.shadeSprite(materials, op.NamedMaterial(), op.Fingerprint, op.Params)
	tint := paramColorOr(op.Params, canvas.TintSlot, m.Color{R: 1, G: 1, B: 1, A: 1})
	keyColor := paramColorOr(op.Params, canvas.KeyColorSlot, types.DefaultKeyColor())
	if t.NineSlice != (canvas.SpriteFrame{}) {
		nineSliceParts(t, entry.Width, entry.Height, func(part canvas.SpriteTransform) {
			p.batchEntry(gfxWrite, surf, entry, part, layerTransform, clip, hasClip, &shading, tint, keyColor)
		})
		return
	}
	p.batchEntry(gfxWrite, surf, entry, t, layerTransform, clip, hasClip, &shading, tint, keyColor)
}

// nineSliceParts splits one transform into the sub-draws a nine-slice is made
// of, each with its own destination rectangle and source Frame, and hands them
// to emit in row-major order. Both sprite sources share it: the corners, sides
// and centre of a nine-slice are the same arithmetic whether the pixels come
// from an atlas entry or from a texture of their own.
//
// Insets that do not fit the source emit nothing, which is the house rule: a
// nine-slice whose borders overlap has no correct picture, and drawing an
// approximate one would hide the authoring mistake.
func nineSliceParts(transform canvas.SpriteTransform, width, height int, emit func(canvas.SpriteTransform)) {
	insets := transform.NineSlice
	if insets.Left < 0 || insets.Right < 0 || insets.Top < 0 || insets.Bottom < 0 ||
		insets.Left+insets.Right >= width || insets.Top+insets.Bottom >= height {
		return
	}
	size := spriteSize(width, height, transform)
	if size.X <= 0 || size.Y <= 0 {
		return
	}
	scale := transform.NineSliceScale
	if scale == 0 {
		scale = 1
	}
	destinationX := splitNineSliceAxis(size.X, float32(insets.Left)*scale, float32(insets.Right)*scale)
	destinationY := splitNineSliceAxis(size.Y, float32(insets.Top)*scale, float32(insets.Bottom)*scale)
	sourceX := [4]int{0, insets.Left, width - insets.Right, width}
	sourceY := [4]int{0, insets.Top, height - insets.Bottom, height}
	for row := 0; row < 3; row++ {
		for column := 0; column < 3; column++ {
			if transform.NineSliceNoCenter && row == 1 && column == 1 {
				continue
			}
			partWidth := destinationX[column+1] - destinationX[column]
			partHeight := destinationY[row+1] - destinationY[row]
			if partWidth <= 0 || partHeight <= 0 {
				continue
			}
			part := transform
			part.Position = m.Vec2{X: transform.Position.X + destinationX[column], Y: transform.Position.Y + destinationY[row]}
			part.Size = m.Vec2{X: partWidth, Y: partHeight}
			part.Origin = m.Vec2{}
			part.Rotation = 0
			part.Frame = canvas.SpriteFrame{
				Left: sourceX[column], Top: sourceY[row],
				Right: width - sourceX[column+1], Bottom: height - sourceY[row+1],
			}
			part.NineSlice = canvas.SpriteFrame{}
			emit(part)
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
func (p *plugin) drawTiledSprite(gfxWrite *gfx.OpQueue, atlas *types.Atlas, gfxResources *gfx.ResourceQueue, filesystem storage.FileSystem, surf surface, t canvas.SpriteTransform, layerTransform m.Mat4, clip m.Rect, hasClip bool, materials *types.ScopeMaterials, op *types.SpriteOp) {
	entry, ok := atlas.ResolveStandalone(op.Path, filesystem, gfxResources)
	if !ok {
		return
	}
	size := t.Size
	tileScale := t.Scale
	if tileScale == 0 {
		tileScale = 1
	}
	if !t.TileX && size.X == 0 {
		size.X = float32(entry.Width) * tileScale
	}
	if !t.TileY && size.Y == 0 {
		size.Y = float32(entry.Height) * tileScale
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
		spanX = size.X / (float32(entry.Width) * tileScale)
	}
	spanY := float32(1)
	if t.TileY {
		spanY = size.Y / (float32(entry.Height) * tileScale)
	}
	tint := spriteTint(op.Params)
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
		gfx.VecParam("canvasViewport", m.Vec4{X: surf.size.X, Y: surf.size.Y, Z: clipEnabled}),
		gfx.MatParam("canvasLayer", layerTransform),
		gfx.VecParam("canvasClip", m.Vec4{X: clip.X, Y: clip.Y, Z: clip.X + clip.Width, W: clip.Y + clip.Height}),
		gfx.TextureParam(canvas.TextureSlot, entry.Texture),
		gfx.SamplerParam(canvas.SamplerSlot, tileSampler(t)),
	)
	p.params = append(p.params, op.Params...)
	material, _, scope := materials.Resolve(types.FamilyTriangles, nil, 0)
	p.params = append(p.params, scope...)
	mesh := gfx.Mesh(gfx.BufferWithBytes(p.tileVertices, true), gfx.TopologyTriangleList, canvas.Vertex{}.VertexLayout()...)
	gfxWrite.Draw(mesh, *material, p.params...)
}

// drawTextureSprite draws a sprite sourcing an arbitrary gfx texture rather than
// an atlas entry.
//
// It cannot join the sprite batch: that shader's binding is a
// texture_2d_array and an arbitrary texture is a texture_2d, so an atlas batch
// and a texture sprite can never be the same draw. It emits its own quad
// instead, the same route the tiled sprite takes, through the texture material
// so the key-colour ramp never touches a rendered image.
func (p *plugin) drawTextureSprite(gfxWrite *gfx.OpQueue, surf surface, layerTransform m.Mat4, clip m.Rect, hasClip bool, materials *types.ScopeMaterials, op *types.SpriteOp) {
	width, height := op.Texture.Size()
	if width <= 0 || height <= 0 {
		// Skip, never substitute. A texture that does not know its size yet - a
		// resource path nothing has baked - has no natural size to draw at and no
		// pixels for a Frame to cut, and a guessed rectangle is worse than none.
		return
	}
	if op.Transform.NineSlice != (canvas.SpriteFrame{}) {
		nineSliceParts(op.Transform, width, height, func(part canvas.SpriteTransform) {
			p.emitTextureQuad(gfxWrite, surf, layerTransform, clip, hasClip, width, height, part, materials, op)
		})
		return
	}
	p.emitTextureQuad(gfxWrite, surf, layerTransform, clip, hasClip, width, height, op.Transform, materials, op)
}

// emitTextureQuad draws one rectangle of a texture-sourced sprite: two triangles
// in the built-in vertex layout, with the tint as vertex colour so it needs no
// parameter the texture material would have to declare.
func (p *plugin) emitTextureQuad(gfxWrite *gfx.OpQueue, surf surface, layerTransform m.Mat4, clip m.Rect, hasClip bool, width, height int, t canvas.SpriteTransform, materials *types.ScopeMaterials, op *types.SpriteOp) {
	size := spriteSize(width, height, t)
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
	uv, ok := textureUV(width, height, t, size)
	if !ok {
		return
	}
	tint := paramColorOr(op.Params, canvas.TintSlot, m.Color{R: 1, G: 1, B: 1, A: 1})
	sine, cosine := sincos(t.Rotation)
	corners := [4]m.Vec2{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1}}
	uvs := [4]m.Vec2{{X: uv.X, Y: uv.Y}, {X: uv.Z, Y: uv.Y}, {X: uv.Z, Y: uv.W}, {X: uv.X, Y: uv.W}}
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
	material, _, scope := materials.Resolve(types.FamilyTexture, op.NamedMaterial(), op.Fingerprint)
	p.params = p.params[:0]
	p.params = append(p.params,
		gfx.VecParam("canvasViewport", m.Vec4{X: surf.size.X, Y: surf.size.Y, Z: clipEnabled}),
		gfx.MatParam("canvasLayer", layerTransform),
		gfx.VecParam("canvasClip", m.Vec4{X: clip.X, Y: clip.Y, Z: clip.X + clip.Width, W: clip.Y + clip.Height}),
		gfx.TextureParam(canvas.TextureSlot, op.Texture),
		gfx.SamplerParam(canvas.SamplerSlot, tileSampler(t)),
	)
	p.params = append(p.params, op.Params...)
	p.params = append(p.params, scope...)
	mesh := gfx.Mesh(gfx.BufferWithBytes(p.tileVertices, true), gfx.TopologyTriangleList, canvas.Vertex{}.VertexLayout()...)
	gfxWrite.Draw(mesh, *material, p.params...)
}

// textureUV resolves the uv rect a texture-sourced sprite samples. Unlike an
// atlas entry the two axes have their own texel size, because a texture is not
// square by construction and is not packed into anything.
//
// A tiled axis runs the uv past 1 by the number of repeats, which is what the
// repeat sampler wraps. Tiling therefore ignores Frame and the flips, exactly as
// the atlas tiling path does: what repeats is the texture, not a window onto it.
func textureUV(width, height int, t canvas.SpriteTransform, size m.Vec2) (m.Vec4, bool) {
	if t.TileX || t.TileY {
		scale := t.Scale
		if scale == 0 {
			scale = 1
		}
		uv := m.Vec4{Z: 1, W: 1}
		if t.TileX {
			uv.Z = size.X / (float32(width) * scale)
		}
		if t.TileY {
			uv.W = size.Y / (float32(height) * scale)
		}
		return uv, true
	}
	uv := m.Vec4{Z: 1, W: 1}
	frame := t.Frame
	if frame != (canvas.SpriteFrame{}) {
		if frame.Left < 0 || frame.Top < 0 || frame.Right < 0 || frame.Bottom < 0 ||
			frame.Left+frame.Right >= width || frame.Top+frame.Bottom >= height {
			return m.Vec4{}, false
		}
		uv.X = float32(frame.Left) / float32(width)
		uv.Y = float32(frame.Top) / float32(height)
		uv.Z = 1 - float32(frame.Right)/float32(width)
		uv.W = 1 - float32(frame.Bottom)/float32(height)
	}
	if t.FlipX {
		uv.X, uv.Z = uv.Z, uv.X
	}
	if t.FlipY {
		uv.Y, uv.W = uv.W, uv.Y
	}
	return uv, true
}

// tileSampler repeats only the axes the transform tiles, so the non-tiled axis
// clamps at its edges instead of wrapping.
func tileSampler(t canvas.SpriteTransform) gfx.SamplerDesc {
	address := func(tile bool) gfx.AddressMode {
		if tile {
			return gfx.AddressRepeat
		}
		return gfx.AddressClamp
	}
	return canvasSampler(address(t.TileX), address(t.TileY), t.Filter)
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

// entrySize resolves the on-screen size of an atlas entry.
func entrySize(entry types.AtlasEntry, transform canvas.SpriteTransform) m.Vec2 {
	return spriteSize(entry.Width, entry.Height, transform)
}

// spriteSize resolves the on-screen size of a source of the given pixel
// dimensions from a transform's explicit size, single-axis size, or uniform
// scale. It is the one place the rule lives, because a texture-sourced sprite
// means by Size and Scale exactly what an atlas-sourced one means.
func spriteSize(width, height int, transform canvas.SpriteTransform) m.Vec2 {
	size := transform.Size
	switch {
	case size.X != 0 && size.Y != 0:
	case size.X != 0:
		size.Y = size.X * float32(height) / float32(width)
	case size.Y != 0:
		size.X = size.Y * float32(width) / float32(height)
	default:
		scale := transform.Scale
		if scale == 0 {
			scale = 1
		}
		size = m.Vec2{X: float32(width) * scale, Y: float32(height) * scale}
	}
	return size
}

// entryUV resolves the sampled uv rect for an atlas entry, applying an optional
// source frame inset and horizontal/vertical flips. It returns false when the
// frame inset is out of bounds.
func entryUV(entry types.AtlasEntry, transform canvas.SpriteTransform) (m.Vec4, bool) {
	uv := entry.UV
	frame := transform.Frame
	if frame != (canvas.SpriteFrame{}) {
		if frame.Left < 0 || frame.Top < 0 || frame.Right < 0 || frame.Bottom < 0 ||
			frame.Left+frame.Right >= entry.Width || frame.Top+frame.Bottom >= entry.Height {
			return m.Vec4{}, false
		}
		uv.X += float32(frame.Left) * entry.TexelSize
		uv.Y += float32(frame.Top) * entry.TexelSize
		uv.Z -= float32(frame.Right) * entry.TexelSize
		uv.W -= float32(frame.Bottom) * entry.TexelSize
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

func (p *plugin) drawGlyphRun(gfxWrite *gfx.OpQueue, atlas *types.Atlas, resources *gfx.ResourceQueue, filesystem storage.FileSystem, surf surface, fonts *types.FontStore, layerTransform m.Mat4, clip m.Rect, hasClip bool, shading *spriteShading, op *types.TextOp) {
	if op.Draw.Size <= 0 || op.Text == "" || op.FontPath == "" {
		return
	}
	// Rasterize glyphs at the on-screen pixel size (layer scale x framebuffer
	// scale), then lay them out in logical units so text stays crisp at any scale.
	px := max(1, int(math.Round(float64(op.Draw.Size*textRasterScale(layerTransform, surf)))))
	face := fonts.Face(filesystem, op.FontPath, px)
	if face == nil {
		return
	}
	toLogical := op.Draw.Size / float32(px)
	y := op.Draw.Position.Y
	for start := 0; start <= len(op.Text); {
		end := start
		for end < len(op.Text) && op.Text[end] != '\n' {
			end++
		}
		line := op.Text[start:end]
		width := types.GlyphLineWidth(atlas, op.FontPath, px, face, line, resources) * toLogical
		x := op.Draw.Position.X
		switch op.Draw.Align {
		case canvas.AlignCenter:
			x -= width / 2
		case canvas.AlignRight:
			x -= width
		}
		var previous rune
		first := true
		for _, character := range line {
			glyph, ok := types.LoadGlyph(atlas, op.FontPath, px, character, face, resources)
			if !ok {
				continue
			}
			if !first {
				x += float32(face.Face.Kern(previous, character)) / 64 * toLogical
			}
			if glyph.Visible {
				transform := canvas.SpriteTransform{
					Position: m.Vec2{X: x + glyph.Offset.X*toLogical, Y: y + glyph.Offset.Y*toLogical},
					Size:     m.Vec2{X: float32(glyph.Entry.Width) * toLogical, Y: float32(glyph.Entry.Height) * toLogical},
				}
				p.batchEntry(gfxWrite, surf, glyph.Entry, transform, layerTransform, clip, hasClip, shading, op.Draw.Color, types.DefaultKeyColor())
			}
			x += glyph.Advance * toLogical
			previous = character
			first = false
		}
		y += face.LineHeight * toLogical
		if end == len(op.Text) {
			break
		}
		start = end + 1
	}
}

// drawText expands inline icons and wraps lines before drawing glyph runs.
func (p *plugin) drawText(gfxWrite *gfx.OpQueue, spriteAtlas, fontAtlas *types.Atlas, resources *gfx.ResourceQueue, filesystem storage.FileSystem, surf surface, fonts *types.FontStore, layerTransform m.Mat4, clip m.Rect, hasClip bool, materials *types.ScopeMaterials, op *types.TextOp) {
	if op.Draw.Size <= 0 || op.Text == "" || op.FontPath == "" {
		return
	}
	px := max(1, int(math.Round(float64(op.Draw.Size*textRasterScale(layerTransform, surf)))))
	face := fonts.Face(filesystem, op.FontPath, px)
	if face == nil {
		return
	}
	// Glyphs and inline icons are both sprite draws, so a text material is a
	// sprite material and one shading covers the whole run.
	shading := p.shadeSprite(materials, op.NamedMaterial(), op.Fingerprint, op.Draw.Params)

	toLogical := op.Draw.Size / float32(px)
	metrics := face.Face.Metrics()
	ascent := float32(metrics.Ascent) / 64 * toLogical
	capHeight := float32(metrics.CapHeight) / 64 * toLogical
	lineHeight := face.LineHeight * toLogical
	measure := func(line []types.InlineSegment) float32 {
		var width float32
		for _, segment := range line {
			if segment.Icon {
				width += p.iconWidth(spriteAtlas, segment.Text, capHeight, filesystem, resources)
				continue
			}
			width += types.GlyphLineWidth(fontAtlas, op.FontPath, px, face, segment.Text, resources) * toLogical
		}
		return width
	}
	lines := types.ParseInlineText(op.Text)
	if op.Draw.WordWrapping && types.ValidWrapWidth(op.Draw.WrapWidth) {
		wrap := p.wrapMeasure(spriteAtlas, resources, filesystem, fonts, op.FontPath, op.Draw.Size, measure)
		lines = types.WrapInlineText(lines, op.Draw.WrapWidth, wrap)
	}
	y := op.Draw.Position.Y
	for _, line := range lines {
		total := measure(line)
		x := op.Draw.Position.X
		switch op.Draw.Align {
		case canvas.AlignCenter:
			x -= total / 2
		case canvas.AlignRight:
			x -= total
		}
		for _, segment := range line {
			if segment.Icon {
				entry, ok := spriteAtlas.ResolveSprite(types.NormalizeResourcePath(segment.Text), filesystem, resources)
				if !ok {
					continue
				}
				width := capHeight * float32(entry.Width) / float32(entry.Height)
				transform := canvas.SpriteTransform{
					Position: m.Vec2{X: x, Y: y + ascent - capHeight},
					Size:     m.Vec2{X: width, Y: capHeight},
				}
				p.batchEntry(gfxWrite, surf, entry, transform, layerTransform, clip, hasClip, &shading, m.Color{R: 1, G: 1, B: 1, A: 1}, types.DefaultKeyColor())
				x += width
				continue
			}
			run := types.TextOp{FontPath: op.FontPath, Text: segment.Text, Draw: canvas.TextDraw{
				Position: m.Vec2{X: x, Y: y}, Size: op.Draw.Size, Color: op.Draw.Color, Align: canvas.AlignLeft,
			}}
			p.drawGlyphRun(gfxWrite, fontAtlas, resources, filesystem, surf, fonts, layerTransform, clip, hasClip, &shading, &run)

			x += types.GlyphLineWidth(fontAtlas, op.FontPath, px, face, segment.Text, resources) * toLogical
		}
		y += lineHeight
	}
}

// wrapMeasure returns the line-width function that decides where drawn text
// breaks. Glyph advances are hinted at the rasterization size, so measuring
// there and scaling back to logical pixels drifts from LookupAccess measurement,
// which uses the face at the logical size. Layout sized the element from that
// measurement, so a line that exactly fills its measured width would otherwise
// spill its last word onto a line the element has no room for. Wrapping with the
// logical face keeps the drawn breaks identical to the measured ones; when that
// face is unavailable the rasterized measurement stands in.
func (p *plugin) wrapMeasure(spriteAtlas *types.Atlas, resources *gfx.ResourceQueue, filesystem storage.FileSystem, fonts *types.FontStore, fontPath string, size float32, fallback func([]types.InlineSegment) float32) func([]types.InlineSegment) float32 {
	px := max(1, int(math.Round(float64(size))))
	face := fonts.Face(filesystem, fontPath, px)
	if face == nil {
		return fallback
	}
	scale := size / float32(px)
	capHeight := float32(face.Face.Metrics().CapHeight) / 64 * scale
	return func(line []types.InlineSegment) float32 {
		var width float32
		for _, segment := range line {
			if segment.Icon {
				width += p.iconWidth(spriteAtlas, segment.Text, capHeight, filesystem, resources)
				continue
			}
			width += types.MeasureLine(face, segment.Text) * scale
		}
		return width
	}
}

// iconWidth resolves an inline icon and returns its cap-height-scaled width.
func (p *plugin) iconWidth(spriteAtlas *types.Atlas, path string, capHeight float32, filesystem storage.FileSystem, resources *gfx.ResourceQueue) float32 {
	entry, ok := spriteAtlas.ResolveSprite(types.NormalizeResourcePath(path), filesystem, resources)
	if !ok {
		return 0
	}
	return capHeight * float32(entry.Width) / float32(entry.Height)
}

// textRasterScale is the world-to-physical-pixel scale for glyph rasterization:
// the layer's uniform scale times the surface's physical-to-logical ratio. Layer
// transforms are rotation-free, so [0] and [5] are the axis scales.
func textRasterScale(layerTransform m.Mat4, surf surface) float32 {
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
	return layerScale * surf.scale
}
