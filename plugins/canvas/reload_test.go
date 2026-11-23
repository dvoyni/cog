package canvas_test

import (
	"image/color"
	"os"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/libs/meshfile"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/platform/filesystems"
	"github.com/dvoyni/cog/plugins/render"
	"golang.org/x/image/font/gofont/goregular"
)

func TestReloadChangedPacksChangedImagesAgainAndLoadsAChangedMeshFileAgainReadingNoUnchangedFile(t *testing.T) {
	// Red, a converted image and a chunk's mesh file are drawn, and an image
	// measured, beside a still image and a still mesh file. The second
	// frame puts each changed file again and calls ReloadChanged, reading
	// neither still file: it then packs red and the converted image again,
	// loads the chunk's quad and measures the new size, and the third frame,
	// calling it again with nothing changed since, makes nothing.
	const (
		stillPath     = "still.png"
		convertedPath = "converted.png"
		measuredPath  = "measured.png"
		chunkPath     = "meshes/chunk.mesh"
		stillMeshPath = "meshes/still.mesh"
	)
	opaqueGreen := color.NRGBA{G: 255, A: 255}
	opaqueBlue := color.NRGBA{B: 255, A: 255}
	memory := filesystems.NewMemoryFS()
	putSolidFile(t, memory, redPath, 4, 4, color.NRGBA{R: 255, A: 255})
	putSolidFile(t, memory, stillPath, 4, 4, opaqueGreen)
	putSolidFile(t, memory, convertedPath, 4, 4, opaqueGreen)
	putSolidFile(t, memory, measuredPath, 4, 4, opaqueGreen)
	putMeshFile(t, memory, chunkPath, triangleVertices, nil, meshfile.TopologyTriangleList)
	putMeshFile(t, memory, stillMeshPath, triangleVertices, nil, meshfile.TopologyTriangleList)
	files := &countingFS{files: memory, openCounts: map[string]int{}}
	trim := trimmer{convertedCount: &atomic.Int32{}}
	red := canvas.ImageFromFile(redPath)
	still := canvas.ImageFromFile(stillPath)
	converted := canvas.ImageFromFile(convertedPath, canvas.ImageOptions{Convert: trim})
	measured := canvas.ImageFromFile(measuredPath)
	chunk := canvas.MeshFileFromFile(chunkPath)
	stillMesh := canvas.MeshFileFromFile(stillMeshPath)
	var measuredSizes [3]m.Vec2
	var stillOpenCount int
	tool := &inspectionTool{duringFrame: func(frameIndex int, _ metronome.Beat, assets canvas.Assets, _ render.Frame) {
		switch frameIndex {
		case 1:
			putSolidFile(t, memory, redPath, 8, 8, opaqueBlue)
			putSolidFile(t, memory, convertedPath, 4, 4, opaqueBlue)
			putSolidFile(t, memory, measuredPath, 8, 2, opaqueBlue)
			putMeshFile(t, memory, chunkPath, makeSquareVertices(10), squareIndices, meshfile.TopologyTriangleList)
			openCount := files.countOpens(stillPath) + files.countOpens(stillMeshPath)
			assets.ReloadChanged()
			stillOpenCount = files.countOpens(stillPath) + files.countOpens(stillMeshPath) - openCount
		case 2:
			assets.ReloadChanged()
		}
		measuredSizes[frameIndex], _ = assets.ImageSize(measured)
	}}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: files}, tool)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 3 {
		h.Record(func(list canvas.DrawList) {
			list.DrawSprite(canvas.Sprite{Image: red})
			list.DrawSprite(canvas.Sprite{Image: still, Order: 1})
			list.DrawSprite(canvas.Sprite{Image: converted, Order: 2})
			list.DrawMesh(canvas.Mesh{File: chunk, Order: 3})
			list.DrawMesh(canvas.Mesh{File: stillMesh, Order: 4})
		})
		h.Step()
	}

	if stillOpenCount != 0 {
		t.Errorf("ReloadChanged opened the still image and mesh file %d times, want none", stillOpenCount)
	}
	frames := h.Frames()
	for index, want := range []struct {
		packed []canvas.InspectedImage
		loaded []canvas.MeshFile
	}{
		{packed: []canvas.InspectedImage{{Image: red}, {Image: still}, {Image: converted}}, loaded: []canvas.MeshFile{chunk, stillMesh}},
		{packed: []canvas.InspectedImage{{Image: red}, {Image: converted}}, loaded: []canvas.MeshFile{chunk}},
		{},
	} {
		if !reflect.DeepEqual(frames[index].PackedImages, want.packed) || !reflect.DeepEqual(frames[index].LoadedMeshFiles, want.loaded) {
			t.Errorf("frame %d packed %+v and loaded %+v, want %+v and %+v", index, frames[index].PackedImages, frames[index].LoadedMeshFiles, want.packed, want.loaded)
		}
	}
	if loaded := findLoadedTextures(frames[1].Render, redPath); len(loaded) != 1 {
		t.Errorf("the frame reloading red loaded %v of it, want its file loaded again", loaded)
	} else if writes := findTextureWrites(frames[1].Render, loaded[0]); len(writes) == 0 || writes[0].Region.Width != 8 || writes[0].Region.Height != 8 {
		t.Errorf("red was loaded again and written %+v, want the new file's 8×8 texels", writes)
	}
	if count := trim.convertedCount.Load(); count != 2 {
		t.Errorf("the converted image was converted %d times, want twice: drawn, then reloaded", count)
	}
	if want := [3]m.Vec2{{X: 4, Y: 4}, {X: 8, Y: 2}, {X: 8, Y: 2}}; measuredSizes != want {
		t.Errorf("the measured image measured %v over the frames, want %v, its header read again once reloaded", measuredSizes, want)
	}
	for index, wantIndexCount := range []uint32{0, 6, 6} {
		// The camera's pass is its list's last, after any packing.
		passes := findCanvasLists(frames[index].Render)[0].Passes
		var indexCounts []uint32
		for _, draw := range passes[len(passes)-1].Draws {
			indexCounts = append(indexCounts, draw.IndexCount)
		}
		if want := []uint32{0, wantIndexCount, 0}; !slices.Equal(indexCounts, want) {
			t.Errorf("frame %d drew draws of %v indices, want the sprites, then the chunk's mesh, then the still mesh, %v", index, indexCounts, want)
		}
	}
}

