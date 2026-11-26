package canvas_test

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/platform/filesystems"
)

// The tests in this file are canvas's concurrency tests: run under
// go test -race, each fails once the guard of shared state its comment
// names is taken away. Two jobs of one task recording into its command list
// together are TestTwoJobsOfOneTaskAndOtherTasksRecordTogetherIntoTheFrame,
// which commandList's mutex keeps apart.

// concurrentCount is how many images, runs or files each test's tasks use
// together in a frame: enough that tasks starting together overlap.
const concurrentCount = 64

// concurrentAlphabet is the characters a run of the glyph test draws, each
// a glyph of its own in JetBrains Mono.
const concurrentAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// addConcurrentImages adds to files concurrentCount PNGs under directory,
// the one of each index 1 + index%5 texels wide and 2 tall, and answers
// their paths, by index.
func addConcurrentImages(t testing.TB, files fstest.MapFS, directory string) []string {
	t.Helper()
	paths := make([]string, concurrentCount)
	for index := range paths {
		paths[index] = fmt.Sprintf("%s/%d.png", directory, index)
		addSolidFile(t, files, paths[index], 1+index%5, 2, opaqueGreen)
	}
	return paths
}

// drawImagesOf answers a record drawing a sprite of each of paths,
// interning the path as it draws it.
func drawImagesOf(paths []string) recordFunc {
	return func(list canvas.CommandList, _ canvas.Assets, _ int) {
		for _, path := range paths {
			list.DrawSprite(canvas.Sprite{Image: canvas.ImageFromFile(path)})
		}
	}
}

// drawTextsOf answers a record drawing each of runs.
func drawTextsOf(runs []canvas.Text) recordFunc {
	return func(list canvas.CommandList, _ canvas.Assets, _ int) {
		for _, run := range runs {
			list.DrawText(run)
		}
	}
}

// countDrawnImages answers how many sprites frame drew of each image.
func countDrawnImages(frame canvas.Inspection) map[canvas.Image]int {
	counts := map[canvas.Image]int{}
	for _, entry := range findDrawnEntries(frame) {
		if entry.Kind == canvas.EntryKindSprite {
			counts[entry.Sprite.Image]++
		}
	}
	return counts
}

func TestTwoTasksDrawingImagesFirstTogetherInternAndPackEachOnce(t *testing.T) {
	// Two tasks intern the same new paths and draw a sprite of each,
	// together: the image table's mutex has each path interned once, so
	// both draw one Image of it, which the frame packs once.
	files := fstest.MapFS{}
	paths := addConcurrentImages(t, files, "concurrent/first-draw")
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: files}, &concurrentUser{records: []recordFunc{drawImagesOf(paths), drawImagesOf(paths)}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()

	frame := h.Frames()[0]
	drawnCounts := countDrawnImages(frame)
	for _, path := range paths {
		if count := drawnCounts[canvas.ImageFromFile(path)]; count != 2 {
			t.Errorf("%s was drawn %d times as its Image, want twice, once by each task", path, count)
		}
	}
	packedCounts := map[canvas.Image]int{}
	for _, packed := range frame.PackedImages {
		packedCounts[packed.Image]++
	}
	if len(frame.PackedImages) != concurrentCount || len(packedCounts) != concurrentCount {
		t.Errorf("the frame packed %d images, %d of them distinct, want each of the %d drawn once", len(frame.PackedImages), len(packedCounts), concurrentCount)
	}
}

