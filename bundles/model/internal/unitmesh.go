package internal

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// UnitMesh names one of the unit meshes: the box, sphere and plane a
// renderer's debug vocabulary is sugar over. They are model's because they are
// meshes in the one mesh table; which call draws which is the renderer's.
type UnitMesh uint8

const (
	UnitBox UnitMesh = iota
	UnitSphere
	UnitPlane
	unitMeshCount
)

// EnsureUnit bakes one of the unit meshes the first time something draws it
// and returns the same ref forever after. It is lazy because the backend may
// not be ready at startup, and a mesh baked then would either panic or silently
// not exist.
func (l *Lookup) EnsureUnit(shape UnitMesh, bake BakeFunc) MeshRef {
	if l.unit[shape].source != MeshNone {
		return l.unit[shape]
	}
	var vertices []Vertex
	var indices []uint32
	switch shape {
	case UnitSphere:
		vertices, indices = UnitSphereGeometry()
	case UnitPlane:
		vertices, indices = UnitPlaneGeometry()
	default:
		vertices, indices = UnitBoxGeometry()
	}
	// The layout goes through the same cache a caller's bake uses, so the unit
	// meshes and a caller's standard-layout mesh share one layout id rather
	// than two that happen to describe the same attributes.
	layoutID, layout, _ := l.layouts.resolve[Vertex]()
	width := indexWidthFor(len(vertices))
	// A unit mesh is a standard-layout mesh like any other, so it is packed
	// through the same pass a caller's bake takes - into an arena of its own,
	// because this path bakes on the spot rather than staging for the flush.
	var arena []byte
	vertexSpan, bounds, uv := PackVertices(&arena, vertices)
	if shape == UnitSphere {
		// The exact sphere, not the circumsphere of its box, which would be
		// sqrt(3) times too generous.
		bounds = m.Sphere{Radius: 1}
	}
	indexSpan := appendArena(&arena, indexBytes(indices, width))
	l.unit[shape] = l.bakeMeshNow(MeshInput{
		vertices: vertexSpan, indices: indexSpan,
		vertexCount: len(vertices), indexCount: len(indices),
		topology: gfx.TopologyTriangleList, indexWidth: width, layout: layout,
		layoutID: layoutID, standard: true, bounds: bounds, uv: uv,
	}, arena, bake)
	return l.unit[shape]
}
