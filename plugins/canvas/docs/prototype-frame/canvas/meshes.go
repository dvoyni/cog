package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/meshfile"
	"prototype.invalid/canvasframe/mx"
)

// Vertex is canvas's standard vertex layout, drawn by canvas's own mesh vertex
// stage, so a fragment-only material draws on a mesh as on a sprite. Color
// follows a tint's rules: zero is white, and a vertex faded out takes
// {1, 1, 1, 0}.
type Vertex struct {
	Position m.Vec2
	UV       m.Vec2
	Color    m.Color
}

// VertexLayout answers Vertex's layout.
func (Vertex) VertexLayout() meshfile.Layout {
	return meshfile.Layout{Stride: 32, Attributes: []meshfile.Attribute{
		{Format: meshfile.Float32x2, Offset: 0, Location: 0},
		{Format: meshfile.Float32x2, Offset: 8, Location: 1},
		{Format: meshfile.Float32x4, Offset: 16, Location: 2},
	}}
}

// Position3 answers the vertex's position with a zero z, for
// meshfile.CalculateBounds(vertices, canvas.Vertex.Position3).
func (v Vertex) Position3() m.Vec3 { return m.Vec3{X: v.Position.X, Y: v.Position.Y} }

// MeshFromFile interns the mesh file at path into a MeshFile. It loads
// nothing: its first draw loads it, through platform's FS, and it stays until
// Assets.Unload.
func MeshFromFile(path string) MeshFile { return tables.internMeshFile(path) }

// Mesh is an entry that draws a mesh file's triangles through a transform. It
// is a plain value, so an ECS component can be exactly it.
type Mesh struct {
	File MeshFile
	// Image is sampled as a sprite samples it, the mesh's UVs running 0 to 1
	// over its region and following its edges beyond; zero is the white
	// texel.
	Image     Image
	Frame     uint32
	Transform mx.Mat2x3
	Tint      m.Color
	Material  Material
	Clip      m.Rect
	Filter    Filter
	// Layer and Order are the sprite's sort key.
	Layer, Order int32
}

// DrawMesh records mesh. Consecutive meshes of one mesh file, with one
// material, atlas, filter and clip, are one instanced draw.
func (l DrawList) DrawMesh(mesh Mesh) { l.recordMesh(mesh, nil) }

// DrawMeshWith records mesh with its material's argument.
func (l DrawList) DrawMeshWith[T any](mesh Mesh, argument T) { l.recordMesh(mesh, argument) }

// DrawMeshes records meshes in their order.
func (l DrawList) DrawMeshes(meshes []Mesh) {
	for _, mesh := range meshes {
		l.recordMesh(mesh, nil)
	}
}

// DrawMeshesWith records meshes with their arguments, parallel slices.
func (l DrawList) DrawMeshesWith[T any](meshes []Mesh, arguments []T) {
	if len(meshes) != len(arguments) {
		tables.report("DrawMeshesWith: slices of different lengths are refused")
		return
	}
	for index, mesh := range meshes {
		l.recordMesh(mesh, arguments[index])
	}
}

// Triangles is a per-frame mesh's geometry: vertices of any layout, optional
// indices, a topology and its bounds, which are required.
type Triangles[V meshfile.VertexType] struct {
	Vertices []V
	Indices  []uint32
	Topology meshfile.Topology
	Bounds   m.Box3
}

// DrawTrianglesOptions are what triangles drawn for one frame are drawn with:
// a Mesh's fields, less the file.
type DrawTrianglesOptions struct {
	Image        Image
	Frame        uint32
	Transform    mx.Mat2x3
	Tint         m.Color
	Material     Material
	Clip         m.Rect
	Filter       Filter
	Layer, Order int32
}

// DrawTriangles records triangles for this frame alone. They are copied now,
// so the caller may reuse its slices. Each is a draw of its own, never merged
// with another.
func (l DrawList) DrawTriangles[V meshfile.VertexType](options DrawTrianglesOptions, triangles Triangles[V]) {
	l.recordTriangles(options, len(triangles.Vertices), triangles.Bounds, isStandard[V](), nil)
}

// DrawTrianglesWith records triangles with options' material's argument.
func (l DrawList) DrawTrianglesWith[V meshfile.VertexType, T any](options DrawTrianglesOptions, triangles Triangles[V], argument T) {
	l.recordTriangles(options, len(triangles.Vertices), triangles.Bounds, isStandard[V](), argument)
}

func isStandard[V meshfile.VertexType]() bool {
	var zero V
	_, standard := any(zero).(Vertex)
	return standard
}
