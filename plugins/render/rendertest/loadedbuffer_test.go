package rendertest_test

import (
	"bytes"
	"io/fs"
	"slices"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/platform/filesystems"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

func TestAnAcquiredBufferIsMadeOfItsFilesSizeWithItsUsageAtOnceItsBytesWrittenBeforeTheSubmitAndItIsSharedAndFreedAsATextureIs(t *testing.T) {
	h := rendertest.NewHarness(t)
	vertexBytes := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	indexBytes := []byte{0, 0, 1, 0, 2, 0, 3, 0}
	files := filesystems.NewMemoryFS()
	putFile(t, files, "meshes/crate.vertices", vertexBytes)
	putFile(t, files, "meshes/crate.indices", indexBytes)
	// vertices is the first frame's acquire of the vertices, shared its
	// second acquire, storage their acquire for storage, and indices the
	// acquire of the indices after the first frame; createdAtOnce is whether
	// each was made when its acquire returned.
	var vertices, shared, storage, indices render.Buffer
	var createdAtOnce [2]bool
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			switch frameIndex {
			case 0:
				vertices = inventory.AcquireBuffer("meshes/crate.vertices", render.BufferUsageFlagVertex)
				_, createdAtOnce[0] = readBufferCalls(h.Calls()).findCreated[recording.CreateBufferArguments](vertices)
				shared = inventory.AcquireBuffer("meshes/crate.vertices", render.BufferUsageFlagVertex)
				inventory.ReleaseBuffer(shared)
				storage = inventory.AcquireBuffer("meshes/crate.vertices", render.BufferUsageFlagStorage)
				inventory.ReleaseBuffer(storage)
			case 1:
				inventory.ReleaseBuffer(vertices)
			}
		},
		afterFrame: func(frameIndex int, inventory render.Inventory) {
			if frameIndex == 0 {
				indices = inventory.AcquireBuffer("meshes/crate.indices", render.BufferUsageFlagIndex|render.BufferUsageFlagIndirect)
				_, createdAtOnce[1] = readBufferCalls(h.Calls()).findCreated[recording.CreateBufferArguments](indices)
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	if shared != vertices {
		t.Errorf("a second acquire of the vertices answered %v, want %v, the first's", shared, vertices)
	}
	if storage == vertices || storage == (render.Buffer{}) {
		t.Errorf("an acquire of the vertices for storage answered %v, want a buffer of its own, beside %v", storage, vertices)
	}
	calls := h.Calls()
	bufferCalls := readBufferCalls(calls)
	submits := findCallIndices(calls, recording.CallKindSubmit)
	for index, want := range []struct {
		buffer      render.Buffer
		size        uint64
		usage       gfx.BufferUsageFlags
		data        []byte
		submitIndex int
		submit      string
	}{
		{buffer: vertices, size: 12, usage: gfx.BufferUsageFlagVertex | gfx.BufferUsageFlagCopyDst, data: append(slices.Clone(vertexBytes), 0, 0), submitIndex: 0, submit: "the submit of the frame it was acquired during"},
		{buffer: indices, size: 8, usage: gfx.BufferUsageFlagIndex | gfx.BufferUsageFlagIndirect | gfx.BufferUsageFlagCopyDst, data: indexBytes, submitIndex: 1, submit: "the submit of the frame after it was acquired"},
	} {
		if !createdAtOnce[index] {
			t.Errorf("buffer %d was not made when its acquire returned", index)
		}
		created, ok := bufferCalls.findCreated[recording.CreateBufferArguments](want.buffer)
		if !ok {
			t.Errorf("buffer %d, %v, was not made once", index, want.buffer)
			continue
		}
		if created.Descriptor.Size != want.size || created.Descriptor.Usage != want.usage {
			t.Errorf("buffer %d was made of %d bytes with usage %#x, want %d, its file's size rounded up to a multiple of 4, with %#x, its acquire's and the copy destination", index, created.Descriptor.Size, created.Descriptor.Usage, want.size, want.usage)
		}
		writes := bufferCalls.find(recording.CallKindWriteBuffer, want.buffer)
		if len(writes) != 1 {
			t.Errorf("buffer %d was written %d times, want once", index, len(writes))
			continue
		}
		if write := writes[0].arguments.(recording.WriteBufferArguments); write.Offset != 0 || !bytes.Equal(write.Data, want.data) {
			t.Errorf("buffer %d was written %v at offset %d, want %v at 0", index, write.Data, write.Offset, want.data)
		}
		if want.submitIndex >= len(submits) || writes[0].index > submits[want.submitIndex] {
			t.Errorf("buffer %d was written at call %d, after %s, at %v", index, writes[0].index, want.submit, submits)
		}
	}
	if frameIndex, freed := bufferCalls.findFreeingFrame(vertices); !freed || frameIndex != 1 {
		t.Errorf("the vertices, acquired twice in the first frame, released there once and in the second frame again, were freed: %v, at the end of frame %d, want once, at the end of frame 1", freed, frameIndex)
	}
}

func TestParallelAcquiresOfOnePathShareOneBufferFreedOnceByTheirReleases(t *testing.T) {
	const acquireCount = 64
	h := rendertest.NewHarness(t)
	// mutex guards acquired, every buffer the tasks acquired.
	var mutex sync.Mutex
	var acquired []render.Buffer
	h.Spawn(nil, &loaderPlugin{
		files:              &overlappingFS{FS: fstest.MapFS{"crate.vertices": {Data: []byte{1, 2, 3, 4}}}, name: "crate.vertices"},
		recordingTaskCount: 2,
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			for range acquireCount {
				switch frameIndex {
				case 0:
					buffer := inventory.AcquireBuffer("crate.vertices", render.BufferUsageFlagVertex)
					mutex.Lock()
					acquired = append(acquired, buffer)
					mutex.Unlock()
				case 1:
					mutex.Lock()
					buffer := acquired[0]
					mutex.Unlock()
					inventory.ReleaseBuffer(buffer)
				}
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()
	h.Step()

	mutex.Lock()
	defer mutex.Unlock()
	if len(acquired) != 2*acquireCount || slices.ContainsFunc(acquired, func(buffer render.Buffer) bool { return buffer != acquired[0] }) {
		t.Fatalf("two tasks acquiring one path %d times each got %v, want one buffer every time", acquireCount, acquired)
	}
	calls := h.Calls()
	if created := findCallIndices(calls, recording.CallKindCreateBuffer); len(created) != 1 {
		t.Fatalf("render made %d buffers, want one", len(created))
	}
	if frameIndex, freed := readBufferCalls(calls).findFreeingFrame(acquired[0]); !freed || frameIndex != 1 {
		t.Errorf("the buffer was freed: %v, at the end of frame %d, want once, at the end of the frame of its last release, 1", freed, frameIndex)
	}
}

func TestABufferThatFailsToLoadIsReportedOncePerKeyUntilFreed(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindLoadFailed)
	// meshes is a folder whose size is not zero, as a folder's is on many
	// disks.
	files := fstest.MapFS{
		"empty.bin":   {},
		"meshes":      {Mode: fs.ModeDir | 0o755, Data: make([]byte, 4096)},
		"resized.bin": {Data: []byte{1, 2, 3, 4}},
	}
	// missing is the first frame's buffer of a missing file, folder that of
	// a folder, empty that of a file of no bytes, and resized that of a file
	// put again at another size between its acquire, after the first frame,
	// and the write of its bytes.
	var missing, folder, empty, resized render.Buffer
	var reportCountAfterTheFirstAcquire int
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			switch frameIndex {
			case 0:
				missing = inventory.AcquireBuffer("missing.bin", render.BufferUsageFlagVertex)
				reportCountAfterTheFirstAcquire = len(h.Reports())
				inventory.ReleaseBuffer(inventory.AcquireBuffer("missing.bin", render.BufferUsageFlagVertex))
				inventory.ReleaseBuffer(missing)
				folder = inventory.AcquireBuffer("meshes", render.BufferUsageFlagVertex)
				empty = inventory.AcquireBuffer("empty.bin", render.BufferUsageFlagVertex)
			case 1:
				inventory.ReleaseBuffer(inventory.AcquireBuffer("missing.bin", render.BufferUsageFlagVertex))
			}
		},
		afterFrame: func(frameIndex int, inventory render.Inventory) {
			if frameIndex == 0 {
				resized = inventory.AcquireBuffer("resized.bin", render.BufferUsageFlagVertex)
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	files["resized.bin"] = &fstest.MapFile{Data: []byte{1, 2, 3, 4, 5, 6, 7, 8}}
	h.Step()

	if reportCountAfterTheFirstAcquire != 1 {
		t.Errorf("render had logged %d reports when the acquire of a missing file returned, want 1", reportCountAfterTheFirstAcquire)
	}
	if kinds, want := makeReportKinds(h), slices.Repeat([]render.ReportKind{render.ReportKindLoadFailed}, 5); !slices.Equal(kinds, want) {
		t.Errorf("render reported kinds %v, want %v: the missing file once in each frame, as it was freed between them, and the folder, the empty file and the resized one once each", kinds, want)
	}
	if missing == (render.Buffer{}) || folder == (render.Buffer{}) || empty == (render.Buffer{}) || folder == empty {
		t.Errorf("the failed loads answered %v, %v and %v, want buffers of their own", missing, folder, empty)
	}
	bufferCalls := readBufferCalls(h.Calls())
	if created := findCallIndices(h.Calls(), recording.CallKindCreateBuffer); len(created) != 1 {
		t.Errorf("render made %d gfx buffers, want one, the resized file's: a failed acquire gets a buffer of no bytes", len(created))
	}
	if writes := bufferCalls.find(recording.CallKindWriteBuffer, resized); len(writes) != 0 {
		t.Errorf("the buffer of the file resized after its acquire was written %d times, want none", len(writes))
	}
}
