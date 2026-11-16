package render

// stampedFile is a file a loaded asset was read from, by its path in
// platform's FS, with the stamp it had when it was read or a change of it
// was last claimed: a texture's or a buffer's own file, or a shader's file
// and each file its preprocessing included.
type stampedFile struct {
	path  string
	stamp fileStamp
}

// equal answers whether f and other are one file, stamped as unchanged.
func (f stampedFile) equal(other stampedFile) bool {
	return f.path == other.path && f.stamp.equal(other.stamp)
}
