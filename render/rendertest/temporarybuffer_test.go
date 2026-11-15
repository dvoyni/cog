package rendertest_test

import (
	"bytes"
	"strings"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/internal/handle"
	"github.com/dvoyni/cog/render/rendertest"
)

// findChunks answers the gfx buffer behind each chunk of the temporary arena
// calls made, with its size.
func findChunks(calls []recording.Call) map[gfx.Buffer]uint64 {
	chunks := map[gfx.Buffer]uint64{}
	for _, call := range calls {
		if arguments, ok := call.Arguments.(recording.CreateBufferArguments); ok && strings.HasSuffix(arguments.Descriptor.Label, handle.TemporaryChunkLabel) {
			chunks[arguments.Buffer] = arguments.Descriptor.Size
		}
	}
	return chunks
}

// findBufferWrites answers the writes calls made into buffer, in order.
func findBufferWrites(calls []recording.Call, buffer gfx.Buffer) []recording.WriteBufferArguments {
	var writes []recording.WriteBufferArguments
	for _, call := range calls {
		if arguments, ok := call.Arguments.(recording.WriteBufferArguments); ok && arguments.Buffer == buffer {
			writes = append(writes, arguments)
		}
	}
	return writes
}

// findChunkOf answers the chunk among chunks whose writes hold the bytes of
// bufferRange, data, at its offset; false when none does.
func findChunkOf(calls []recording.Call, chunks map[gfx.Buffer]uint64, bufferRange render.BufferRange, data []byte) (gfx.Buffer, bool) {
	for chunk := range chunks {
		for _, write := range findBufferWrites(calls, chunk) {
			if write.Offset == 0 && uint64(len(write.Data)) >= bufferRange.Offset+bufferRange.Size && bytes.Equal(write.Data[bufferRange.Offset:bufferRange.Offset+bufferRange.Size], data) {
				return chunk, true
			}
		}
	}
	return gfx.Buffer{}, false
}

func TestEachListTakesChunksOfItsOwnWrittenOnceAtItsEndAndABlockLargerThanAChunkGetsItsOwn(t *testing.T) {
	h := rendertest.NewHarness(t)
	// blocks are the data of the first list's blocks: five of one 256-byte
	// slot each, filling a chunk of 1024 bytes and spilling into a second,
	// one larger than a chunk, and one more, which goes into the second.
	blocks := [][]uint32{make([]uint32, 64), make([]uint32, 3), make([]uint32, 64), make([]uint32, 64), make([]uint32, 1), make([]uint32, 700), make([]uint32, 2)}
	for index, block := range blocks {
		for element := range block {
			block[element] = uint32(index<<16 | element)
		}
	}
	other := []uint32{7, 7, 7}
	unended := []uint32{9, 9, 9}
	var ranges []render.BufferRange
	var otherRange, unendedRange render.BufferRange
	spawnPassParameterHarness(t, h, &render.Config{TemporaryChunkSize: 1024}, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		list := frame.BeginCommandList("first", drawOrder)
		otherList := frame.BeginCommandList("second", drawOrder+1)
		unendedList := frame.BeginCommandList("unended", drawOrder+2)
		for _, block := range blocks {
			ranges = append(ranges, list.AcquireTemporaryBuffer(block))
		}
		otherRange = otherList.AcquireTemporaryBuffer(other)
		unendedRange = unendedList.AcquireTemporaryBuffer(unended)
		if empty := list.AcquireTemporaryBuffer([]uint32{}); empty != (render.BufferRange{}) {
			t.Errorf("a temporary buffer of no data is %+v, want the zero BufferRange", empty)
		}
		list.End()
		otherList.End()
	})

	wantOffsets := []uint64{0, 256, 512, 768, 0, 0, 256}
	for index, bufferRange := range ranges {
		if bufferRange.Offset != wantOffsets[index] || bufferRange.Size != uint64(4*len(blocks[index])) {
			t.Errorf("block %d is at offset %d of %d bytes, want offset %d of %d", index, bufferRange.Offset, bufferRange.Size, wantOffsets[index], 4*len(blocks[index]))
		}
	}
	if ranges[0].Buffer != ranges[3].Buffer || ranges[4].Buffer == ranges[0].Buffer || ranges[5].Buffer == ranges[0].Buffer || ranges[5].Buffer == ranges[4].Buffer || ranges[6].Buffer != ranges[4].Buffer || otherRange.Buffer == ranges[0].Buffer || otherRange.Buffer == ranges[4].Buffer {
		t.Errorf("the blocks are in buffers %v, and the second list's in %v, want four in one chunk, the fifth and the last in a second, the large one in a third, and the second list's in a fourth", ranges, otherRange.Buffer)
	}
	calls := h.Calls()
	chunks := findChunks(calls)
	sizes := map[uint64]int{}
	for chunk, size := range chunks {
		sizes[size]++
		if writes := findBufferWrites(calls, chunk); len(writes) > 1 {
			t.Errorf("chunk %v was written %d times, want once, at its list's End", chunk, len(writes))
		}
	}
	if sizes[1024] != 4 || sizes[3072] != 1 || len(chunks) != 5 {
		t.Errorf("render made chunks of sizes %v, want four of 1024 bytes, one for each list and a second for the first, and one of 3072 for the large block of 2800, its size rounded up to a multiple of the chunk size", sizes)
	}
	for index, block := range blocks {
		if _, ok := findChunkOf(calls, chunks, ranges[index], unsafe.Slice((*byte)(unsafe.Pointer(&block[0])), 4*len(block))); !ok {
			t.Errorf("no chunk write holds block %d at its range %+v", index, ranges[index])
		}
	}
	if _, ok := findChunkOf(calls, chunks, otherRange, unsafe.Slice((*byte)(unsafe.Pointer(&other[0])), 12)); !ok {
		t.Error("no chunk write holds the second list's block")
	}
	if _, ok := findChunkOf(calls, chunks, unendedRange, unsafe.Slice((*byte)(unsafe.Pointer(&unended[0])), 12)); ok {
		t.Error("a chunk of a list never ended was written, want its bytes handed to gfx only at its End")
	}
}

