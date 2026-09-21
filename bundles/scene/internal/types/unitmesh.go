package types

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// unitShape names one of scene's own meshes, the ones the debug vocabulary is
// sugar over. The zero value is ShapeNone, so a draw record that says nothing
// about its shape draws the mesh it names instead - and a draw that names no
// mesh either draws nothing, which is what a rejected mint has to yield.
type unitShape uint8

const (
	ShapeNone unitShape = iota
	ShapeBox
	shapeSphere
	shapePlane
	shapeCount
)

// ensureUnit bakes one of scene's own meshes the first time something draws it
// and returns the same ref forever after. It is lazy because the backend may
// not be ready at startup, and a mesh baked then would either panic or silently
// not exist.
func (l *Lookup) ensureUnit(shape unitShape, bake BakeFunc) MeshRef {
	if l.unit[shape].source != MeshNone {
		return l.unit[shape]
	}
	var vertices []Vertex
	var indices []uint32
	switch shape {
	case shapeSphere:
		vertices, indices = model.UnitSphereGeometry()
	case shapePlane:
		vertices, indices = model.UnitPlaneGeometry()
	default:
		vertices, indices = model.UnitBoxGeometry()
	}
	// The layout goes through the same cache a caller's bake uses, so scene's
	// own meshes and a caller's standard-layout mesh share one layout id rather
	// than two that happen to describe the same attributes.
	layoutID, layout, _ := l.layouts.resolve[Vertex]()
	width := indexWidthFor(len(vertices))
	// A unit mesh is a standard-layout mesh like any other, so it is packed
	// through the same pass a caller's bake takes - into an arena of its own,
	// because this path bakes on the spot rather than staging for the flush.
	var arena []byte
	vertexSpan, bounds, uv := PackVertices(&arena, vertices)
	if shape == shapeSphere {
		// The exact sphere, not the circumsphere of its box, which would be
		// sqrt(3) times too generous.
		bounds = m.Sphere{Radius: 1}
	}
	indexSpan := appendArena(&arena, indexBytes(indices, width))
	l.unit[shape] = l.bakeMeshNow(meshInput{
		vertices: vertexSpan, indices: indexSpan,
		vertexCount: len(vertices), indexCount: len(indices),
		topology: gfx.TopologyTriangleList, indexWidth: width, layout: layout,
		layoutID: layoutID, standard: true, bounds: bounds, uv: uv,
	}, arena, bake)
	return l.unit[shape]
}
