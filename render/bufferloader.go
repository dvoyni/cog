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
		l.pendingLoads = append(l.pendingLoads, pendingBufferLoad{buffer: buffer, path: key.path})
	}
	return buffer
}

// writePending reads the file of each buffer pending and still held in
// table, one after another, holding no mutex, and writes it into the buffer,
// padded with zeros to its size. A file that cannot be read, or whose bytes
// padded no longer fill the buffer made at its acquire, is reported, and the
// buffer keeps what it held.
func (l *bufferLoader) writePending(table *assetTable, device gfx.Device, files fs.FS) {
	l.mutex.Lock()
	loads := l.pendingLoads
	l.pendingLoads = nil
	l.mutex.Unlock()
	l.write(table, device, files, loads)
}

// writeHeld writes the bytes of every buffer l holds again, as writePending
// writes those pending, taking the loads pending, which it holds too: after
// a device loss, which left them empty. A buffer whose bytes failed to load
// keeps zeros, its failure not reported again, and one of no bytes, whose
// file failed at its acquire, has none to write.
func (l *bufferLoader) writeHeld(table *assetTable, device gfx.Device, files fs.FS) {
	l.mutex.Lock()
	l.pendingLoads = nil
	var loads []pendingBufferLoad
	for buffer, entry := range l.entries {
		if !entry.failed {
			loads = append(loads, pendingBufferLoad{buffer: buffer, path: entry.key.path})
		}
	}
	l.mutex.Unlock()
	l.write(table, device, files, loads)
}

// write reads and writes the file of each of loads still held in table, as
// writePending says, holding no mutex but to mark a failed load.
func (l *bufferLoader) write(table *assetTable, device gfx.Device, files fs.FS, loads []pendingBufferLoad) {
	for _, load := range loads {
		gfxBuffer, size, ok := table.findBuffer(load.buffer)
		if !ok {
			continue
		}
		data, err := fs.ReadFile(files, load.path)
		paddedData := padBytes(data)
		if err == nil && uint64(len(paddedData)) != size {
			err = fmt.Errorf("a file of %d bytes, where its buffer, made when acquired, holds %d", len(data), size)
		}
		if err != nil {
			table.report(ReportKindLoadFailed, fmt.Errorf("render: buffer %q failed to load: %w", load.path, err))
			l.mutex.Lock()
			l.markFailed(load.buffer)
			l.mutex.Unlock()
			continue
		}
		writeBytes(table, device, load.buffer, gfxBuffer, 0, paddedData)
	}
}

// bufferKey is what an acquired buffer is shared by: the path of its file
// and the usage it was acquired with.
type bufferKey struct {
	path  string
	usage BufferUsageFlags
}

// pendingBufferLoad is a buffer whose bytes the decode-and-write job has yet
// to write: its handle and the path of its file.
type pendingBufferLoad struct {
	buffer Buffer
	path   string
}
