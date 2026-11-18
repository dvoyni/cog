// Package canvas is a PROTOTYPE of canvas's API for
// https://github.com/dvoyni/cog/issues/871. Every call records what canvas
// would do, and Prototype.End prints the frame as canvas would draw it. It is
// not canvas.
package canvas

// Image names a picture: a path in the FileSystem and its options, interned
// by FromFile into a plain, pointer-free, comparable value, so an ECS
// component holds it. Naming it loads nothing; its first draw loads it.
type Image struct{ id uint32 }

// MeshFile names a mesh file by its path, interned by MeshFromFile as an
// Image is; a Mesh draws it.
type MeshFile struct{ id uint32 }

// FontSet names a font set made through Fonts' writing side; the zero
// FontSet is the default set, made from Config.DefaultFontSet.
type FontSet struct{ id uint32 }

// Material names a 2D material; the zero Material is canvas's default one.
type Material struct{ id uint32 }
