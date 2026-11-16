package rendertest_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

func TestAMadeBufferOrTextureExistsFilledAtOnceWithEveryUsageAndIsFreedAtTheEndOfTheFrameItsMakerReleasesIt(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindReleaseRefused)
	atlasTexels := make([]byte, 2*2*4*3)
	for index := range atlasTexels {
		atlasTexels[index] = byte(index)
	}
	// rows, odd and empty are made buffers, of three uint32s, five bytes and
	// none; atlas is a made 2D-array texture of three layers filled from
	// atlasTexels, and history a made depth texture left empty. calls is
	// every gfx call recorded when the last of them was made.
	var rows, odd, empty render.Buffer
	var atlas, history render.Texture
	var calls []recording.Call
	h.Spawn(nil, &loaderPlugin{
		files: fstest.MapFS{},
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			switch frameIndex {
			case 0:
				rows = inventory.MakeBuffer("rows", render.BufferUsageFlagStorage, []uint32{1, 2, 3})
				odd = inventory.MakeBuffer("odd", render.BufferUsageFlagVertex|render.BufferUsageFlagCopySrc, []byte{1, 2, 3, 4, 5})
				empty = inventory.MakeBuffer[byte]("empty", render.BufferUsageFlagUniform, nil)
				atlas = inventory.MakeTexture(render.TextureDescriptor{Label: "atlas", Width: 2, Height: 2, LayerCount: 3, Format: render.TextureFormatRGBA8Unorm}, atlasTexels)
				history = inventory.MakeTexture[byte](render.TextureDescriptor{Label: "history", Width: 4, Height: 4, Format: render.TextureFormatDepth24Plus}, nil)
				calls = h.Calls()
				inventory.ReleaseBuffer(rows)
				inventory.ReleaseTexture(atlas)
				inventory.ReleaseBuffer(rows)
			case 1:
				inventory.ReleaseBuffer(odd)
				inventory.ReleaseBuffer(empty)
				inventory.ReleaseTexture(history)
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	if kinds, want := makeReportKinds(h), []render.ReportKind{render.ReportKindReleaseRefused}; !slices.Equal(kinds, want) {
		t.Errorf("render reported kinds %v, want %v: a made buffer released twice", kinds, want)
	}
	bufferCalls := readBufferCalls(calls)
	for _, want := range []struct {
		buffer render.Buffer
		size   uint64
		usage  gfx.BufferUsageFlags
		data   []byte
		what   string
	}{
		{buffer: rows, size: 12, usage: gfx.BufferUsageFlagStorage | gfx.BufferUsageFlagCopyDst, data: binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, 1), 2), 3), what: "three uint32s for storage"},
		{buffer: odd, size: 8, usage: gfx.BufferUsageFlagVertex | gfx.BufferUsageFlagCopySrc | gfx.BufferUsageFlagCopyDst, data: []byte{1, 2, 3, 4, 5, 0, 0, 0}, what: "five bytes of vertices"},
	} {
		created, ok := bufferCalls.findCreated[recording.CreateBufferArguments](want.buffer)
		if !ok {
			t.Errorf("the buffer of %s was not made once when its make returned", want.what)
			continue
		}
		if created.Descriptor.Size != want.size || created.Descriptor.Usage != want.usage {
			t.Errorf("the buffer of %s was made of %d bytes with usage %#x, want %d with %#x, its own and the copy destination", want.what, created.Descriptor.Size, created.Descriptor.Usage, want.size, want.usage)
		}
		writes := bufferCalls.find(recording.CallKindWriteBuffer, want.buffer)
		if len(writes) != 1 || !bytes.Equal(writes[0].arguments.(recording.WriteBufferArguments).Data, want.data) {
			t.Errorf("the buffer of %s was written %d times when its make returned, want once, with %v", want.what, len(writes), want.data)
		}
	}
	if empty == (render.Buffer{}) || len(findCallIndices(calls, recording.CallKindCreateBuffer)) != 2 {
		t.Errorf("a made buffer of no bytes answered %v and render made %d gfx buffers, want a buffer of its own and two, the others'", empty, len(findCallIndices(calls, recording.CallKindCreateBuffer)))
	}
	textureCalls := readTextureCalls(calls)
	for _, want := range []struct {
		texture render.Texture
		usage   gfx.TextureUsageFlags
		what    string
	}{
		{texture: atlas, usage: gfx.TextureUsageFlagRenderAttachment | gfx.TextureUsageFlagTextureBinding | gfx.TextureUsageFlagStorageBinding | gfx.TextureUsageFlagCopySrc | gfx.TextureUsageFlagCopyDst, what: "the RGBA8Unorm atlas"},
		{texture: history, usage: gfx.TextureUsageFlagRenderAttachment | gfx.TextureUsageFlagTextureBinding | gfx.TextureUsageFlagCopySrc | gfx.TextureUsageFlagCopyDst, what: "the Depth24Plus history"},
	} {
		created, ok := textureCalls.findCreated[recording.CreateTextureArguments](want.texture)
		if !ok {
			t.Errorf("%s was not made once when its make returned", want.what)
			continue
		}
		if created.Descriptor.Usage != want.usage {
			t.Errorf("%s was made with usage %#x, want %#x, every usage its format allows", want.what, created.Descriptor.Usage, want.usage)
		}
	}
	atlasWrites := textureCalls.find(recording.CallKindWriteTexture, atlas)
	if len(atlasWrites) != 1 {
		t.Fatalf("the atlas was written %d times when its make returned, want once, every layer at once", len(atlasWrites))
	}
	wantAtlasSize := gfx.Extent3D{Width: 2, Height: 2, DepthOrArrayLayers: 3}
	wantAtlasLayout := gfx.ImageDataLayout{BytesPerRow: 8, RowsPerImage: 2}
	if arguments := atlasWrites[0].arguments.(recording.WriteTextureArguments); arguments.Destination.MipLevel != 0 || arguments.Destination.Origin != (gfx.Origin3D{}) || arguments.Size != wantAtlasSize || arguments.Layout != wantAtlasLayout || !bytes.Equal(arguments.Data, atlasTexels) {
		t.Errorf("the atlas was written %+v laid out as %+v of %v at mip level %d from %+v, want mip level 0 from its corner, %+v laid out as %+v of %v", arguments.Size, arguments.Layout, arguments.Data, arguments.Destination.MipLevel, arguments.Destination.Origin, wantAtlasSize, wantAtlasLayout, atlasTexels)
	}
	if writes := textureCalls.find(recording.CallKindWriteTexture, history); len(writes) != 0 {
		t.Errorf("the history, made with no data, was written %d times, want none", len(writes))
	}
	allBufferCalls := readBufferCalls(h.Calls())
	allTextureCalls := readTextureCalls(h.Calls())
	rowsFrameIndex, rowsFreed := allBufferCalls.findFreeingFrame(rows)
	oddFrameIndex, oddFreed := allBufferCalls.findFreeingFrame(odd)
	atlasFrameIndex, atlasFreed := allTextureCalls.findFreeingFrame(atlas)
	historyFrameIndex, historyFreed := allTextureCalls.findFreeingFrame(history)
	for _, freed := range []struct {
		frameIndex, wantFrameIndex int
		once                       bool
		what                       string
	}{
		{frameIndex: rowsFrameIndex, wantFrameIndex: 0, once: rowsFreed, what: "rows"},
		{frameIndex: oddFrameIndex, wantFrameIndex: 1, once: oddFreed, what: "odd"},
		{frameIndex: atlasFrameIndex, wantFrameIndex: 0, once: atlasFreed, what: "the atlas"},
		{frameIndex: historyFrameIndex, wantFrameIndex: 1, once: historyFreed, what: "the history"},
	} {
		if !freed.once || freed.frameIndex != freed.wantFrameIndex {
			t.Errorf("%s was freed: %v, at the end of frame %d, want once, at the end of frame %d, which released it", freed.what, freed.once, freed.frameIndex, freed.wantFrameIndex)
		}
	}

}

