package canvas

// meshFileNames is the process's table of the mesh files MeshFileFromFile
// interned, by path, which every kernel's canvas reads their paths from. A
// MeshFile's index is its path's place in it, plus 1, so the zero MeshFile
// names none.
var meshFileNames internTable[string]
