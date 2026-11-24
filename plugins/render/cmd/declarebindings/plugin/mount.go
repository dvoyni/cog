package plugin

// Mount is a folder mounted in platform's FS under a prefix, beside
// Config.Directory, for the shaders' includes of files kept elsewhere: a 2D
// material's shader includes canvas's include, cog.canvas/canvas.wgsl, from
// canvas's assets folder mounted under cog.canvas.
type Mount struct {
	// Prefix is the path in platform's FS the folder's files appear under, a
	// valid path below the root, as platform.ReadMountSocket's Prefix is.
	Prefix string
	// Directory is the folder mounted, relative to the folder the process
	// runs in, as Config.Directory is.
	Directory string
}