func TestReloadChangedLoadsAChangedFontFileAgainLayingItsSetsRunsOutAnewAndReadsNoUnchangedOne(t *testing.T) {
	// A set of go.ttf and a set of still.ttf each draw a run. The second
	// frame puts go.ttf again with a line gap and calls ReloadChanged: once
	// that frame is drawn, the set of go.ttf, under the same FontSet,
	// measures the gap and lays its run out anew, making its glyphs again;
	// still.ttf is never read again, and its run never laid out again.
	memory := filesystems.NewMemoryFS()
	putFile(t, memory, "go.ttf", goregular.TTF)
	putFile(t, memory, "still.ttf", goregular.TTF)
	files := &countingFS{files: memory, openCounts: map[string]int{}}
	var changed, still canvas.FontSet
	var lineGaps [4]float32
	var stillOpenCount int
	tool := &inspectionTool{duringFrame: func(frameIndex int, _ metronome.Beat, assets canvas.Assets, _ render.Frame) {
		if frameIndex == 1 {
			putFile(t, memory, "go.ttf", makeGappedFont(goregular.TTF, goGappedLineGap))
			stillOpenCount = files.countOpens("still.ttf")
			assets.ReloadChanged()
		}
	}}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: files}, tool, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			if frame == 0 {
				changed = fonts.MakeFontSet(canvas.FontSetDescriptor{Label: "changed", Default: canvas.Font{Path: "go.ttf"}})
				still = fonts.MakeFontSet(canvas.FontSetDescriptor{Label: "still", Default: canvas.Font{Path: "still.ttf"}})
			}
		},
		read: func(fonts canvas.FontLibrary, frame int) {
			if frame < len(lineGaps) {
				lineGaps[frame] = fonts.Metrics(changed, goFontUnits.unitsPerEm, "").LineGap
			}
		},
		draw: func(list canvas.DrawList, _ int) {
			list.DrawText(canvas.Text{String: "ab", Font: changed})
			list.DrawText(canvas.Text{String: "cd", Font: still, Order: 1})
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range len(lineGaps) {
		h.Step()
	}

	if count := files.countOpens("still.ttf") - stillOpenCount; count != 0 {
		t.Errorf("still.ttf was opened %d times after ReloadChanged, want none", count)
	}
	if want := [4]float32{0, 0, goGappedLineGap, goGappedLineGap}; lineGaps != want {
		t.Errorf("the set of go.ttf measured line gaps %v over the frames, want %v: the new file's from the frame after the reload", lineGaps, want)
	}
	frames := h.Frames()
	// The first frame lays out and makes everything, so the checks begin at
	// the second.
	for offset, wantString := range []string{"", "ab", ""} {
		index := offset + 1
		var made []string
		for _, layout := range frames[index].MadeLayouts {
			made = append(made, layout.String)
		}
		var glyphCount int
		for _, glyphs := range frames[index].MadeGlyphs {
			glyphCount += glyphs.Count
		}
		switch {
		case wantString == "" && (len(made) != 0 || glyphCount != 0):
			t.Errorf("frame %d made layouts %v and %d glyphs, want none", index, made, glyphCount)
		case wantString != "" && (len(made) != 1 || made[0] != wantString || glyphCount != 2):
			t.Errorf("frame %d made layouts %v and %d glyphs, want %q's and its 2 glyphs, laid out in the set made again", index, made, glyphCount, wantString)
		}
	}
}

