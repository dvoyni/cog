package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/libs/meshfile"
)

// Vertex is canvas's standard vertex layout, 32 bytes a vertex, drawn by
// canvas's own mesh vertex stage, so a material of a fragment stage alone
// draws meshes as it draws sprites. Position is in the mesh's own space,
// which its transform maps into the world; UV runs 0 to 1 over its image's
// region, or its frame's for a sheet; Color multiplies the image as a tint
// does: straight alpha, premultiplied by canvas, zero meaning white, so a
// vertex faded out takes {1, 1, 1, 0}.
type Vertex struct {
	Position m.Vec2
	UV       m.Vec2
	Color    m.Color
}

// vertexLayout is Vertex's layout: its position at location 0, its UV at 1
// and its colour at 2, as canvas's include's CanvasVertex reads them.
var vertexLayout = meshfile.Layout{
	Stride: 32,
	Attributes: []meshfile.Attribute{
		{Format: meshfile.FormatFloat32x2, Offset: 0, Location: 0},
		{Format: meshfile.FormatFloat32x2, Offset: 8, Location: 1},
		{Format: meshfile.FormatFloat32x4, Offset: 16, Location: 2},
	},
}

// VertexLayout answers Vertex's layout, for meshfile.Encode and a mesh's
// draw.
func (Vertex) VertexLayout() meshfile.Layout {
	return vertexLayout
}

// Position3 answers the vertex's position with a zero z, for
// meshfile.CalculateBounds(vertices, canvas.Vertex.Position3).
func (v Vertex) Position3() m.Vec3 {
	return m.Vec3{X: v.Position.X, Y: v.Position.Y}
}
