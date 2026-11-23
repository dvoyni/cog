package canvas

// Material names one of canvas's 2D materials, made by Assets.MakeMaterial:
// a pointer-free, comparable handle an ECS component can hold. The zero
// Material is the default material, which draws an entry's image × its tint.
type Material struct{ index, generation uint32 }

// FontSet names one of canvas's font sets, made by WriteFontLibrary.MakeFontSet:
// a pointer-free, comparable handle an ECS component can hold. The zero
// FontSet is the default set, made from Config.DefaultFontSet.
type FontSet struct{ index, generation uint32 }

// Image names an image, a path in platform's FS and its ImageOptions,
// interned by ImageFromFile into a pointer-free, comparable value an ECS
// component can hold. The zero Image is canvas's white texel, which shapes
// draw over: one opaque white texel, centred.
type Image struct{ index uint32 }

// MeshFile names a mesh file, a path in platform's FS, interned by
// MeshFileFromFile into a pointer-free, comparable value an ECS component
// can hold. The zero MeshFile names none, and a Mesh of it draws nothing.
type MeshFile struct{ index uint32 }

// MeshFileFromFile interns the mesh file at path in platform's FS into a
// MeshFile: every call of the same path answers the same one. It loads
// nothing: a mesh's first draw of it loads it, through platform's FS, and
// it stays until Assets.Unload unloads its path. Each call takes the
// table's lock and hashes path, so a game makes a MeshFile once, as it
// names its art, and keeps it.
func MeshFileFromFile(path string) MeshFile {
	return MeshFile{index: meshFileNames.internKey(path)}
}
