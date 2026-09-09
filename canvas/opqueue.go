package canvas

import (
	"math"
	pathpkg "path"
	"reflect"
	"strings"
	"unsafe"

	"github.com/dvoyni/cog/m"

	"github.com/dvoyni/cog/gfx"
)

type spriteOp struct {
	path      string
	texture   gfx.TextureDescr
	params    []gfx.ParameterDescr
	material  gfx.MaterialDescr
	transform SpriteTransform
	// fingerprint is material's batch key, taken once here beside the clone into
	// the material arena rather than per draw at flush. It survives the clone by
	// design, because it hashes the descriptor's content and not its address.
	fingerprint uint64

	// hasTexture marks a sprite sourced from a gfx texture rather than a
	// resource path. It cannot join the atlas batch - that binding is a
	// texture_2d_array and an arbitrary texture is a texture_2d - so it draws
	// as its own quad through the texture material.
	hasTexture bool
	// hasMaterial says whether there is a material here to take the address of.
	// It is not a batch key field: a draw naming no material resolves to the
	// layer's set or the built-in, and both carry a fingerprint of their own.
	hasMaterial bool
}

// namedMaterial reports the material this op named, or nil where it named none
// and its family's slot has to supply one.
func (op *spriteOp) namedMaterial() *gfx.MaterialDescr {
	if !op.hasMaterial {
		return nil
	}
	return &op.material
}

type textOp struct {
	fontPath string
	text     string
	draw     TextDraw
	// material is the clone of draw.Material into the queue's arena, with its
	// fingerprint, on the same terms a sprite op's is.
	material    gfx.MaterialDescr
	fingerprint uint64
	hasMaterial bool
}

func (op *textOp) namedMaterial() *gfx.MaterialDescr {
	if !op.hasMaterial {
		return nil
	}
	return &op.material
}

type trianglesOp struct {
	vertices    []byte
	params      []gfx.ParameterDescr
	material    gfx.MaterialDescr
	fingerprint uint64
	layoutID    int

	// builtinLayout marks a list recorded with the built-in Vertex type, so
	// inspection can reinterpret the bytes without a layout description.
	builtinLayout bool
	hasMaterial   bool
	// unkeyed selects the built-in texture material over the built-in triangle
	// one: DrawTexture samples its texture as it is, where DrawTriangles runs
	// every texel through the key-colour ramp.
	unkeyed bool
}

func (op *trianglesOp) namedMaterial() *gfx.MaterialDescr {
	if !op.hasMaterial {
		return nil
	}
	return &op.material
}

type drawOpKind uint8

const (
	drawSprite drawOpKind = iota
	drawText
	drawTriangles
)

type drawOp struct {
	sprite    spriteOp
	text      textOp
	triangles trianglesOp
	clip      m.Rect

	kind    drawOpKind
	hasClip bool
}

type opQueue struct {
	ops           map[Layer]layer
	paramArena    []gfx.ParameterDescr
	materialArena []gfx.ParameterDescr
	vertexArena   []byte
	layoutIDs     map[reflect.Type]int
	layouts       [][]gfx.VertexAttr
	// clip and hasClip are the recording-time clip cursor snapshotted into each op.
	clip    m.Rect
	hasClip bool
}

type layer struct {
	ops    []drawOp
	window m.Rect
	aspect AspectMode
	// target is where this layer draws, and its zero value is the screen. A run
	// of adjacent layers naming one target collapses into a single GPU pass, so
	// giving a layer a target costs a pass only where the target changes.
	target gfx.TargetDescr
	// clearColor and hasColor are this layer's own clear. It is per layer rather
	// than per frame because a frame that renders one layer into a texture wants
	// that texture cleared and the screen cleared too, and the two clears land on
	// different attachments.
	clearColor m.Color
	hasColor   bool
	// materials is the set SetLayerMaterial put on this layer, supplying a slot
	// to every draw under it that named no material of its own.
	materials layerMaterials
}

