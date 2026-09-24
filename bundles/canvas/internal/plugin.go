package internal

import (
	"encoding/binary"
	"io/fs"
	"math"
	"slices"

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
	config       Config
	quad         gfx.MeshDescr
	quadVertices gfx.BufferDescr
	quadIndices  gfx.BufferDescr
	quadReady    bool
	layers       []Layer
	// quadParams is the shading scratch for the two draws that build their own
	// quad rather than replaying vertices an app recorded: the tiled sprite and
	// the texture sprite. Both join the triangles batcher, which copies what it
	// keeps, so one buffer serves every quad in a frame.
	quadParams   []gfx.ParameterDescr
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

	// frame is the flush's frame-scoped context, held here so a steady-state
	// frame allocates nothing for it. flushFrame fills it and empties it again.
	frame frame
}

// frame is what one flush holds for the whole of a frame and every draw under it
// needs: the kernel a load reports through, the read filesystem, the resource
// queue an upload goes to, and the Lookup the caches and the packers live on.
//
// The filesystem is boxed into its interface once here rather than at each
// resolve: storage.FileSystem is a struct, and handing one to an interface
// parameter allocates every time it is done, which on the per-sprite path is an
// allocation per sprite per frame.
//
// None of the four may outlive the handler that granted them, so flushFrame
// empties this again on the way out. A kernel read out of an emptied frame
// panics, which is what makes a leak loud rather than stale.
type frame struct {
	k         kernel.Kernel
	fsys      fs.FS
	resources *gfx.ResourceQueue
	lookup    *Lookup
}

// sprite resolves one recorded sprite to its atlas entry. A path canvas will not
// open was marked invalid where it entered the queue, so it is reported here and
// never reaches a cache; a zero entry, however it arose, draws nothing.
// tileX and tileY say which axes the draw will wrap its uv on, which picks the
// gutter fill of the entry it gets.
func (fr *frame) sprite(op *SpriteOp, tileX, tileY bool) AtlasEntry {
	if op.InvalidPath {
		ReportInvalidSpritePath(fr.k, op.Path)
		return AtlasEntry{}
	}
	return LookupResolveSprite(fr.lookup, fr.k, op.Path, fr.fsys, fr.resources, tileX, tileY)
}

// icon resolves an inline icon's atlas entry. An icon path enters canvas inside
// the text it is written in rather than at a Sprite call, so this is where it is
// validated.
func (fr *frame) icon(path string) AtlasEntry {
	recorded, invalid := SpritePath(path)
	if invalid {
		ReportInvalidSpritePath(fr.k, recorded)
		return AtlasEntry{}
	}
	return LookupResolveSprite(fr.lookup, fr.k, recorded, fr.fsys, fr.resources, false, false)
}

// face bakes (or reuses) one font face at a rasterization size. The path is the
// one the queue recorded, and Text resolved the empty path to the built-in
// default when it recorded it, so nothing arrives here unnamed.
func (fr *frame) face(path string, px int) *Font {
	return LookupFace(fr.lookup, fr.k, path, px, fr.fsys)
}

// New creates the canvas plugin. Configure it with a canvas.Config under
// canvas.Name.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins canvas requires: gfx (for the draw
// pipeline), storage (which hosts its shader filesystem mount) and app, whose
// TimeCmd the canvas_draws capability dispatches.
func (p *plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{app.Name, gfx.Name, storage.Name}
}

