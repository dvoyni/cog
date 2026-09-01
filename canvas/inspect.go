package canvas

import (
	"slices"
	"unsafe"

	"github.com/dvoyni/cog/m"

	"github.com/dvoyni/cog/gfx"
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
	// Path, Transform and HasMaterial describe an OpSprite.
	Path        string
	Transform   SpriteTransform
	HasMaterial bool
	// FontPath, Text and Draw describe an OpText.
	FontPath string
	Text     string
	Draw     TextDraw
	// Vertices holds an OpTriangles list recorded with the built-in Vertex type.
	// A custom vertex layout reports no vertices.
	Vertices []Vertex
	Params   []gfx.ParameterDescr
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
func (w *opQueue) Ops(dst []Op) []Op {
	layers := make([]Layer, 0, len(w.ops))
	for layerID, value := range w.ops {
		if len(value.ops) > 0 {
			layers = append(layers, layerID)
		}
	}
	slices.Sort(layers)
	for _, layerID := range layers {
		value := w.ops[layerID]
		for i := range value.ops {
			dst = append(dst, w.inspectOp(layerID, &value.ops[i]))
		}
	}
	return dst
}

// LayerWindow reports the world-space window and aspect mode set for a layer by
// SetLayerTransform, and whether the layer has one.
func (w *opQueue) LayerWindow(layerID Layer) (m.Rect, AspectMode, bool) {
	value, ok := w.ops[layerID]
	if !ok || value.window == (m.Rect{}) {
		return m.Rect{}, AspectInscribe, false
	}
	return value.window, value.aspect, true
}

// ClearColor reports the color passed to Clear, and whether it was called.
func (w *opQueue) ClearColor() (m.Color, bool) { return w.clearColor, w.hasColor }

func (w *opQueue) inspectOp(layerID Layer, op *drawOp) Op {
	view := Op{Layer: layerID, Clip: op.clip, HasClip: op.hasClip}
	switch op.kind {
	case drawSprite:
		view.Kind = OpSprite
		view.Path = op.sprite.path
		view.Transform = op.sprite.transform
		view.HasMaterial = op.sprite.hasMaterial
		view.Params = op.sprite.params
	case drawText:
		view.Kind = OpText
		view.FontPath = op.text.fontPath
		view.Text = op.text.text
		view.Draw = op.text.draw
	case drawTriangles:
		view.Kind = OpTriangles
		view.HasMaterial = op.triangles.hasMaterial
		view.Params = op.triangles.params
		view.Vertices = builtinVertices(&op.triangles)
	}
	return view
}

// builtinVertices reinterprets a recorded triangle list as built-in vertices,
// or returns nil when it was recorded with a custom vertex type.
func builtinVertices(op *trianglesOp) []Vertex {
	if !op.builtinLayout || len(op.vertices) == 0 {
		return nil
	}
	size := int(unsafe.Sizeof(Vertex{}))
	return unsafe.Slice((*Vertex)(unsafe.Pointer(&op.vertices[0])), len(op.vertices)/size)
}