func TestTwoTasksLayingOutRunsOfTheSameCharactersMakeEachGlyphOnce(t *testing.T) {
	// Two tasks draw the alphabet at 16 and at 24, together: the runs are
	// laid out apart, finding each character's font in the default set
	// under its character mutex, and share their glyphs, which the glyph
	// table's mutex has made once each. The inspector's mutex keeps both
	// jobs' notes of what they made, as an inspection is pending.
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &concurrentUser{records: []recordFunc{
		drawTextsOf([]canvas.Text{{String: concurrentAlphabet, Size: 16}}),
		drawTextsOf([]canvas.Text{{String: concurrentAlphabet, Size: 24}}),
	}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()

	frame := h.Frames()[0]
	madeCount := 0
	for _, made := range frame.MadeGlyphs {
		madeCount += made.Count
	}
	if madeCount != len(concurrentAlphabet) || len(frame.MadeLayouts) != 2 {
		t.Errorf("two runs of the alphabet laid out together made %d glyphs in %+v and %d layouts, want each of its %d glyphs once in 2 layouts", madeCount, frame.MadeGlyphs, len(frame.MadeLayouts), len(concurrentAlphabet))
	}
}

func TestTwoTasksDrawingTheSameRunsTogetherCacheEachLayoutOnce(t *testing.T) {
	// Two tasks draw the same runs, together, in each of three frames: the
	// layout cache's mutex has each laid out once, in the first frame,
	// whose drawn glyphs one job keeps by compare-and-swap, each job
	// assigning fields to a copy of its own, so neither compacts the
	// layout's glyphs, spaces dropped, under the other, which would draw a
	// glyph twice; both stamp its last use, an atomic, in every frame, so
	// none is freed. Each run holds a Cyrillic letter of its own, whose
	// field its first draw makes outside the glyph table's lock.
	runs := make([]canvas.Text, concurrentCount)
	glyphCount := 0
	for index := range runs {
		runs[index] = canvas.Text{String: fmt.Sprintf("run %d %c", index, 'А'+rune(index)), Size: 12}
		glyphCount += len([]rune(strings.ReplaceAll(runs[index].String, " ", "")))
	}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &concurrentUser{records: []recordFunc{drawTextsOf(runs), drawTextsOf(runs)}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 3 {
		h.Step()
	}

	for index, frame := range h.Frames() {
		wantMade := 0
		if index == 0 {
			wantMade = concurrentCount
		}
		textCount := 0
		for _, entry := range findDrawnEntries(frame) {
			if entry.Kind == canvas.EntryKindText {
				textCount++
			}
		}
		instanceCount := 0
		for _, camera := range frame.Cameras {
			for _, batch := range camera.Batches {
				instanceCount += int(batch.InstanceCount)
			}
		}
		if len(frame.MadeLayouts) != wantMade || textCount != 2*concurrentCount || instanceCount != 2*glyphCount {
			t.Errorf("frame %d laid out %d runs and drew %d of %d glyphs, want %d laid out and each of the %d drawn twice, of %d glyphs", index, len(frame.MadeLayouts), textCount, instanceCount, wantMade, concurrentCount, 2*glyphCount)
		}
	}
}

func TestAnIconRegisteredWhileTasksLayOutRunsHoldingItIsOrderedByFontLibrarysLock(t *testing.T) {
	// fontLibraryUser's task registers the prompt as the square icon, then
	// the wide one, in alternate frames, and new names besides, while two
	// tasks lay out the prompt and a third draws it: FontLibrary's write
	// lock runs the registration before or after them, never during, so
	// each layout holds one icon or the other whole.
	capHeight := float32(builtInCapHeight * 10)
	prompt := canvas.Text{Spans: []canvas.Span{{Icon: "prompt"}}, Size: 10}
	prompts := make([]canvas.Text, concurrentCount)
	for index := range prompts {
		prompts[index] = prompt
	}
	layOut := func(fonts canvas.FontLibrary, _ int) {
		for range concurrentCount {
			if width := fonts.Layout(prompt).LineWidth(0); math.Abs(float64(width-capHeight)) > 1e-3 && math.Abs(float64(width-2*capHeight)) > 1e-3 {
				t.Errorf("the prompt was laid out %v wide, want %v, of the square icon, or %v, of the wide one", width, capHeight, 2*capHeight)
			}
		}
	}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: makeIconFiles(t)}, &concurrentUser{
		records: []recordFunc{drawTextsOf(prompts)},
		reads:   []func(canvas.FontLibrary, int){layOut, layOut},
	}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			fonts.RegisterIcon("prompt", canvas.IconDescriptor{Image: canvas.ImageFromFile([]string{squareIconPath, wideIconPath}[frame%2])})
			for index := range concurrentCount {
				fonts.RegisterIcon(fmt.Sprint("icon ", frame, " ", index), canvas.IconDescriptor{Image: canvas.ImageFromFile(squareIconPath)})
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 4 {
		h.Step()
	}
}

func TestImageSizeAskedFromTwoTasksWhileAFrameDrawsAnswersEachImagesSize(t *testing.T) {
	// Two tasks ask the size of each image while canvas packs them, first
	// drawn by a third: the image size table's mutex keeps their headers
	// read apart, and the file stamp table's both tasks' and the drawing
	// job's stamps.
	files := fstest.MapFS{}
	paths := addConcurrentImages(t, files, "concurrent/sized")
	askSizes := func(assets canvas.Assets, _ int) {
		for index, path := range paths {
			size, read := assets.ImageSize(canvas.ImageFromFile(path))
			if want := (m.Vec2{X: float32(1 + index%5), Y: 2}); !read || size != want {
				t.Errorf("%s's size is %v (read %t), want %v", path, size, read, want)
			}
		}
	}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: files}, &concurrentUser{
		records:      []recordFunc{drawImagesOf(paths)},
		whileDrawing: []func(canvas.Assets, int){askSizes, askSizes},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 2 {
		h.Step()
	}
}

func TestPreloadAndUnloadFromTwoTasksWhileAFrameDrawsLeaveEveryImageDrawn(t *testing.T) {
	// One task preloads every image and another unloads each, while canvas
	// applies the requests queued before and draws them: the residency
	// request queue's mutex keeps the requests and the drawing job's
	// taking of them apart, and every sprite is drawn, packed again where
	// it was unloaded.
	files := fstest.MapFS{}
	paths := addConcurrentImages(t, files, "concurrent/resident")
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: files}, &concurrentUser{
		records: []recordFunc{drawImagesOf(paths)},
		whileDrawing: []func(canvas.Assets, int){
			func(assets canvas.Assets, _ int) {
				for _, path := range paths {
					assets.Preload(canvas.ImageFromFile(path))
				}
			},
			func(assets canvas.Assets, _ int) {
				for _, path := range paths {
					assets.Unload(path)
				}
			},
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 3 {
		h.Step()
	}

	for index, frame := range h.Frames() {
		if drawnCounts := countDrawnImages(frame); len(drawnCounts) != concurrentCount {
			t.Errorf("frame %d drew sprites of %d images, want each of the %d", index, len(drawnCounts), concurrentCount)
		}
	}
}

func TestReloadChangedWhileAFrameDrawsNewImagesPacksEachNewImage(t *testing.T) {
	// A task calls ReloadChanged again and again in each frame while
	// canvas packs the frame's new images and a second task asks their
	// sizes: the file stamp table's mutex keeps its snapshots apart from
	// their stamps, and the font reload request, an atomic, apart from the
	// drawing job's taking of it. No file changes, so each frame packs its
	// new images alone.
	memory := filesystems.NewMemoryFS()
	const frameCount = 3
	var paths [frameCount][]string
	for frame := range paths {
		for index := range concurrentCount {
			path := fmt.Sprintf("concurrent/reloaded/%d/%d.png", frame, index)
			putSolidFile(t, memory, path, 2, 2, opaqueGreen)
			paths[frame] = append(paths[frame], path)
		}
	}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: memory}, &concurrentUser{
		records: []recordFunc{func(list canvas.CommandList, assets canvas.Assets, frame int) {
			drawImagesOf(paths[frame])(list, assets, frame)
			list.DrawText(canvas.Text{String: "reloaded", Size: 12})
		}},
		whileDrawing: []func(canvas.Assets, int){
			func(assets canvas.Assets, _ int) {
				for range 4 * concurrentCount {
					assets.ReloadChanged()
				}
			},
			func(assets canvas.Assets, frame int) {
				for _, path := range paths[min(frame+1, frameCount-1)] {
					assets.ImageSize(canvas.ImageFromFile(path))
				}
			},
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range frameCount {
		h.Step()
	}

	for frame, inspection := range h.Frames() {
		packed := map[canvas.Image]bool{}
		for _, image := range inspection.PackedImages {
			packed[image.Image] = true
		}
		missing := 0
		for _, path := range paths[frame] {
			if !packed[canvas.ImageFromFile(path)] {
				missing++
			}
		}
		if missing != 0 || len(inspection.PackedImages) != concurrentCount {
			t.Errorf("frame %d packed %d images, %d of its %d new ones missing, want its new ones alone", frame, len(inspection.PackedImages), missing, concurrentCount)
		}
	}
}

func TestMaterialsMadeAndReleasedFromTwoTasksWhileAFrameDrawsLeaveTheDrawnMaterialDrawing(t *testing.T) {
	// Two tasks make and release materials while canvas draws shapes of a
	// material made in the first frame: the material table's mutex keeps
	// their slots apart from each other's and from the drawing job's
	// reads, so each frame draws its shapes in the material they name.
	makeAndRelease := func(assets canvas.Assets, _ int) {
		for range 8 {
			assets.ReleaseMaterial(assets.MakeMaterial(canvas.MaterialDescriptor{Label: "passing", Shader: tintShaderPath}))
		}
	}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, newMaterialMounter(t), &materialRecorder{
		descriptors: []canvas.MaterialDescriptor{{Label: "tint", Shader: tintShaderPath}},
		record: func(list canvas.CommandList, _ canvas.Assets, materials []canvas.Material, _ int) {
			for range concurrentCount {
				list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}, Material: materials[0]})
			}
		},
	}, &concurrentUser{whileDrawing: []func(canvas.Assets, int){makeAndRelease, makeAndRelease}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 3 {
		h.Step()
	}

	for index, frame := range h.Frames() {
		var labels []string
		for _, camera := range frame.Cameras {
			for _, batch := range camera.Batches {
				labels = append(labels, batch.MaterialLabel)
			}
		}
		if !slices.Equal(labels, []string{"tint"}) {
			t.Errorf("frame %d drew batches of materials %q, want one of tint", index, labels)
		}
	}
}

func TestRunsOfMalformedMarkupDrawnFromTwoTasksAreEachReportedOnce(t *testing.T) {
	// Two tasks draw the same runs of markup that does not parse, together:
	// the font library's malformed mutex notes each string once, so each is
	// reported once.
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindMarkupMalformed)
	runs := make([]canvas.Text, concurrentCount)
	for index := range runs {
		runs[index] = canvas.Text{String: fmt.Sprintf(`\c:#%d{`, index), Markup: true, Size: 12}
	}
	h.Spawn(nil, nil, &concurrentUser{records: []recordFunc{drawTextsOf(runs), drawTextsOf(runs)}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 2 {
		h.Step()
	}

	if count := countReports(h, canvas.ReportKindMarkupMalformed); count != concurrentCount {
		t.Errorf("%d malformed runs reported over two frames of two tasks drawing them, want each of the %d once", count, concurrentCount)
	}
}

func TestRunsOfCharactersNoFontHasDrawnFromTwoTasksAreReportedOnceForTheSet(t *testing.T) {
	// Two tasks draw runs of Hangul, which JetBrains Mono, the default
	// set's only font, has none of, together: the set's slot notes in its
	// atomic flag that a missing character was reported, so the set is
	// reported once.
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindCharacterMissing)
	runs := make([]canvas.Text, concurrentCount)
	for index := range runs {
		runs[index] = canvas.Text{String: string(rune('가' + index)), Size: 12}
	}
	h.Spawn(nil, nil, &concurrentUser{records: []recordFunc{drawTextsOf(runs), drawTextsOf(runs)}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()

	if count := countReports(h, canvas.ReportKindCharacterMissing); count != 1 {
		t.Errorf("the default set was reported %d times for its missing characters, want once", count)
	}
}

func TestATaskRecordingWhileCanvasOpensAndDrawsTheFrameLeavesTheFramesOwnEntriesDrawn(t *testing.T) {
	// A task records fills off render's frame event, while canvas opens,
	// sorts and draws the frame, so each is refused or recorded as the
	// draw state's recording flag, an atomic, says at that moment; the fills
	// recorded in the frame are drawn whole.
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindRecordedOutsideFrame)
	h.Spawn(nil, nil, &concurrentUser{
		records: []recordFunc{func(list canvas.CommandList, _ canvas.Assets, _ int) {
			for range concurrentCount {
				list.DrawShape(canvas.Shape{Order: 1})
			}
		}},
		lateRecords: []func(canvas.CommandList, int){func(list canvas.CommandList, _ int) {
			for range 1000 * concurrentCount {
				list.DrawShape(canvas.Shape{Order: 2})
			}
		}},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 3 {
		h.Step()
	}

	for index, frame := range h.Frames() {
		onTime := 0
		for _, shape := range findDrawnShapes(frame) {
			if shape.Order == 1 {
				onTime++
			}
		}
		if onTime != concurrentCount {
			t.Errorf("frame %d drew %d of the fills recorded in it, want each of the %d", index, onTime, concurrentCount)
		}
	}
}

func TestAnInspectionRequestedWhileAFrameDrawsLeavesEachStepInspected(t *testing.T) {
	// Two tasks request an inspection while canvas draws each frame, beside
	// the harness's own request: the inspector's pending request, an
	// atomic, keeps them apart from the drawing job's taking of it, and
	// each frame is inspected once.
	request := func(assets canvas.Assets, _ int) { assets.RequestInspection() }
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &concurrentUser{
		records:      []recordFunc{func(list canvas.CommandList, _ canvas.Assets, _ int) { list.DrawShape(canvas.Shape{}) }},
		whileDrawing: []func(canvas.Assets, int){request, request},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 3 {
		h.Step()
	}

	for index, frame := range h.Frames() {
		if shapes := findDrawnShapes(frame); len(shapes) != 1 {
			t.Errorf("frame %d's inspection drew %d shapes, want its fill", index, len(shapes))
		}
	}
}
