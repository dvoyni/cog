// Package meshfile reads and writes mesh files: a mesh's vertices kept with
// their layout, its topology, its optional indices and its bounds, in one
// file. canvas loads its meshes from them, and a 3D plugin can share them;
// the package imports no render, so a tool exporting mesh files needs
// neither render nor gfx.
//
// A tool writes a mesh file with Encode, from vertices of a type that
// answers its own layout:
//
//	type GrassVertex struct {
//		Position m.Vec2
//		Sway     float32
//		_        float32
//	}
//
//	func (GrassVertex) VertexLayout() meshfile.Layout {
//		return meshfile.Layout{Stride: 16, Attributes: []meshfile.Attribute{
//			{Format: meshfile.FormatFloat32x2, Offset: 0, Location: 0},
//			{Format: meshfile.FormatFloat32, Offset: 8, Location: 1},
//		}}
//	}
//
//	bounds := meshfile.CalculateBounds(vertices, func(v GrassVertex) m.Vec3 { return m.Vec3{X: v.Position.X, Y: v.Position.Y} })
//	data, err := meshfile.Encode(vertices, indices, meshfile.TopologyTriangleList, bounds)
//
// and a reader decodes it with Decode, which answers its vertex and index
// bytes as slices of the file's, ready to write into buffers.
//
// # The format, version 1
//
// A mesh file is little-endian throughout. It is a header of 56 bytes, each
// field 4 bytes:
//
//	offset  field
//	0       the magic, the ASCII bytes "CMSH"
//	4       the version, 1
//	8       the topology, a Topology: 0 a triangle list, 1 a point list,
//	        2 a line list, 3 a line strip, 4 a triangle strip
//	12      the index format, an IndexFormat: 0 no indices, 1 16 bits,
//	        2 32 bits
//	16      the stride, the bytes from one vertex to the next
//	20      the number of attributes
//	24      the number of vertices
//	28      the number of indices, 0 when the index format is 0 and more
//	        otherwise
//	32      the bounds' minimum x, y and z, each a float32
//	44      the bounds' maximum x, y and z, each a float32
//
// then each attribute, 12 bytes of its format, a Format, its offset into the
// vertex and its shader location, each 4 bytes; then the vertices, stride
// bytes each; then the indices, of 2 or 4 bytes each; and then the file
// ends. A format, a topology and an index format each take the value of
// WebGPU's of the same name in gfx, so a reader on render converts them as
// they are.
//
// Encode writes the vertices' own bytes, as Go lays out the vertex type in
// memory, which is little-endian on every machine WebGPU runs on. It writes
// indices in 16 bits while the mesh has at most 65,535 vertices, so no index
// is 0xFFFF, which a strip reads as a restart, and in 32 bits beyond.
//
// A later version changes the version field, and Decode refuses a version it
// does not know, so a reader never misreads a file written for a later one.
package meshfile