// Clear fills one layer's target, before anything that layer draws. It is
// positioned rather than frame-global because a frame-global clear cannot
// survive anything rendering below canvas: a camera ordered under the clear
// would be wiped. Naming the layer keeps the clear where the caller put it even
// on a frame where that layer draws nothing.
//
// It is per layer rather than one per frame because layers no longer share an
// attachment: a layer with a target of its own is cleared independently of the
// screen, and both clears belong to the same frame.
func (w *opQueue) Clear(layerID Layer, color m.Color) {
	value := w.layer(layerID)
	value.clearColor, value.hasColor = color, true
	w.setLayer(layerID, value)
}

// SetLayerTarget renders one layer into a gfx texture instead of the screen.
//
// Canvas mints nothing and names nothing here: the target is the gfx handle the
// caller allocated, passed through untouched, because minting a texture takes
// the gfx queue and a canvas recorder does not hold it. Take a frame-local one
// from gfx.OpQueue.TemporaryTarget, or a durable one from
// gfx.ResourceQueue.AllocateRenderTarget when the contents must outlive the
// frame. The texture that comes back with it is an ordinary gfx.TextureDescr,
// so the same handle serves DrawTexture, SpriteTexture and a scene material
// alike.
//
// The layer measures against the target's size rather than the viewport, and
// carries its own Clear. Passing the zero TargetDescr puts the layer back on
// the screen.
func (w *opQueue) SetLayerTarget(layerID Layer, target gfx.TargetDescr) {
	value := w.layer(layerID)
	value.target = target
	w.setLayer(layerID, value)
}

func (w *opQueue) SetLayerTransform(layerID Layer, window m.Rect, aspect AspectMode) {
	value := w.layer(layerID)
	value.window = window
	value.aspect = aspect
	w.setLayer(layerID, value)
}

// SetLayerMaterial puts one material set over everything a layer draws that
// named no material of its own - its sprites, its glyphs, its fills and its
// triangles alike, each taking the slot for the family it is.
//
// Like Clear, SetLayerTarget and SetLayerTransform it is key/value applied at
// flush, not positional. That is the whole point: a caller that runs after every
// screen has recorded still reaches what they drew, which a recording cursor
// modelled on SetClip could never do. The last call in a tick wins, with the
// values the set held at that moment - it clones into the queue's material arena
// here, exactly as Sprite does, so a caller mutating their material afterwards
// does not retroactively change what was recorded.
//
// A nil slot keeps its built-in and one parameter list serves all three, so
// putting a fade over a menu is one call naming one shader and one amount. A
// draw that names its own material takes neither the set's slot nor its
// parameters: a scope's material and its parameters are one unit.
//
// reset clears it with the layer's other per-frame state.
func (w *opQueue) SetLayerMaterial(layerID Layer, set MaterialSet) {
	value := w.layer(layerID)
	value.materials = layerMaterials{set: set, has: true}
	value.materials.set.Sprite, w.materialArena = cloneMaterial(set.Sprite, w.materialArena)
	value.materials.set.Triangles, w.materialArena = cloneMaterial(set.Triangles, w.materialArena)
	value.materials.set.Texture, w.materialArena = cloneMaterial(set.Texture, w.materialArena)
	start := len(w.paramArena)
	w.paramArena = append(w.paramArena, set.Params...)
	value.materials.set.Params = w.paramArena[start:]
	w.setLayer(layerID, value)
}

// cloneMaterial snapshots one slot of a material set into the queue's arena. The
// clone lives in the layer rather than in an op, so it is stored by value behind
// a pointer into a per-layer cell: the layer map holds it, and nothing reallocs
// it for the rest of the tick.
func cloneMaterial(material *gfx.MaterialDescr, arena []gfx.ParameterDescr) (*gfx.MaterialDescr, []gfx.ParameterDescr) {
	if material == nil {
		return nil, arena
	}
	clone, arena := material.CloneTo(arena)
	return &clone, arena
}

// SetClip restricts subsequent operations to a rectangle in layer world space,
// the same coordinates as the layer window rect (before the layer transform).
func (w *opQueue) SetClip(clip m.Rect) {
	w.clip = clip
	w.hasClip = true
}