func TestATemporaryBufferHoldsVerticesIndicesOrRowsForTheFrame(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene passParameterScene
	vertices := []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 0}
	indices := []uint16{0, 1, 2, 2, 1, 3, 0, 2, 1}
	rows := []float32{1, 2, 3, 4}
	var rowsRange render.BufferRange
	spawnPassParameterHarness(t, h, nil, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makePassParameterScene(inventory)
		list := frame.BeginCommandList("draws", drawOrder)
		vertexRange := list.AcquireTemporaryBuffer(vertices)
		indexRange := list.AcquireTemporaryBuffer(indices)
		rowsRange = list.AcquireTemporaryBuffer(rows)
		mesh := inventory.MakeMesh(render.MeshDescriptor{
			Label:         "temporary",
			VertexBuffers: []render.VertexBuffer{{Range: vertexRange, Layout: positionLayout}},
			Indices:       render.IndexBuffer{Range: indexRange, Format: render.IndexFormatUint16},
		})
		pass := list.BeginPass(scene.colorPass("scene"))
		pass.SetMaterial(scene.material)
		pass.SetBuffer("rows", rowsRange)
		pass.Draw(mesh, render.DrawArgs{})
		pass.End()
		list.End()
		inventory.ReleaseMesh(mesh)
	})

	draw := findOnlyDraw(t, h, "scene")[0]
	if draw.IndexCount != uint32(len(indices)) {
		t.Errorf("the draw of the temporary mesh drew %d indices, want %d", draw.IndexCount, len(indices))
	}
	if parameter, _ := findParameterIn(draw.DrawParameters, "rows"); parameter.Buffer != rowsRange {
		t.Errorf("rows bound %+v, want %+v, the temporary buffer", parameter.Buffer, rowsRange)
	}
	if rowsRange.Offset%256 != 0 {
		t.Errorf("the rows are at offset %d, want a multiple of 256", rowsRange.Offset)
	}
	calls := h.Calls()
	if _, ok := findChunkOf(calls, findChunks(calls), rowsRange, unsafe.Slice((*byte)(unsafe.Pointer(&rows[0])), 16)); !ok {
		t.Error("no chunk write holds the rows")
	}
}

func TestChunksAndDrawParameterBindGroupsUnusedForTemporaryIdleFramesAreFreed(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene passParameterScene
	spawnPassParameterHarness(t, h, &render.Config{TemporaryIdleFrames: 2}, 5, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makePassParameterScene(inventory)
		}
		if frameIndex > 1 {
			return
		}
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(scene.material)
			pass.SetUniform("object", [4]float32{float32(frameIndex)})
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
			pass.SetMaterial(scene.material)
			pass.SetBuffer("rows", render.BufferRange{Buffer: scene.lightGrid})
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
	})

	calls := h.Calls()
	chunks := findChunks(calls)
	if len(chunks) != 1 {
		t.Fatalf("render made %d chunks, want one, taken in the first two frames", len(chunks))
	}
	for chunk := range chunks {
		if frameIndex, ok := findReleaseFrame(calls, recording.CallKindReleaseBuffer, chunk); !ok || frameIndex != 4 {
			t.Errorf("the chunk was released after %d submits (%t), want after 4: at the end of the second frame it went unused", frameIndex, ok)
		}
	}
	created := findCreatedBindGroups(calls, parameterBindGroupLabel)
	if len(created) != 2 {
		t.Fatalf("render made %d bind groups of draw parameters, want two, one naming the chunk and one the light grid, each shared by both frames", len(created))
	}
	for group := range created {
		if frameIndex, ok := findReleaseFrame(calls, recording.CallKindReleaseBindGroup, group); !ok || frameIndex != 4 {
			t.Errorf("bind group %v of draw parameters was released after %d submits (%t), want after 4, though its buffer is held", group, frameIndex, ok)
		}
	}
}
