package canvas

import "github.com/dvoyni/cog/libs/m"

// Mesh is an entry that draws a mesh file's triangles through a transform,
// sampling an image as a sprite does. It is a literal, like every other
// entry: canvas.Mesh{File: tree, Image: bark, Transform: m.Translation2x3(x, y), Layer: l}.
// Consecutive meshes in sort order of one file, material, atlas, filter and
// clip are one instanced draw, so a forest of one tree costs one draw.
type Mesh struct {
	// File is the mesh file drawn. Its first draw loads it, through
	// platform's FS, drawn that frame, and it stays until its path is
	// unloaded. A file that is missing, is no mesh file, is of a version
	// canvas does not know or has zero bounds draws nothing, reported once
	// for its path as ReportKindMeshFileNotLoaded, until its path is
	// unloaded, even once a good file is put there. A file of canvas.Vertex's
	// layout is drawn by canvas's own mesh vertex stage and the material's
	// fragment stage; one of another layout by the material's own vertex
	// stage, and one drawn with a material whose vertex stage is canvas's,
	// as a fragment-only material's is, is skipped, reported once for the
	// material as ReportKindVertexStageMissing. The zero MeshFile draws
	// nothing.
	File MeshFile
	// Image is what the mesh samples, as a sprite does, the zero Image the
	// white texel, so an untextured polygon draws its vertices' colours
	// times its tint. A vertex's UV runs 0 to 1 over the image's region, or
	// Frame's of its sheet; past them, it repeats along the axes the image
	// wraps along, and continues the image's edge along the others.
	Image Image
	Frame uint32
	// Transform maps the mesh's own space, where its file's bounds are, into
	// the world, a zero one read as the identity; a camera culls the mesh by
	// its bounds through it. Under a pixel-perfect camera its translation
	// snaps to the world-unit grid, the vertices moving with it unsnapped,
	// so a mesh authored on the art grid stays on it.
	Transform m.Mat2x3
	// Tint multiplies the image's colour and the vertices': straight alpha,
	// premultiplied by canvas, zero meaning white.
	Tint m.Color
	// Material is the 2D material the mesh is drawn with, the zero one the
	// default material.
	Material Material
	// Clip is a rectangle of the world outside which the mesh draws nothing,
	// zero meaning none, as a Shape's Clip is.
	Clip m.Rect
	// Filter is how the image's texels are read between their centres,
	// zero meaning Config.DefaultFilter, as a Sprite's Filter is.
	Filter Filter
	// Layer and Order are the mesh's sort key, as a Shape's are.
	Layer int32
	Order int32
}
