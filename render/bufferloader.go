package render

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/dvoyni/cog/gfx"
)

// bufferLoader shares, counts and frees the buffers render loads from files:
// one buffer for each path and usage, counted by the acquires holding it, its bytes
// written by render's decode-and-write job, and freed at the end of the frame
// its count reaches zero in. Its counter's mutex also guards its
// pending loads; it reads no file holding it.
type bufferLoader struct {
	assetCounter[bufferKey, Buffer]
	// pendingLoads are the buffers whose bytes the decode-and-write job has
	// yet to write.
	pendingLoads []pendingBufferLoad
}

// acquire hands out the buffer of key, counting one more holder, and makes
// it in table, of the file's size rounded up to a multiple of 4, when the
// loader holds none, leaving its bytes for the decode-and-write job to write.
// A file fs.Stat fails on, a folder or a file of no bytes is reported, and
// gets a buffer of no bytes. A buffer gfx refuses to make is logged, unless
// the device is lost, and the zero Buffer answered.
func (l *bufferLoader) acquire(table *assetTable, device gfx.Device, files fs.FS, key bufferKey) Buffer {
	l.mutex.Lock()
	buffer, ok := l.hold(key)
	l.mutex.Unlock()
	if ok {
		return buffer
	}
	var size uint64
	info, statErr := fs.Stat(files, key.path)
	switch {
	case statErr != nil:
	case info.IsDir():
		statErr = errors.New("a folder")
	case info.Size() == 0:
		statErr = errors.New("a file of no bytes")
	default:
		size = uint64(info.Size())
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if buffer, ok := l.hold(key); ok {
		return buffer
	}
	buffer, _, err := table.addBuffer(device, key.path, size, key.usage)
	if err != nil {
		table.logUnlessLost(fmt.Errorf("render: acquiring buffer %q: %w", key.path, err))
		return Buffer{}
	}
	l.add(key, buffer)
	if statErr != nil {
		table.report(ReportKindLoadFailed, fmt.Errorf("render: buffer %q failed to load: %w", key.path, statErr))
	} else {
		l.pendingLoads = append(l.pendingLoads, pendingBufferLoad{buffer: buffer, path: key.path, size: size})
	}
	return buffer
}

// writePending reads the file of each buffer pending and still held in
// table, one after another, holding no mutex, and writes it into the buffer,
// padded with zeros to its size. A file that cannot be read, or is no longer
// of the size its acquire found, is reported, and the buffer keeps zeros.
func (l *bufferLoader) writePending(table *assetTable, device gfx.Device, files fs.FS) {
	l.mutex.Lock()
	loads := l.pendingLoads
	l.pendingLoads = nil
	l.mutex.Unlock()
	for _, load := range loads {
		gfxBuffer, _, ok := table.findBuffer(load.buffer)
		if !ok {
			continue
		}
		data, err := fs.ReadFile(files, load.path)
		if err == nil && uint64(len(data)) != load.size {
			err = fmt.Errorf("a file of %d bytes, where it was of %d when acquired", len(data), load.size)
		}
		if err != nil {
			table.report(ReportKindLoadFailed, fmt.Errorf("render: buffer %q failed to load: %w", load.path, err))
			continue
		}
		writeBytes(table, device, gfxBuffer, 0, padBytes(data))
	}
}

// bufferKey is what an acquired buffer is shared by: the path of its file
// and the usage it was acquired with.
type bufferKey struct {
	path  string
	usage BufferUsageFlags
}

// pendingBufferLoad is a buffer whose bytes the decode-and-write job has yet
// to write: its handle, the path of its file and the file's size.
type pendingBufferLoad struct {
	buffer Buffer
	path   string
	size   uint64
}