func TestWritesLandBeforeTheSubmitInTheOrderMadeAndWriteTextureWritesARegionOfOneMipLevelAndOneLayer(t *testing.T) {
	h := rendertest.NewHarness(t)
	// uniforms is a made buffer of 8 float32s, atlas a made 4×4 R8Unorm
	// texture of 2 mip levels and 3 layers.
	var uniforms render.Buffer
	var atlas render.Texture
	firstPage := slices.Repeat([]byte{1}, 16)
	secondPage := slices.Repeat([]byte{2}, 16)
	h.Spawn(nil, &loaderPlugin{
		files: fstest.MapFS{},
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			if frameIndex > 0 {
				return
			}
			uniforms = inventory.MakeBuffer("uniforms", render.BufferUsageFlagUniform, make([]float32, 8))
			atlas = inventory.MakeTexture[byte](render.TextureDescriptor{Label: "atlas", Width: 4, Height: 4, LayerCount: 3, MipLevelCount: 2, Format: render.TextureFormatR8Unorm}, nil)
			inventory.WriteBuffer(uniforms, 16, []float32{1, 2})
			inventory.WriteBuffer(uniforms, 16, []float32{3, 4})
			inventory.WriteTexture(atlas, render.TextureRegion{MipLevel: 1, Layer: 2, X: 1, Width: 1, Height: 2}, []byte{7, 8})
			inventory.WriteTexture(atlas, render.TextureRegion{Layer: 1}, firstPage)
			inventory.WriteTexture(atlas, render.TextureRegion{Layer: 1}, secondPage)
		},
	})

	h.Step()

	calls := h.Calls()
	submits := findCallIndices(calls, recording.CallKindSubmit)
	if len(submits) != 1 {
		t.Fatalf("the frame was submitted %d times, want once", len(submits))
	}
	bufferWrites := readBufferCalls(calls).find(recording.CallKindWriteBuffer, uniforms)
	if len(bufferWrites) != 3 {
		t.Fatalf("the uniforms were written %d times, want 3: when made, and twice to one region", len(bufferWrites))
	}
	for index, want := range [][]float32{{1, 2}, {3, 4}} {
		write := bufferWrites[index+1]
		arguments := write.arguments.(recording.WriteBufferArguments)
		wantData := binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, math.Float32bits(want[0])), math.Float32bits(want[1]))
		if arguments.Offset != 16 || !bytes.Equal(arguments.Data, wantData) || write.index > submits[0] {
			t.Errorf("write %d of the uniforms' region was %v at offset %d, at call %d, want %v at 16 before the submit, at %d", index, arguments.Data, arguments.Offset, write.index, wantData, submits[0])
		}
	}
	textureWrites := readTextureCalls(calls).find(recording.CallKindWriteTexture, atlas)
	if len(textureWrites) != 3 {
		t.Fatalf("the atlas was written %d times, want 3", len(textureWrites))
	}
	for index, want := range []struct {
		mipLevel uint32
		origin   gfx.Origin3D
		size     gfx.Extent3D
		layout   gfx.ImageDataLayout
		data     []byte
	}{
		{mipLevel: 1, origin: gfx.Origin3D{X: 1, Z: 2}, size: gfx.Extent3D{Width: 1, Height: 2, DepthOrArrayLayers: 1}, layout: gfx.ImageDataLayout{BytesPerRow: 1, RowsPerImage: 2}, data: []byte{7, 8}},
		{mipLevel: 0, origin: gfx.Origin3D{Z: 1}, size: gfx.Extent3D{Width: 4, Height: 4, DepthOrArrayLayers: 1}, layout: gfx.ImageDataLayout{BytesPerRow: 4, RowsPerImage: 4}, data: firstPage},
		{mipLevel: 0, origin: gfx.Origin3D{Z: 1}, size: gfx.Extent3D{Width: 4, Height: 4, DepthOrArrayLayers: 1}, layout: gfx.ImageDataLayout{BytesPerRow: 4, RowsPerImage: 4}, data: secondPage},
	} {
		write := textureWrites[index]
		arguments := write.arguments.(recording.WriteTextureArguments)
		if arguments.Destination.MipLevel != want.mipLevel || arguments.Destination.Origin != want.origin || arguments.Size != want.size || arguments.Layout != want.layout || !bytes.Equal(arguments.Data, want.data) || write.index > submits[0] {
			t.Errorf("the atlas's write %d was %+v laid out as %+v at mip level %d from %+v, of %v, at call %d, want %+v laid out as %+v at mip level %d from %+v, of %v, before the submit, at %d", index, arguments.Size, arguments.Layout, arguments.Destination.MipLevel, arguments.Destination.Origin, arguments.Data, write.index, want.size, want.layout, want.mipLevel, want.origin, want.data, submits[0])
		}
	}
}

