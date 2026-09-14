package types

import (
	"slices"
	"unsafe"

	"github.com/dvoyni/cog/libs/m"

	"github.com/dvoyni/cog/extensions/gfx"
)

// OpKind identifies which recording call produced an Op.
type OpKind uint8

const (
	OpSprite OpKind = iota
	OpText
	OpTriangles
)

// Op is a read-only view of one recorded operation. Ops returns them in flush
// order so a recorder can assert what it produced — layer, order, transform,
// clip and parameters — without running the GPU pipeline.
type Op struct {
	Kind    OpKind
	Layer   Layer
	Clip    m.Rect
	HasClip bool
	// Path and Transform describe an OpSprite. Texture is the gfx texture the op
	// samples - what SpriteTexture and DrawTexture were given, or whatever any op
	// bound to TextureSlot - and is the zero descriptor for an op that names a
	// resource path instead.
	//
	// HasMaterial says whether the op named a material of its own, on all three
	// kinds. It is not what the op will draw with: a draw that names none
	// resolves to the layer's material set and then to the built-in, both at
	// flush.

	Path       string
	Texture    gfx.TextureDescr
	HasTexture bool
	Transform  SpriteTransform
	// Material is the material the op named, recorded as it stood at record
	// time, and HasMaterial says whether it named one at all. Reading it is how
	// a caller answers "what shades this draw" without running the flush; the
	// resolution an op that named none goes through is described above.
	Material    gfx.MaterialDescr
	HasMaterial bool
	// FontPath, Text and Draw describe an OpText.
	FontPath string
	Text     string
	Draw     TextDraw
	// Vertices holds an OpTriangles list recorded with the built-in Vertex type.
	// A custom vertex layout reports no vertices.
	Vertices []Vertex
	// VertexBytes is how much vertex data an OpTriangles recorded, whatever
	// layout it used. It is the only thing a custom layout can say about its
	// geometry, and it is what tells "recorded with a layout nothing here can
	// read" apart from "recorded nothing".
	VertexBytes int
	Params      []gfx.ParameterDescr
}

// Param returns the recorded parameter with the given name.
func (o Op) Param(name string) (gfx.ParameterDescr, bool) {
	for i := range o.Params {
		if o.Params[i].Name() == name {
			return o.Params[i], true
		}
	}
	return gfx.ParameterDescr{}, false
}

// ColorParam returns the recorded color parameter with the given name.
func (o Op) ColorParam(name string) (m.Color, bool) {
	param, ok := o.Param(name)
	if !ok {
		return m.Color{}, false
	}
	return param.ColorValue()
}

// Ops appends every recorded operation to dst in flush order: layers ascending,
// then recording order within a layer. The returned slices alias the queue's
// storage and stay valid until the queue is reset or recorded into again.
func (w *OpQueue) Ops(dst []Op) []Op {
	layers := make([]Layer, 0, len(w.ops))
	for layerID, value := range w.ops {
		if len(value.Ops) > 0 {
			layers = append(layers, layerID)
		}
	}
	slices.Sort(layers)
	for _, layerID := range layers {
		value := w.ops[layerID]
		for i := range value.Ops {
			dst = append(dst, w.inspectOp(layerID, &value.Ops[i]))
		}
	}
	return dst
}

// LayerWindow reports the world-space window and aspect mode set for a layer by
// SetLayerTransform, and whether the layer has one.
func (w *OpQueue) LayerWindow(layerID Layer) (m.Rect, AspectMode, bool) {
	value, ok := w.ops[layerID]
	if !ok || value.Window == (m.Rect{}) {
		return m.Rect{}, AspectInscribe, false
	}
	return value.Window, value.Aspect, true
}

// LayerTarget reports the render target set for a layer by SetLayerTarget, and
// whether the layer has one. A layer without one draws to the screen.
func (w *OpQueue) LayerTarget(layerID Layer) (gfx.TargetDescr, bool) {
	value, ok := w.ops[layerID]
	if !ok || value.Target == (gfx.TargetDescr{}) {
		return gfx.TargetDescr{}, false
	}
	return value.Target, true
}

// LayerClear reports the color passed to Clear for one layer, and whether that
// layer clears at all.
func (w *OpQueue) LayerClear(layerID Layer) (m.Color, bool) {
	value, ok := w.ops[layerID]
	if !ok {
		return m.Color{}, false
	}
	return value.ClearColor, value.HasColor
}

func (w *OpQueue) inspectOp(layerID Layer, op *DrawOp) Op {
	view := Op{Kind: OpKindOf(op.Kind), Layer: layerID, Clip: op.Clip, HasClip: op.HasClip}
	switch op.Kind {
	case DrawSpriteKind:
		view.Path = op.Sprite.Path
		view.Texture, view.HasTexture = op.Sprite.Texture, op.Sprite.HasTexture
		view.Transform = op.Sprite.Transform
		view.Material, view.HasMaterial = op.Sprite.Material, op.Sprite.HasMaterial
		view.Params = op.Sprite.Params
	case DrawTextKind:
		view.FontPath = op.Text.FontPath
		view.Text = op.Text.Text
		view.Draw = op.Text.Draw
		view.Material, view.HasMaterial = op.Text.Material, op.Text.HasMaterial
		view.Params = op.Text.Draw.Params
	case DrawTrianglesKind:
		view.Material, view.HasMaterial = op.Triangles.Material, op.Triangles.HasMaterial
		view.Params = op.Triangles.Params
		if param, ok := view.Param(TextureSlot); ok {
			view.Texture, view.HasTexture = param.TextureValue()
		}
		view.Vertices = builtinVertices(&op.Triangles)
		view.VertexBytes = len(op.Triangles.Vertices)
	}
	return view
}

// OpKindOf names the recording call one DrawOp came from. It is one table
// rather than a case per site, because the snapshot decides whether to inspect
// an op from its kind alone and must agree with what inspection then reports.
func OpKindOf(kind DrawOpKind) OpKind {
	switch kind {
	case DrawTextKind:
		return OpText
	case DrawTrianglesKind:
		return OpTriangles
	}
	return OpSprite
}

// builtinVertices reinterprets a recorded triangle list as built-in vertices,
// or returns nil when it was recorded with a custom vertex type.
func builtinVertices(op *TrianglesOp) []Vertex {
	if !op.BuiltinLayout || len(op.Vertices) == 0 {
		return nil
	}
	size := int(unsafe.Sizeof(Vertex{}))
	return unsafe.Slice((*Vertex)(unsafe.Pointer(&op.Vertices[0])), len(op.Vertices)/size)
}