func (p *plugin) Register(registrar *kernel.Registrar, value any) error {
	config, err := resolveConfig(value)
	if err != nil {
		return err
	}
	p.config = config
	registrar.InitResource(&OpQueue{})
	registrar.InitResource(NewSizedLookup(config))
	registrar.HandleCommand[ArmDrawsCmd](p.armDrawsCmdImpl)
	registrar.Subscribe[armDrawsOnUpdate](p.armSnapshotOnUpdate).First()
	registrar.Subscribe[drawsOnUpdate](p.snapshotOnUpdate).
		Last().Before[FlushOnUpdate]()
	registrar.Subscribe[FlushOnUpdate](p.flush).
		Last().Before[gfx.PresentOnUpdate]()
	registrar.ProvideAdapter[McpProvider](mcp.Provider(provider{}))
	// The built-in shaders and default font sit above every other mount, so a
	// game's own files never shadow them. storage installs the mount at its
	// Start, ahead of every plugin that depends on it, so both are in place for
	// the first frame.
	registrar.ProvideAdapter[StorageReadMount](storage.ReadMount{
		Id: builtinMountID, Priority: math.MaxInt, FS: builtinFS,
	})
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
	return nil, func(kernel.Kernel, app.UpdateEvent) {
		p.snapshots.beginTick()
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
	var queue kernel.Read[*OpQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetRead[*OpQueue]()
		}, func(_ kernel.Kernel, event app.UpdateEvent) {
			p.snapshots.record(queue.Get(), event.Tick)
		}
}

func (p *plugin) flush() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var writeQueue kernel.Write[*OpQueue]
	var gfxQueue kernel.Write[*gfx.OpQueue]
	var gfxResourceQueue kernel.Write[*gfx.ResourceQueue]
	var viewport kernel.Read[*gfx.Viewport]
	var filesystem kernel.Read[storage.FileSystem]
	var lookupResource kernel.Write[*Lookup]
	return func(access kernel.ResourceAccess) {
			writeQueue = access.GetWrite[*OpQueue]()
			gfxQueue = access.GetWrite[*gfx.OpQueue]()
			gfxResourceQueue = access.GetWrite[*gfx.ResourceQueue]()
			viewport = access.GetRead[*gfx.Viewport]()
			filesystem = access.GetRead[storage.FileSystem]()
			lookupResource = access.GetWrite[*Lookup]()
		}, func(k kernel.Kernel, _ app.UpdateEvent) {
			p.flushFrame(k, writeQueue.Get(), gfxQueue.Get(), gfxResourceQueue.Get(),
				viewport.Get(), filesystem.Get(), lookupResource.Get())
		}
}