func TestAWriteThatDoesNotFitItsBufferOrTextureIsRefusedAndReported(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindWriteRefused)
	var uniforms render.Buffer
	var atlas render.Texture
	h.Spawn(nil, &loaderPlugin{
		files: fstest.MapFS{},
		duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
			if frameIndex > 0 {
				return
			}
			uniforms = inventory.MakeBuffer("uniforms", render.BufferUsageFlagUniform, make([]float32, 8))
			empty := inventory.MakeBuffer[byte]("empty", render.BufferUsageFlagUniform, nil)
			atlas = inventory.MakeTexture[byte](render.TextureDescriptor{Label: "atlas", Width: 4, Height: 4, LayerCount: 3, MipLevelCount: 2, Format: render.TextureFormatR8Unorm}, nil)
			depth := inventory.MakeTexture[byte](render.TextureDescriptor{Label: "depth", Width: 4, Height: 4, Format: render.TextureFormatDepth24Plus}, nil)
			multisampled := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "multisampled", Width: 4, Height: 4, SampleCount: 4, Format: render.TextureFormatRGBA8Unorm})
			inventory.WriteBuffer(uniforms, 2, []byte{1, 2, 3, 4})
			inventory.WriteBuffer(uniforms, 0, []byte{1, 2, 3})
			inventory.WriteBuffer(uniforms, 28, []uint32{1, 2})
			inventory.WriteBuffer(uniforms, 36, []uint32{1})
			inventory.WriteBuffer(render.Buffer{}, 0, []uint32{1})
			inventory.WriteBuffer(empty, 0, []uint32{1})
			inventory.WriteTexture(atlas, render.TextureRegion{MipLevel: 2}, []byte{1})
			inventory.WriteTexture(atlas, render.TextureRegion{Layer: 3}, make([]byte, 16))
			inventory.WriteTexture(atlas, render.TextureRegion{MipLevel: 1, X: 1, Width: 2, Height: 1}, []byte{1, 2})
			inventory.WriteTexture(atlas, render.TextureRegion{X: 5}, []byte{1})
			inventory.WriteTexture(atlas, render.TextureRegion{}, make([]byte, 15))
			inventory.WriteTexture(depth, render.TextureRegion{}, make([]byte, 64))
			inventory.WriteTexture(multisampled, render.TextureRegion{}, make([]byte, 64))
			inventory.WriteTexture(render.Texture{}, render.TextureRegion{}, []byte{1})
			inventory.MakeTexture(render.TextureDescriptor{Label: "short", Width: 2, Height: 2, LayerCount: 2, Format: render.TextureFormatRGBA8Unorm}, make([]byte, 16))
		},
	})

	h.Step()

	if kinds, want := makeReportKinds(h), slices.Repeat([]render.ReportKind{render.ReportKindWriteRefused}, 15); !slices.Equal(kinds, want) {
		t.Errorf("render reported kinds %v, want %v: six refused buffer writes, eight refused texture writes and a texture made from data too short", kinds, want)
	}
	calls := h.Calls()
	if writes := readBufferCalls(calls).find(recording.CallKindWriteBuffer, uniforms); len(writes) != 1 {
		t.Errorf("the uniforms were written %d times, want once, when made", len(writes))
	}
	if writes := findCallIndices(calls, recording.CallKindWriteTexture); len(writes) != 0 {
		t.Errorf("gfx was asked for %d texture writes, want none", len(writes))
	}
}