func (w *opQueue) RemoveClip() {
	w.hasClip = false
}

func (w *opQueue) Sprite(layerID Layer, texturePath string, transform SpriteTransform, material *gfx.MaterialDescr, params ...gfx.ParameterDescr) {
	op := spriteOp{path: normalizeResourcePath(texturePath), transform: transform}
	op.material, op.fingerprint, op.hasMaterial, w.materialArena = w.recordMaterial(material)
	start := len(w.paramArena)
	w.paramArena = append(w.paramArena, params...)
	op.params = w.paramArena[start:]
	w.record(layerID, drawOp{kind: drawSprite, sprite: op})
}

// SpriteTexture draws a rectangle sourcing an arbitrary gfx texture - a canvas
// layer's own render target, a camera's, or any baked texture - rather than a
// sprite path. A nil material uses TextureMaterial, which samples the texture as
// it is; the built-in sprite and triangle materials must not be used here,
// because both run the key-colour ramp over every texel.
//
// The transform means what it means for Sprite, measured against the texture's
// own dimensions instead of an atlas entry's: an unset Size takes the texture
// size, Frame selects a pixel sub-rectangle of it, and TileX/TileY repeat it.
// A texture that does not know its size yet - a resource path that has not
// baked - has no natural size and draws nothing.
func (w *opQueue) SpriteTexture(layerID Layer, texture gfx.TextureDescr, transform SpriteTransform, material *gfx.MaterialDescr, params ...gfx.ParameterDescr) {
	op := spriteOp{texture: texture, hasTexture: true, transform: transform}
	op.material, op.fingerprint, op.hasMaterial, w.materialArena = w.recordMaterial(material)
	start := len(w.paramArena)
	w.paramArena = append(w.paramArena, params...)
	op.params = w.paramArena[start:]
	w.record(layerID, drawOp{kind: drawSprite, sprite: op})
}

// FillRect fills a rectangle. It is a sprite draw over the atlas's white texel,
// so it batches with the sprites and glyphs around it and can carry a material
// of its own like any of them. Thickness is ignored.
func (w *opQueue) FillRect(layerID Layer, rect m.Rect, draw ShapeDraw) {
	w.shape(layerID, SpriteTransform{
		Position: m.Vec2{X: rect.X, Y: rect.Y}, Size: m.Vec2{X: rect.Width, Y: rect.Height},
	}, draw)
}

// StrokeRect outlines a rectangle with four fills, each carrying the draw's
// shading, so an outline under a custom material is four instances of one batch
// rather than four draws.
func (w *opQueue) StrokeRect(layerID Layer, rect m.Rect, draw ShapeDraw) {
	thickness := draw.Thickness
	if thickness <= 0 || rect.Width == 0 || rect.Height == 0 {
		return
	}
	w.FillRect(layerID, m.Rect{X: rect.X, Y: rect.Y, Width: rect.Width, Height: thickness}, draw)
	w.FillRect(layerID, m.Rect{X: rect.X, Y: rect.Y + rect.Height - thickness, Width: rect.Width, Height: thickness}, draw)
	w.FillRect(layerID, m.Rect{X: rect.X, Y: rect.Y + thickness, Width: thickness, Height: rect.Height - thickness*2}, draw)
	w.FillRect(layerID, m.Rect{X: rect.X + rect.Width - thickness, Y: rect.Y + thickness, Width: thickness, Height: rect.Height - thickness*2}, draw)
}

// Line draws a rotated fill between two points, Thickness wide.
func (w *opQueue) Line(layerID Layer, start, end m.Vec2, draw ShapeDraw) {
	dx, dy := end.X-start.X, end.Y-start.Y
	length := float32(math.Hypot(float64(dx), float64(dy)))
	if length == 0 || draw.Thickness <= 0 {
		return
	}
	w.shape(layerID, SpriteTransform{
		Position: start, Size: m.Vec2{X: length, Y: draw.Thickness},
		Origin: m.Vec2{Y: 0.5}, Rotation: float32(math.Atan2(float64(dy), float64(dx))),
	}, draw)
}

