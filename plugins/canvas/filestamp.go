package canvas

import (
	"io/fs"
	"time"
)

// fileStamp is what fs.Stat answered of a file canvas read, which
// Assets.ReloadChanged compares to tell the file changed since: its
// modification time and size, or that fs.Stat failed. An embedded file's
// modification time is zero, and its stamp never changes. The table and a
// font set's slot stamp their files with it.
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

// matches answers whether the file at path in files, which s stamped, is
// unchanged since, as fs.Stat answers it now, reading none of it.
func (s fileStamp) matches(files fs.FS, path string) bool {
	now := readFileStamp(files, path)
	return s.modTime.Equal(now.modTime) && s.size == now.size && s.statFailed == now.statFailed
}
