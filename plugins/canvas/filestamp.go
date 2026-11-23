package canvas

import (
	"io/fs"
	"time"
)

// fileStamp is what fs.Stat answered of a file canvas read, which
// Assets.ReloadChanged compares to tell the file changed since: its
// modification time and size, or that fs.Stat failed. An embedded file's
// modification time is zero, and its stamp never changes.
type fileStamp struct {
	modTime    time.Time
	size       int64
	statFailed bool
}

// readFileStamp answers the stamp of the file at path in files as fs.Stat
// answers it now.
func readFileStamp(files fs.FS, path string) fileStamp {
	info, err := fs.Stat(files, path)
	if err != nil {
		return fileStamp{statFailed: true}
	}
	return fileStamp{modTime: info.ModTime(), size: info.Size()}
}

// equal answers whether s and other stamp the file as unchanged.
func (s fileStamp) equal(other fileStamp) bool {
	return s.modTime.Equal(other.modTime) && s.size == other.size && s.statFailed == other.statFailed
}
