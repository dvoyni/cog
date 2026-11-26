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

// DrawTrianglesOptions is how DrawTriangles draws its triangles: what a
// sprite carries but its size, pivot, draw mode and crop, since a mesh's
// geometry is its shape and its origin its pivot.
type DrawTrianglesOptions struct {
	// Image is what the triangles sample, as a sprite does, the zero Image
	// the white texel, so an untextured polygon draws its vertices' colours
	// times its tint. A vertex's UV runs 0 to 1 over the image's region, or
	// Frame's of its sheet; past them, it repeats along the axes the image
	// wraps along, so a terrain polygon whose UVs are its positions over the
	// image's size fills with tiling grass, and continues the image's edge
	// along the others.
	Image Image
	Frame uint32
	// Transform maps the triangles' own space into the world, a zero one
	// read as the identity. Under a pixel-perfect camera its translation
	// snaps to the world-unit grid, the vertices moving with it unsnapped, so
	// a mesh authored on the art grid stays on it.
	Transform m.Mat2x3
	// Tint multiplies the image's colour and the vertices': straight alpha,
	// premultiplied by canvas, zero meaning white.
	Tint m.Color
	// Material is the 2D material the triangles are drawn with, the zero
	// one the default material.
	Material Material
	// Clip is a rectangle of the world outside which the triangles draw
	// nothing, zero meaning none, as a Shape's Clip is.
	Clip m.Rect
	// Filter is how the image's texels are read between their centres,
	// zero meaning Config.DefaultFilter, as a Sprite's Filter is.
	Filter Filter
	// Layer and Order are the triangles' sort key, as a Shape's are.
	Layer int32
	Order int32
}
