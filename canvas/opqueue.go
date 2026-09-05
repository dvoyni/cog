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
	params    []gfx.ParameterDescr
	material  gfx.MaterialDescr
	transform SpriteTransform

	hasMaterial bool
}

type textOp struct {
	fontPath string
	text     string
	draw     TextDraw
}

type trianglesOp struct {
	vertices []byte
	params   []gfx.ParameterDescr
	material gfx.MaterialDescr
	layoutID int

	// builtinLayout marks a list recorded with the built-in Vertex type, so
	// inspection can reinterpret the bytes without a layout description.
	builtinLayout bool
	hasMaterial   bool
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
	clip       m.Rect
	clearColor m.Color
	clearLayer Layer
	hasClip    bool
	hasColor   bool
}

type layer struct {
	ops    []drawOp
	window m.Rect
	aspect AspectMode
}

// Clear fills the screen at one layer, before anything that layer draws. It is
// positioned rather than frame-global because a frame-global clear cannot
// survive anything rendering below canvas: a camera ordered under the clear
// would be wiped. Naming the layer keeps the clear where the caller put it even
// on a frame where that layer draws nothing.
func (w *opQueue) Clear(layerID Layer, color m.Color) {
	w.clearColor, w.clearLayer, w.hasColor = color, layerID, true
}

func (w *opQueue) SetLayerTransform(layerID Layer, window m.Rect, aspect AspectMode) {
	value := w.layer(layerID)
	value.window = window
	value.aspect = aspect
	w.setLayer(layerID, value)
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
	if material != nil {
		op.material, w.materialArena = material.CloneTo(w.materialArena)
		op.hasMaterial = true
	}
	start := len(w.paramArena)
	w.paramArena = append(w.paramArena, params...)
	op.params = w.paramArena[start:]
	w.record(layerID, drawOp{kind: drawSprite, sprite: op})
}

func (w *opQueue) FillRect(layerID Layer, rect m.Rect, color m.Color) {
	w.Sprite(layerID, "", SpriteTransform{
		Position: m.Vec2{X: rect.X, Y: rect.Y}, Size: m.Vec2{X: rect.Width, Y: rect.Height},
	}, nil, gfx.ColorParam("tint", color))
}

func (w *opQueue) StrokeRect(layerID Layer, rect m.Rect, thickness float32, color m.Color) {
	if thickness <= 0 || rect.Width == 0 || rect.Height == 0 {
		return
	}
	w.FillRect(layerID, m.Rect{X: rect.X, Y: rect.Y, Width: rect.Width, Height: thickness}, color)
	w.FillRect(layerID, m.Rect{X: rect.X, Y: rect.Y + rect.Height - thickness, Width: rect.Width, Height: thickness}, color)
	w.FillRect(layerID, m.Rect{X: rect.X, Y: rect.Y + thickness, Width: thickness, Height: rect.Height - thickness*2}, color)
	w.FillRect(layerID, m.Rect{X: rect.X + rect.Width - thickness, Y: rect.Y + thickness, Width: thickness, Height: rect.Height - thickness*2}, color)
}

func (w *opQueue) Line(layerID Layer, start, end m.Vec2, thickness float32, color m.Color) {
	dx, dy := end.X-start.X, end.Y-start.Y
	length := float32(math.Hypot(float64(dx), float64(dy)))
	if length == 0 || thickness <= 0 {
		return
	}
	w.Sprite(layerID, "", SpriteTransform{
		Position: start, Size: m.Vec2{X: length, Y: thickness},
		Origin: m.Vec2{Y: 0.5}, Rotation: float32(math.Atan2(float64(dy), float64(dx))),
	}, nil, gfx.ColorParam("tint", color))
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

func (w *opQueue) Text(layerID Layer, fontPath, text string, draw TextDraw) {
	w.record(layerID, drawOp{
		kind: drawText,
		text: textOp{fontPath: fontPath, text: text, draw: draw},
	})
}

// DrawTriangles snapshots a non-indexed triangle list. TVertex must be a
// pointer-free plain-data struct whose VertexLayout matches its memory layout
// and the material shader's vertex inputs. Bind a texture and sampler to
// TextureSlot/SamplerSlot (via material or params) to texture the triangles; the
// default material samples an opaque-white texel, so output equals vertex color.
func (w *opQueue) DrawTriangles[TVertex VertexLayout](layerID Layer, vertices []TVertex, material *gfx.MaterialDescr, params ...gfx.ParameterDescr) {
	if len(vertices) < 3 || len(vertices)%3 != 0 {
		return
	}
	var vertex TVertex
	vertexSize := int(unsafe.Sizeof(vertex))
	op := trianglesOp{}
	vertexStart := len(w.vertexArena)
	vertexBytes := unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), len(vertices)*vertexSize)
	w.vertexArena = append(w.vertexArena, vertexBytes...)
	op.vertices = w.vertexArena[vertexStart:]
	op.layoutID = w.cacheVertexLayout[TVertex]()
	op.builtinLayout = reflect.TypeFor[TVertex]() == reflect.TypeFor[Vertex]()
	if material != nil {
		op.material, w.materialArena = material.CloneTo(w.materialArena)
		op.hasMaterial = true
	}
	paramStart := len(w.paramArena)
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
		w.ops[layerID] = value
	}
	w.paramArena = w.paramArena[:0]
	w.materialArena = w.materialArena[:0]
	w.vertexArena = w.vertexArena[:0]
	w.clip = m.Rect{}
	w.clearColor = m.Color{}
	w.hasClip = false
	w.hasColor = false
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