func (p *plugin) flushFrame(
	k kernel.Kernel, write *OpQueue, gfxWrite *gfx.OpQueue, gfxResources *gfx.ResourceQueue,
	view *gfx.Viewport, filesystem storage.FileSystem, lookup *Lookup,
) error {
	defer OpQueueReset(write)
	if !gfxResources.Ready() || view.Width <= 0 || view.Height <= 0 {
		return nil
	}
	LookupInvalidateFontsOnResize(lookup, k, gfxResources, view)
	if !p.ensureQuad(gfxResources) {
		return nil
	}
	p.frame = frame{k: k, fsys: filesystem, resources: gfxResources, lookup: lookup}
	fr := &p.frame
	// Nothing the frame carries may outlive this handler, and a kernel read out
	// of the zero value panics, so a draw that kept hold of one fails loudly
	// rather than reporting through a stale kernel.
	defer func() { p.frame = frame{} }()
	// The white texel is packed before any layer's ops rather than lazily beside
	// the first sprite that needs it. The atlas batch is keyed on the texture, so
	// a texel that landed in a second array would split every fill away from
	// every sprite it draws with; reserving it first is what keeps them in one
	// instanced draw.
	//
	// A frame whose texel does not pack draws nothing, which is what the
	// reservation inside the old insert did by refusing every sprite behind it.
	// Nothing reaches this today - reserving first means the packer has room when
	// the texel is asked for, and no verb here frees the texel on its own - so it
	// holds the invariant rather than a path. It is here because the alternative
	// is half a frame: every fill, line and stroke vanishing while the sprites
	// beside them still draw, which hides the misconfiguration instead of showing
	// it.
	if white := LookupResolveSprite(fr.lookup, fr.k, "", fr.fsys, fr.resources, false, false); white.Width <= 0 {
		return nil
	}
	p.layers = p.layers[:0]
	ops := OpQueueLayers(write)
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
			materials = OpQueueDefaults(write)
		}

		for i := range value.Ops {
			switch value.Ops[i].Kind {
			case DrawSpriteKind:
				p.drawSprite(gfxWrite, fr, surf, transform, value.Ops[i].Clip, value.Ops[i].HasClip, materials, &value.Ops[i].Sprite)
			case DrawTextKind:
				p.tris.flush(gfxWrite)
				p.drawText(gfxWrite, fr, surf, transform, value.Ops[i].Clip, value.Ops[i].HasClip, materials, &value.Ops[i].Text)
			case DrawTrianglesKind:
				p.batch.flush(gfxWrite, p.quad)
				op := &value.Ops[i].Triangles
				p.drawTriangles(gfxWrite, surf, transform, value.Ops[i].Clip, value.Ops[i].HasClip, OpQueueLayout(write, op.LayoutID), materials, op)
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
func canvasPass(layerID Layer, value LayerOps, first, last bool) gfx.PassDescr {
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
func (p *plugin) drawTriangles(gfxWrite *gfx.OpQueue, surf surface, layerTransform m.Mat4, clip m.Rect, hasClip bool, layout []gfx.VertexAttr, materials *ScopeMaterials, op *TrianglesOp) {
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

func resolveLayerTransform(value LayerOps, surf surface) m.Mat4 {
	scale, offset := LayerTransform(value.Window, value.Aspect, surf.size)
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

func (p *plugin) drawSprite(gfxWrite *gfx.OpQueue, fr *frame, surf surface, layerTransform m.Mat4, clip m.Rect, hasClip bool, materials *ScopeMaterials, op *SpriteOp) {
	// Recording order is the contract, so a sprite closes every batch whose
	// pending draw would otherwise land after it - but only the batches it
	// cannot itself join. A texture-sourced or tiled sprite is a quad in the
	// built-in vertex layout and joins the triangles batch, so flushing that
	// batch here would close the very run it is about to extend; it closes the
	// atlas batch instead. A path sprite is the mirror image: it joins the atlas
	// batch and closes the triangles one, which is why that flush sits below
	// these two returns rather than above them.
	if op.HasTexture {
		p.batch.flush(gfxWrite, p.quad)
		p.drawTextureSprite(gfxWrite, fr, surf, layerTransform, clip, hasClip, materials, op)
		return
	}
	t := op.Transform
	if t.TileX || t.TileY {
		// Tiling repeats a window onto an atlas entry, wrapped in the fragment
		// stage - so a tiled sprite is an ordinary sprite that carries repeat
		// counts, and it takes the ordinary path below. Two cases cannot.
		//
		// The generated texel is one texel: there is no window to repeat and no
		// file to name it by, so tiling it is dropped as it always was.
		//
		// A path canvas will not open is not measured either - it has no header
		// to read - and goes on to the resolve below, which is where an invalid
		// path has always been reported.
		//
		// An image whose padded rectangle is larger than an atlas page has no
		// entry to wrap inside, and keeps the standalone repeat texture it has
		// always had. The route is decided from the header rather than by
		// packing and recovering, because both packer refusals are terminal and
		// have already reported by the time a fallback could see them.
		switch {
		case op.Path == "":
			t.TileX, t.TileY = false, false
		case !op.InvalidPath && !LookupSpriteFitsAtlas(fr.lookup, fr.k, op.Path, fr.fsys):
			p.batch.flush(gfxWrite, p.quad)
			p.drawTiledSprite(gfxWrite, fr, surf, t, layerTransform, clip, hasClip, materials, op)
			return
		}
	}
	entry := fr.sprite(op, t.TileX, t.TileY)
	if entry.Width <= 0 || entry.Height <= 0 {
		return
	}
	// The frame is checked here, before any size is resolved, because this is
	// where the path and the kernel are both in hand - the altitude the tiled
	// sprite already reports an unusable path from. Everything downstream may
	// then assume a frame that fits.
	//
	// A bad frame reports alone. The nine-slice insets are measured into the
	// frame's extent, so on a frame that does not fit their verdict is derived
	// from a number that means nothing, and reporting it too would raise a
	// second error that vanishes when the first is fixed.
	if !frameFits(entry.Width, entry.Height, t.Frame) {
		ReportInvalidSpriteFrame(fr.k, op.Path, entry.Width, entry.Height, t.Frame)
		return
	}
	// The sprite is going to the atlas batch, which no triangle draw can join,
	// so the pending triangles run closes here. It closes after the resolve
	// rather than before it: a sprite whose atlas entry does not resolve draws
	// nothing, and ending a batch for a draw that never happens costs a draw
	// for no ordering anyone can observe.
	p.tris.flush(gfxWrite)
	// Naming a material no longer costs a draw: it joins the batch key by
	// fingerprint, so a sprite that names one merges with every sprite that names
	// the same one, and a lone sprite is the one-instance case.
	shading := p.shadeSprite(materials, op.NamedMaterial(), op.Fingerprint, op.Params)
	tint := paramColorOr(op.Params, TintSlot, m.Color{R: 1, G: 1, B: 1, A: 1})
	keyColor := paramColorOr(op.Params, KeyColorSlot, DefaultKeyColor())
	if t.NineSlice != (SpriteFrame{}) {
		frameWidth, frameHeight := framedSource(entry.Width, entry.Height, t.Frame)
		if !insetsFit(frameWidth, frameHeight, t.NineSlice) {
			ReportInvalidSpriteNineSlice(fr.k, op.Path, frameWidth, frameHeight, t.Frame, t.NineSlice)
			return
		}
		nineSliceParts(t, entry.Width, entry.Height, func(part SpriteTransform) {
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
// A nine-slice over a framed sprite slices the frame: the insets are measured
// into the frame's extent and offset by it, so what the parts sample is the
// frame's span even though the Frame each one carries is absolute against the
// source. The frame used to be overwritten here and the caller's discarded, so
// the two features silently did not compose.
//
// Both the frame and the insets are assumed to fit. A nine-slice whose borders
// overlap has no correct picture and an approximate one would hide the
// authoring mistake, so it is refused - but at drawSprite and
// drawTextureSprite, which can name the sheet in the report, rather than by
// returning quietly from here.
func nineSliceParts(transform SpriteTransform, width, height int, emit func(SpriteTransform)) {
	frame := transform.Frame
	frameWidth, frameHeight := framedSource(width, height, frame)
	insets := transform.NineSlice
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
	// The source columns and rows are cut out of the frame and then said in the
	// source's own coordinates, because a part's Frame is absolute: what a part
	// samples is the frame's span, and where it says it is is the whole sheet.
	sourceX := [4]int{0, insets.Left, frameWidth - insets.Right, frameWidth}
	sourceY := [4]int{0, insets.Top, frameHeight - insets.Bottom, frameHeight}
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
			part.Frame = SpriteFrame{
				Left: frame.Left + sourceX[column], Top: frame.Top + sourceY[row],
				Right: width - (frame.Left + sourceX[column+1]), Bottom: height - (frame.Top + sourceY[row+1]),
			}
			part.NineSlice = SpriteFrame{}
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
func (p *plugin) drawTiledSprite(gfxWrite *gfx.OpQueue, fr *frame, surf surface, t SpriteTransform, layerTransform m.Mat4, clip m.Rect, hasClip bool, materials *ScopeMaterials, op *SpriteOp) {
	if op.InvalidPath {
		ReportInvalidSpritePath(fr.k, op.Path)
		return
	}
	entry := LookupResolveStandalone(fr.lookup, fr.k, op.Path, fr.fsys, fr.resources)
	if entry.Width <= 0 || entry.Height <= 0 {
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
	if hasClip && (clip.Width <= 0 || clip.Height <= 0) {
		return
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
	// A tiled sprite keeps FamilyTriangles: it is artwork, and artwork wants the
	// key-colour ramp. Only the texture sprite below is exempt.
	material, fingerprint, scope := materials.Resolve(FamilyTriangles, nil, 0)
	shading := p.shadeQuad(material, fingerprint, entry.Texture, tileSampler(t), op.Params, scope)
	p.tris.add(gfxWrite, surf.size, builtinQuadLayoutID, Vertex{}.VertexLayout(),
		layerTransform, clip, hasClip, &shading, p.tileVertices)
}

// drawTextureSprite draws a sprite sourcing an arbitrary gfx texture rather than
// an atlas entry.
//
// It cannot join the sprite batch: that shader's binding is a
// texture_2d_array and an arbitrary texture is a texture_2d, so an atlas batch
// and a texture sprite can never be the same draw. It builds its own quad
// instead, the same route the tiled sprite takes, through the texture material
// so the key-colour ramp never touches a rendered image.
//
// The quad still batches - it joins the triangles batcher, and a nine-slice is
// one draw rather than nine - because what keeps it off the sprite path is the
// binding type, not anything about the geometry. Two quads over one texture and
// one sampler are one draw; a second texture splits them.
func (p *plugin) drawTextureSprite(gfxWrite *gfx.OpQueue, fr *frame, surf surface, layerTransform m.Mat4, clip m.Rect, hasClip bool, materials *ScopeMaterials, op *SpriteOp) {
	width, height := op.Texture.Size()
	if width <= 0 || height <= 0 {
		// Skip, never substitute. A texture that does not know its size yet - a
		// resource path nothing has baked - has no natural size to draw at and no
		// pixels for a Frame to cut, and a guessed rectangle is worse than none.
		return
	}
	// The same refusal as the atlas path, at the same altitude and for the same
	// reason: a frame is checked once, before a size is resolved, while there is
	// still something to name in the report.
	if !frameFits(width, height, op.Transform.Frame) {
		ReportInvalidTextureFrame(fr.k, op.Texture, width, height, op.Transform.Frame)
		return
	}
	if op.Transform.NineSlice != (SpriteFrame{}) {
		frameWidth, frameHeight := framedSource(width, height, op.Transform.Frame)
		if !insetsFit(frameWidth, frameHeight, op.Transform.NineSlice) {
			ReportInvalidTextureNineSlice(fr.k, op.Texture, frameWidth, frameHeight, op.Transform.Frame, op.Transform.NineSlice)
			return
		}
		nineSliceParts(op.Transform, width, height, func(part SpriteTransform) {
			p.emitTextureQuad(gfxWrite, surf, layerTransform, clip, hasClip, width, height, part, materials, op)
		})
		return
	}
	p.emitTextureQuad(gfxWrite, surf, layerTransform, clip, hasClip, width, height, op.Transform, materials, op)
}

// emitTextureQuad adds one rectangle of a texture-sourced sprite to the triangles
// batcher: two triangles in the built-in vertex layout, with the tint as vertex
// colour so it needs no parameter the texture material would have to declare -
// and so that two quads differing only in tint still merge.
func (p *plugin) emitTextureQuad(gfxWrite *gfx.OpQueue, surf surface, layerTransform m.Mat4, clip m.Rect, hasClip bool, width, height int, t SpriteTransform, materials *ScopeMaterials, op *SpriteOp) {
	size := spriteSize(width, height, t)
	if size.X == 0 || size.Y == 0 {
		return
	}
	if hasClip && (clip.Width <= 0 || clip.Height <= 0) {
		return
	}
	uv, ok := textureUV(width, height, t, size)
	if !ok {
		return
	}
	tint := paramColorOr(op.Params, TintSlot, m.Color{R: 1, G: 1, B: 1, A: 1})
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
	material, fingerprint, scope := materials.Resolve(FamilyTexture, op.NamedMaterial(), op.Fingerprint)
	shading := p.shadeQuad(material, fingerprint, op.Texture, tileSampler(t), op.Params, scope)
	p.tris.add(gfxWrite, surf.size, builtinQuadLayoutID, Vertex{}.VertexLayout(),
		layerTransform, clip, hasClip, &shading, p.tileVertices)
}

// textureUV resolves the uv rect a texture-sourced sprite samples. Unlike an
// atlas entry the two axes have their own texel size, because a texture is not
// square by construction and is not packed into anything.
//
// A tiled axis runs the uv past 1 by the number of repeats, which is what the
// repeat sampler wraps. Tiling therefore ignores Frame and the flips, exactly as
// the atlas tiling path does: what repeats is the texture, not a window onto it.
func textureUV(width, height int, t SpriteTransform, size m.Vec2) (m.Vec4, bool) {
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
	if frame != (SpriteFrame{}) {
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
func tileSampler(t SpriteTransform) gfx.SamplerDesc {
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
func entrySize(entry AtlasEntry, transform SpriteTransform) m.Vec2 {
	if transform.TileX || transform.TileY {
		return tiledSize(entry, transform)
	}
	return spriteSize(entry.Width, entry.Height, transform)
}

// tiledSize resolves the on-screen size of a tiled sprite, which does not mean
// by Size what an untiled one means.
//
// A tiled axis must be told how long it is. spriteSize would derive a missing
// axis from the one it was given, by aspect - and "repeat until the aspect ratio
// matches" is not something any caller means, so a tiled axis with no Size draws
// nothing rather than guessing at a length.
//
// A non-tiled axis defaults to one tile, which is the framed tile: a Frame says
// which texels the sprite draws, so after it says how many, and it is the tile
// that repeats rather than the sheet it was cut from.
func tiledSize(entry AtlasEntry, transform SpriteTransform) m.Vec2 {
	tileWidth, tileHeight := framedSource(entry.Width, entry.Height, transform.Frame)
	scale := transform.Scale
	if scale == 0 {
		scale = 1
	}
	size := transform.Size
	if !transform.TileX && size.X == 0 {
		size.X = float32(tileWidth) * scale
	}
	if !transform.TileY && size.Y == 0 {
		size.Y = float32(tileHeight) * scale
	}
	return size
}

// tiledRepeat is how many times the framed tile repeats on each axis, which the
// fragment stage wraps the uv by. An axis that does not tile repeats once.
//
// One is load-bearing rather than merely correct: the wrap multiplies the quad
// coordinate by this before taking its fractional part, so a zero here would
// collapse the sampled rectangle to its top-left corner and paint every sprite,
// glyph and fill in one texel's colour.
func tiledRepeat(entry AtlasEntry, transform SpriteTransform, size m.Vec2) m.Vec2 {
	repeat := m.Vec2{X: 1, Y: 1}
	if !transform.TileX && !transform.TileY {
		return repeat
	}
	tileWidth, tileHeight := framedSource(entry.Width, entry.Height, transform.Frame)
	scale := transform.Scale
	if scale == 0 {
		scale = 1
	}
	if transform.TileX && tileWidth > 0 {
		repeat.X = size.X / (float32(tileWidth) * scale)
	}
	if transform.TileY && tileHeight > 0 {
		repeat.Y = size.Y / (float32(tileHeight) * scale)
	}
	return repeat
}

// spriteSize resolves the on-screen size of a source of the given pixel
// dimensions from a transform's explicit size, single-axis size, or uniform
// scale. It is the one place the rule lives, because a texture-sourced sprite
// means by Size and Scale exactly what an atlas-sourced one means.
//
// A Frame narrows the source first: it says which texels the sprite draws, so
// it says how many, and Scale means one framed texel per world unit. Sizing
// from the whole sheet instead stretched a sub-rect over the full image, which
// is the one thing a transform whose uv is already inset can never want. The
// frame is assumed to fit - drawSprite and drawTextureSprite refuse and report
// one that does not before any size is resolved.
func spriteSize(width, height int, transform SpriteTransform) m.Vec2 {
	width, height = framedSource(width, height, transform.Frame)
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
func entryUV(entry AtlasEntry, transform SpriteTransform) (m.Vec4, bool) {
	uv := entry.UV
	frame := transform.Frame
	if frame != (SpriteFrame{}) {
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

func (p *plugin) drawGlyphRun(gfxWrite *gfx.OpQueue, fr *frame, surf surface, layerTransform m.Mat4, clip m.Rect, hasClip bool, shading *spriteShading, op *TextOp) {
	if op.Draw.Size <= 0 || op.Text == "" || op.FontPath == "" {
		return
	}
	// Rasterize glyphs at the on-screen pixel size (layer scale x framebuffer
	// scale), then lay them out in logical units so text stays crisp at any scale.
	px := max(1, int(math.Round(float64(op.Draw.Size*textRasterScale(layerTransform, surf)))))
	face := fr.face(op.FontPath, px)
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
		width := GlyphLineWidth(fr.lookup, face, line, fr.resources) * toLogical
		x := op.Draw.Position.X
		switch op.Draw.Align {
		case AlignCenter:
			x -= width / 2
		case AlignRight:
			x -= width
		}
		var previous rune
		first := true
		for _, character := range line {
			glyph, ok := LoadGlyph(fr.lookup, character, face, fr.resources)
			if !ok {
				continue
			}
			if !first {
				x += float32(face.Face.Kern(previous, character)) / 64 * toLogical
			}
			if glyph.Visible {
				transform := SpriteTransform{
					Position: m.Vec2{X: x + glyph.Offset.X*toLogical, Y: y + glyph.Offset.Y*toLogical},
					Size:     m.Vec2{X: float32(glyph.Entry.Width) * toLogical, Y: float32(glyph.Entry.Height) * toLogical},
				}
				p.batchEntry(gfxWrite, surf, glyph.Entry, transform, layerTransform, clip, hasClip, shading, op.Draw.Color, DefaultKeyColor())
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
func (p *plugin) drawText(gfxWrite *gfx.OpQueue, fr *frame, surf surface, layerTransform m.Mat4, clip m.Rect, hasClip bool, materials *ScopeMaterials, op *TextOp) {
	if op.Draw.Size <= 0 || op.Text == "" || op.FontPath == "" {
		return
	}
	px := max(1, int(math.Round(float64(op.Draw.Size*textRasterScale(layerTransform, surf)))))
	face := fr.face(op.FontPath, px)
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
	measure := func(line []InlineSegment) float32 {
		var width float32
		for _, segment := range line {
			if segment.Icon {
				width += p.iconWidth(fr, segment.Text, capHeight)
				continue
			}
			width += GlyphLineWidth(fr.lookup, face, segment.Text, fr.resources) * toLogical
		}
		return width
	}
	lines := ParseInlineText(op.Text)
	if op.Draw.WordWrapping && ValidWrapWidth(op.Draw.WrapWidth) {
		wrap := p.wrapMeasure(fr, op.FontPath, op.Draw.Size, measure)
		lines = WrapInlineText(lines, op.Draw.WrapWidth, wrap)
	}
	y := op.Draw.Position.Y
	for _, line := range lines {
		total := measure(line)
		x := op.Draw.Position.X
		switch op.Draw.Align {
		case AlignCenter:
			x -= total / 2
		case AlignRight:
			x -= total
		}
		for _, segment := range line {
			if segment.Icon {
				entry := fr.icon(segment.Text)
				if entry.Width <= 0 || entry.Height <= 0 {
					continue
				}
				width := capHeight * float32(entry.Width) / float32(entry.Height)
				transform := SpriteTransform{
					Position: m.Vec2{X: x, Y: y + ascent - capHeight},
					Size:     m.Vec2{X: width, Y: capHeight},
				}
				// The run's alpha and not its colour. Rgb is what colour the ink
				// is, which a glyph needs because its atlas entry is RGB=255
				// with coverage in alpha, and an icon does not because its
				// texel is already the artwork. Alpha is how present the run
				// is, which is true of anything drawn.
				p.batchEntry(gfxWrite, surf, entry, transform, layerTransform, clip, hasClip, &shading, m.Color{R: 1, G: 1, B: 1, A: op.Draw.Color.A}, DefaultKeyColor())
				x += width
				continue
			}
			run := TextOp{FontPath: op.FontPath, Text: segment.Text, Draw: TextDraw{
				Position: m.Vec2{X: x, Y: y}, Size: op.Draw.Size, Color: op.Draw.Color, Align: AlignLeft,
			}}
			p.drawGlyphRun(gfxWrite, fr, surf, layerTransform, clip, hasClip, &shading, &run)

			x += GlyphLineWidth(fr.lookup, face, segment.Text, fr.resources) * toLogical
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
func (p *plugin) wrapMeasure(fr *frame, fontPath string, size float32, fallback func([]InlineSegment) float32) func([]InlineSegment) float32 {
	px := max(1, int(math.Round(float64(size))))
	face := fr.face(fontPath, px)
	if face == nil {
		return fallback
	}
	scale := size / float32(px)
	capHeight := float32(face.Face.Metrics().CapHeight) / 64 * scale
	return func(line []InlineSegment) float32 {
		var width float32
		for _, segment := range line {
			if segment.Icon {
				width += p.iconWidth(fr, segment.Text, capHeight)
				continue
			}
			width += MeasureLine(face, segment.Text) * scale
		}
		return width
	}
}

// iconWidth resolves an inline icon and returns its cap-height-scaled width.
func (p *plugin) iconWidth(fr *frame, path string, capHeight float32) float32 {
	entry := fr.icon(path)
	if entry.Width <= 0 || entry.Height <= 0 {
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

// framedSource narrows a source's pixel dimensions to the sub-rect a Frame
// selects. A zero Frame selects the whole source, which is why the common case
// costs a comparison and nothing else.
func framedSource(width, height int, frame SpriteFrame) (int, int) {
	if frame == (SpriteFrame{}) {
		return width, height
	}
	return width - frame.Left - frame.Right, height - frame.Top - frame.Bottom
}

// frameFits reports whether a Frame selects a non-empty sub-rect of a source of
// the given dimensions. It is the condition entryUV and textureUV already
// applied silently, lifted out so the draw can refuse and say why.
func frameFits(width, height int, frame SpriteFrame) bool {
	if frame == (SpriteFrame{}) {
		return true
	}
	return frame.Left >= 0 && frame.Top >= 0 && frame.Right >= 0 && frame.Bottom >= 0 &&
		frame.Left+frame.Right < width && frame.Top+frame.Bottom < height
}

// insetsFit reports whether nine-slice insets leave a non-empty middle in a
// source of the given dimensions. The dimensions are the frame's, not the
// sheet's: a nine-slice over a framed sprite slices the frame.
func insetsFit(width, height int, insets SpriteFrame) bool {
	return insets.Left >= 0 && insets.Right >= 0 && insets.Top >= 0 && insets.Bottom >= 0 &&
		insets.Left+insets.Right < width && insets.Top+insets.Bottom < height
}