// shape records one shape helper's sprite: the draw's colour as the instance
// tint, ahead of the caller's own parameters so a caller that wants something
// else sets Color rather than passing a tint that first-wins would drop.
func (w *opQueue) shape(layerID Layer, transform SpriteTransform, draw ShapeDraw) {
	op := spriteOp{transform: transform}
	op.material, op.fingerprint, op.hasMaterial, w.materialArena = w.recordMaterial(draw.Material)
	start := len(w.paramArena)
	w.paramArena = append(w.paramArena, gfx.ColorParam(TintSlot, draw.tint()))
	w.paramArena = append(w.paramArena, draw.Params...)
	op.params = w.paramArena[start:]
	w.record(layerID, drawOp{kind: drawSprite, sprite: op})
}

func normalizeResourcePath(resourcePath string) string {
	if resourcePath == "" {
		return ""
	}
	cleaned := pathpkg.Clean(strings.ReplaceAll(resourcePath, "\\", "/"))
	if cleaned == "." {
		return ""
	}
	return cleaned
}

// Text records a text draw. An empty fontPath is resolved to DefaultFontPath
// here, at record time, so every consumer downstream (the flush, inspection,
// unloading) sees the path that will actually be drawn.
func (w *opQueue) Text(layerID Layer, fontPath, text string, draw TextDraw) {
	fontPath = resolveFontPath(fontPath)
	op := textOp{fontPath: fontPath, text: text, draw: draw}
	op.material, op.fingerprint, op.hasMaterial, w.materialArena = w.recordMaterial(draw.Material)
	w.record(layerID, drawOp{kind: drawText, text: op})
}

// recordMaterial snapshots a draw's material into the queue's arena and takes
// its batch key, both at record time. A nil material takes neither: it resolves
// to the layer's set or the built-in at flush, and each of those carries a
// fingerprint of its own.
func (w *opQueue) recordMaterial(material *gfx.MaterialDescr) (gfx.MaterialDescr, uint64, bool, []gfx.ParameterDescr) {
	if material == nil {
		return gfx.MaterialDescr{}, 0, false, w.materialArena
	}
	clone, arena := material.CloneTo(w.materialArena)
	// The fingerprint survives the clone by design: it hashes the descriptor's
	// content, not its address.
	return clone, clone.Fingerprint(), true, arena
}

// DrawTriangles snapshots a non-indexed triangle list. TVertex must be a
// pointer-free plain-data struct whose VertexLayout matches its memory layout
// and the material shader's vertex inputs. Bind a texture and sampler to
// TextureSlot/SamplerSlot (via material or params) to texture the triangles; the
// default material samples an opaque-white texel, so output equals vertex color.
func (w *opQueue) DrawTriangles[TVertex VertexLayout](layerID Layer, vertices []TVertex, material *gfx.MaterialDescr, params ...gfx.ParameterDescr) {
	w.drawTriangles(layerID, vertices, material, gfx.TextureDescr{}, false, params)
}

// DrawTexture is DrawTriangles sourcing an arbitrary gfx texture: the shape is
// the caller's, the texels come from texture, and the uvs in the vertices say
// which. It is what a composited camera panel, a masked minimap or a curved
// display is made of.
//
// It differs from binding the texture to TextureSlot on DrawTriangles in the
// material it defaults to. DrawTriangles samples through the key-colour ramp,
// which is right for artwork and silent damage to a rendered image; this
// samples the texture as it is. A non-nil material overrides that, and the
// texture is still bound to TextureSlot for it - canvas binds the slot itself,
// ahead of the caller's parameters, and first-wins means canvas's binding is the
// one that lands.
//
// The sampler comes from TextureMaterial, whose default is clamped and linear -
// what resampling a render target into a panel wants. Pass a SamplerParam to
// override it.
func (w *opQueue) DrawTexture[TVertex VertexLayout](layerID Layer, texture gfx.TextureDescr, vertices []TVertex, material *gfx.MaterialDescr, params ...gfx.ParameterDescr) {
	w.drawTriangles(layerID, vertices, material, texture, true, params)
}

