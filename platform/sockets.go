package platform

// WriteFSSocket is the writable filesystem, which exactly one plugin binds in
// its Init with configurator.BindSocket[platform.WriteFSSocket]. It is
// searched before every read mount, and writes land in it. It is no
// resource: jobs write through ElevateFileAccess, so the lock on the FS
// resource keeps readers from seeing a file mid-write.
type WriteFSSocket interface {
	WriteFS
}

// ReadMountSocket is one read-only layer of the FS resource, which any
// plugin binds in its Init with
// configurator.BindSocket(platform.ReadMountSocket{...}): an embed.FS, an
// os.DirFS, any fs.FS. Higher priorities are searched first, equal ones in
// plugin order, all after the writable filesystem.
type ReadMountSocket struct {
	FS       FS
	Priority int
}