// findTextureWrites answers the writes into texture that land in frame.
func findTextureWrites(frame render.Inspection, texture render.Texture) []render.InspectedWrite {
	var writes []render.InspectedWrite
	for _, write := range frame.Writes {
		if write.Texture == texture {
			writes = append(writes, write)
		}
	}
	return writes
}

func TestReloadChangedReloadsAChanged2DMaterialsShaderThroughRender(t *testing.T) {
	// The tint material's shader is put again at the second frame, which
	// calls ReloadChanged: render reloads it, swapped in at the third
	// frame's start, which makes its pipeline again.
	memory := filesystems.NewMemoryFS()
	for _, path := range []string{tintShaderPath, tintShaderPath + ".json", "assets/canvas.wgsl"} {
		putModuleFile(t, memory, path)
	}
	tool := &inspectionTool{duringFrame: func(frameIndex int, _ metronome.Beat, assets canvas.Assets, _ render.Frame) {
		if frameIndex == 1 {
			shader, err := memory.ReadFile(tintShaderPath)
			if err != nil {
				t.Fatal(err)
			}
			putFile(t, memory, tintShaderPath, append(shader, "\n// Changed.\n"...))
			assets.ReloadChanged()
		}
	}}
	recorder := &materialRecorder{
		descriptors: []canvas.MaterialDescriptor{{Label: "tint", Shader: tintShaderPath}},
		record: func(list canvas.DrawList, _ canvas.Assets, materials []canvas.Material, _ int) {
			list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 10, Height: 10}, Material: materials[0]})
		},
	}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: memory}, recorder, tool)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 3 {
		h.Step()
	}

	var madeCounts []int
	for _, frame := range h.Frames() {
		madeCounts = append(madeCounts, len(frame.Render.MadePipelines))
	}
	if madeCounts[0] == 0 || madeCounts[1] != 0 || madeCounts[2] == 0 {
		t.Errorf("the frames made %v pipelines, want some, then none, then the reloaded shader's", madeCounts)
	}
}

func TestReloadChangedDropsAConvertedFilePreloadedBeforeItsFirstDrawOnceItChanges(t *testing.T) {
	// A converted image is preloaded at the first frame, its file decoded
	// and held, then put again at 8×8 texels and reloaded at the second,
	// and first drawn at the third, which converts the new file.
	const preloadedPath = "preloaded.png"
	memory := filesystems.NewMemoryFS()
	putSolidFile(t, memory, preloadedPath, 4, 4, color.NRGBA{R: 255, A: 255})
	preloaded := canvas.ImageFromFile(preloadedPath, canvas.ImageOptions{Convert: trimmer{convertedCount: &atomic.Int32{}}})
	tool := &inspectionTool{duringFrame: func(frameIndex int, _ metronome.Beat, assets canvas.Assets, _ render.Frame) {
		switch frameIndex {
		case 0:
			assets.Preload(preloaded)
		case 1:
			putSolidFile(t, memory, preloadedPath, 8, 8, color.NRGBA{B: 255, A: 255})
			assets.ReloadChanged()
		}
	}}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: memory}, tool)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Step()
	h.Step()
	h.Record(makeSpriteRecord(preloaded))
	h.Step()

	made := findLoadedTextures(h.Frames()[2].Render, preloadedPath)
	if len(made) != 1 {
		t.Fatalf("the frame first drawing the preloaded image made %v of it, want one texture of its output", made)
	}
	if writes := findTextureWrites(h.Frames()[2].Render, made[0]); len(writes) == 0 || writes[0].Region.Width != 8 || writes[0].Region.Height != 8 {
		t.Errorf("its output was written %+v, want the new file's 8×8 texels", writes)
	}
}

// putSolidFile puts into memory a PNG at path of width×height texels, each
// of colour.
func putSolidFile(t testing.TB, memory *filesystems.MemoryFS, path string, width, height int, colour color.NRGBA) {
	t.Helper()
	putFile(t, memory, path, encodePNG(t, path, makeSolidPicture(width, height, colour)))
}

// putModuleFile puts into memory the file at path in the module, at that
// path.
func putModuleFile(t testing.TB, memory *filesystems.MemoryFS, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	putFile(t, memory, path, data)
}