// drawTriangles records one triangle list. A non-zero texture is bound to
// TextureSlot ahead of the caller's own parameters, and under first-wins that
// means the binding is canvas's: a caller who wants their own texture on their
// own shape uses DrawTriangles instead.
func (w *opQueue) drawTriangles[TVertex VertexLayout](layerID Layer, vertices []TVertex, material *gfx.MaterialDescr, texture gfx.TextureDescr, unkeyed bool, params []gfx.ParameterDescr) {
	if len(vertices) < 3 || len(vertices)%3 != 0 {
		return
	}
	var vertex TVertex
	vertexSize := int(unsafe.Sizeof(vertex))
	op := trianglesOp{unkeyed: unkeyed}
	op.material, op.fingerprint, op.hasMaterial, w.materialArena = w.recordMaterial(material)
	vertexStart := len(w.vertexArena)
	vertexBytes := unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), len(vertices)*vertexSize)
	w.vertexArena = append(w.vertexArena, vertexBytes...)
	op.vertices = w.vertexArena[vertexStart:]
	op.layoutID = w.cacheVertexLayout[TVertex]()
	op.builtinLayout = reflect.TypeFor[TVertex]() == reflect.TypeFor[Vertex]()
	paramStart := len(w.paramArena)
	if unkeyed {
		w.paramArena = append(w.paramArena, gfx.TextureParam(TextureSlot, texture))
	}
	w.paramArena = append(w.paramArena, params...)
	op.params = w.paramArena[paramStart:]
	w.record(layerID, drawOp{kind: drawTriangles, triangles: op})
}

func (w *opQueue) reset() {
	for layerID, value := range w.ops {
		clear(value.ops)
		value.ops = value.ops[:0]
		value.window = m.Rect{}
		value.aspect = AspectInscribe
		value.target = gfx.TargetDescr{}
		value.clearColor = m.Color{}
		value.hasColor = false
		value.materials = layerMaterials{}
		w.ops[layerID] = value
	}
	w.paramArena = w.paramArena[:0]
	w.materialArena = w.materialArena[:0]
	w.vertexArena = w.vertexArena[:0]
	w.clip = m.Rect{}
	w.hasClip = false
}

func (w *opQueue) layer(layerID Layer) layer {
	if value, ok := w.ops[layerID]; ok {
		return value
	}
	return layer{}
}

func (w *opQueue) setLayer(layerID Layer, value layer) {
	if w.ops == nil {
		w.ops = map[Layer]layer{}
	}
	w.ops[layerID] = value
}

// Reset drops all recorded operations and per-frame state, keeping cached vertex
// layouts. Recorders call it at the start of a frame so re-recording (e.g. one
// draw per fixed-update catch-up step) does not accumulate across frames.
func (w *opQueue) Reset() { w.reset() }

// OpCount returns the total number of recorded draw operations across all layers.
func (w *opQueue) OpCount() int {
	n := 0
	for _, value := range w.ops {
		n += len(value.ops)
	}
	return n
}

// record stamps the active clip cursor into d and appends it to its layer, so
// each operation carries the clip state in effect when it was recorded.
func (w *opQueue) record(layerID Layer, d drawOp) {
	d.clip = w.clip
	d.hasClip = w.hasClip
	value := w.layer(layerID)
	value.ops = append(value.ops, d)
	w.setLayer(layerID, value)
}

func (w *opQueue) cacheVertexLayout[TVertex VertexLayout]() int {
	vertexType := reflect.TypeFor[TVertex]()
	if id, ok := w.layoutIDs[vertexType]; ok {
		return id
	}
	var vertex TVertex
	layout := vertex.VertexLayout()
	if w.layoutIDs == nil {
		w.layoutIDs = map[reflect.Type]int{}
	}
	id := len(w.layouts)
	w.layoutIDs[vertexType] = id
	w.layouts = append(w.layouts, append([]gfx.VertexAttr(nil), layout...))
	return id
}
