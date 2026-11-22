package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/libs/meshfile"
)

// Triangles is a mesh given with its draw for one frame, as a trail, a rope
// or a skin is, which DrawTriangles copies, so its caller may reuse its
// slices: its Vertices, of a layout V answers, canvas.Vertex's drawn by
// canvas's own mesh vertex stage; its Indices, nil drawing the vertices in
// their order, kept as 16 bits while the vertices number at most 65,535;
// its Topology, zero meaning a triangle list; and its Bounds, in the mesh's
// own space before its transform, z unread, which a camera culls it by:
// meshfile.CalculateBounds(vertices, canvas.Vertex.Position3) answers them.
// Bounds are required, so a mesh never vanishes when its origin leaves the
// view; one that must never be culled gives bounds larger than any view.
type Triangles[V meshfile.VertexType] struct {
	Vertices []V
	Indices  []uint32
	Topology meshfile.Topology
	Bounds   m.Box3
}
