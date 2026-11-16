package render

import (
	"io/fs"
	"time"
)

// fileStamp is what fs.Stat answered of a loaded asset's file, which
// Inventory.ReloadChanged compares to tell a file changed since: its
// modification time and size, or that fs.Stat failed. An embedded file's
// modification time is zero, and its stamp never changes.
type fileStamp struct {
	modTime    time.Time
	size       int64
	statFailed bool
}

// makeFileStamp answers the stamp of a file fs.Stat answered info and err
// of.
func makeFileStamp(info fs.FileInfo, err error) fileStamp {
	if err != nil {
		return fileStamp{statFailed: true}
	}
	return fileStamp{modTime: info.ModTime(), size: info.Size()}
}

// equal answers whether s and other stamp the file as unchanged.
func (s fileStamp) equal(other fileStamp) bool {
	return s.modTime.Equal(other.modTime) && s.size == other.size && s.statFailed == other.statFailed
}
