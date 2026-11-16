package render

import (
	"io/fs"
	"slices"
	"sync"
)

// stampingFS is a filesystem over files that notes each file opened through
// it, once, with the stamp of the file it opened, a file that fails to open
// stamped as failing, so what read through it can tell later whether the
// files it read, or looked for, changed: render hands one over platform's
// FS to gfx's PreprocessShader, making a shader's include list. Its mutex
// guards the files noted.
type stampingFS struct {
	files        fs.FS
	mutex        sync.Mutex
	stampedFiles []stampedFile
}

// Open opens name in f's files, noting it.
func (f *stampingFS) Open(name string) (fs.File, error) {
	file, err := f.files.Open(name)
	stamp := fileStamp{statFailed: true}
	if err == nil {
		stamp = makeFileStamp(file.Stat())
	}
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if !slices.ContainsFunc(f.stampedFiles, func(stamped stampedFile) bool { return stamped.path == name }) {
		f.stampedFiles = append(f.stampedFiles, stampedFile{path: name, stamp: stamp})
	}
	return file, err
}
