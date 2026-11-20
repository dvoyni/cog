package canvas_test

import (
	"image"
	"image/color"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// sheetsPath and tilePath are the paths addSheetsFiles puts its images at.
const (
	sheetsPath = "sheets.png"
	tilePath   = "tile.png"
)

// addSheetsFiles adds to files two opaque images: at sheetsPath, one of 12
// by 8 texels, whose left 8 columns are a sheet of 2 by 2 frames of 4
// texels, red, green, then blue and white, and whose right 4 columns are
// yellow above cyan, each 4 texels; and at tilePath, one of 4 texels whose
// quadrants are red at the top left, green at the top right, blue at the
// bottom left and white at the bottom right.
func addSheetsFiles(t testing.TB, files fstest.MapFS) {
	t.Helper()
	sheets := image.NewNRGBA(image.Rect(0, 0, 12, 8))
	for y := range 8 {
		for x := range 12 {
			sheets.SetNRGBA(x, y, [2][3]color.NRGBA{
				{opaqueRed, opaqueGreen, opaqueYellow},
				{opaqueBlue, opaqueWhite, opaqueCyan},
			}[y/4][x/4])
		}
	}
	addImageFile(t, files, sheetsPath, sheets)
	tile := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			tile.SetNRGBA(x, y, [2][2]color.NRGBA{{opaqueRed, opaqueGreen}, {opaqueBlue, opaqueWhite}}[y/2][x/2])
		}
	}
	addImageFile(t, files, tilePath, tile)
}

// The images of sheetsPath the tests draw: its sheet of 2 by 2 frames, cut
// from the rectangle of its left 8 columns, its yellow and its cyan, each a
// rectangle of its own, and its cyan again, about its top left.
var (
	sheetsSheet       = canvas.ImageFromFile(sheetsPath, canvas.ImageOptions{Sheet: canvas.Grid{Columns: 2, Rows: 2}, SourceRect: m.Recti{Width: 8, Height: 8}})
	sheetsYellow      = canvas.ImageFromFile(sheetsPath, canvas.ImageOptions{SourceRect: m.Recti{X: 8, Width: 4, Height: 4}})
	sheetsCyan        = canvas.ImageFromFile(sheetsPath, canvas.ImageOptions{SourceRect: m.Recti{X: 8, Y: 4, Width: 4, Height: 4}})
	sheetsCyanTopLeft = canvas.ImageFromFile(sheetsPath, canvas.ImageOptions{SourceRect: m.Recti{X: 8, Y: 4, Width: 4, Height: 4}, Pivot: canvas.PivotTopLeft})
)

// sheetsSprite is a sprite of sheetsPath the tests draw, and the colour it
// shows.
type sheetsSprite struct {
	name  string
	image canvas.Image
	frame uint32
	want  color.NRGBA
}

// sheetsSprites are the sprites of each of sheetsPath's frames and
// rectangles.
var sheetsSprites = []sheetsSprite{
	{"the sheet's first frame", sheetsSheet, 0, opaqueRed},
	{"the sheet's second frame", sheetsSheet, 1, opaqueGreen},
	{"the sheet's third frame", sheetsSheet, 2, opaqueBlue},
	{"the sheet's last frame", sheetsSheet, 3, opaqueWhite},
	{"the yellow rectangle", sheetsYellow, 0, opaqueYellow},
	{"the cyan rectangle", sheetsCyan, 0, opaqueCyan},
}

func TestASheetsFramesAndAFilesRectanglesDrawTheirOwnTexelsAndNeverBleed(t *testing.T) {
	// Each frame and rectangle of the file, 4 texels, is drawn at 20 units
	// and linearly filtered, probed half a unit inside its edges, where it
	// reads its gutter, and at 1 unit, minified to its mips, so each reads
	// only its own texels, never its neighbour in the file. The yellow,
	// drawn at its natural size scaled by 5, and the cyan, about its top
	// left, show each is its rectangle's size, 4 texels, not the file's.
	files := makeQuadrantsFiles(t)
	addSheetsFiles(t, files)
	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(list canvas.DrawList) {
		for index, sprite := range sheetsSprites {
			x := float32(-130 + 30*index)
			list.DrawSprite(canvas.Sprite{Image: sprite.image, Frame: sprite.frame, Transform: m.Translation2x3(x, -80), Size: m.Vec2{X: 20, Y: 20}})
			list.DrawSprite(canvas.Sprite{Image: sprite.image, Frame: sprite.frame, Transform: m.Translation2x3(x+0.5, -39.5), Size: m.Vec2{X: 1, Y: 1}})
		}
		list.DrawSprite(canvas.Sprite{Image: sheetsYellow, Transform: m.TRS2x3(m.Vec2{X: 60, Y: 0}, 0, m.Vec2{X: 5, Y: 5})})
		list.DrawSprite(canvas.Sprite{Image: sheetsCyanTopLeft, Transform: m.TRS2x3(m.Vec2{X: 100, Y: 0}, 0, m.Vec2{X: 5, Y: 5})})
	}, &fileMounter{files: files})

	var probes []probe
	for index, sprite := range sheetsSprites {
		centre := m.Vec2{X: float32(-130 + 30*index), Y: -80}
		probes = append(probes,
			probe{sprite.name + "'s centre", centre, sprite.want},
			probe{sprite.name + "'s left edge", centre.Add(m.Vec2{X: -9.5}), sprite.want},
			probe{sprite.name + "'s right edge", centre.Add(m.Vec2{X: 9.5}), sprite.want},
			probe{sprite.name + "'s top edge", centre.Add(m.Vec2{Y: -9.5}), sprite.want},
			probe{sprite.name + "'s bottom edge", centre.Add(m.Vec2{Y: 9.5}), sprite.want},
			probe{sprite.name + " minified", m.Vec2{X: centre.X + 0.5, Y: -39.5}, sprite.want},
		)
	}
	probes = append(probes,
		probe{"the yellow at its natural size's left edge", m.Vec2{X: 50.5}, opaqueYellow},
		probe{"the yellow at its natural size's bottom edge", m.Vec2{X: 60, Y: 9.5}, opaqueYellow},
		probe{"left of the yellow at its natural size", m.Vec2{X: 49.5}, opaqueBlack},
		probe{"below the yellow at its natural size", m.Vec2{X: 60, Y: 10.5}, opaqueBlack},
		probe{"the cyan about its top left's top left", m.Vec2{X: 100.5, Y: 0.5}, opaqueCyan},
		probe{"the cyan about its top left's bottom right", m.Vec2{X: 119.5, Y: 19.5}, opaqueCyan},
		probe{"right of the cyan about its top left", m.Vec2{X: 120.5, Y: 10}, opaqueBlack},
		probe{"above the cyan about its top left", m.Vec2{X: 110, Y: -0.5}, opaqueBlack},
	)
	picture.expect(t, probes)
}

func TestAFilesFramesAndRectanglesArePackedApartFromOneDecodeOfIt(t *testing.T) {
	// The sheet's 4 frames and the 2 rectangles are 6 cells, each filled
	// from the one texture render loaded the file into; images differing
	// only in their pivot share a cell.
	files := fstest.MapFS{}
	addSheetsFiles(t, files)
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: files})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Record(func(list canvas.DrawList) {
		for order, sprite := range sheetsSprites {
			list.DrawSprite(canvas.Sprite{Image: sprite.image, Frame: sprite.frame, Order: int32(order)})
		}
		list.DrawSprite(canvas.Sprite{Image: sheetsCyanTopLeft, Order: int32(len(sheetsSprites))})
	})
	h.Step()

	frame := h.Frames()[0].Render
	if loaded := findLoadedTextures(frame, sheetsPath); len(loaded) != 1 {
		t.Errorf("the file was loaded into %d textures, want one, its frames and rectangles sharing it", len(loaded))
	}
	packings := findPackings(t, frame, findTextureLabels(frame))
	sources := make([]string, len(packings))
	for index, packing := range packings {
		sources[index] = packing.source
	}
	if want := slices.Repeat([]string{sheetsPath}, len(sheetsSprites)); !slices.Equal(sources, want) {
		t.Errorf("the cells filled were filled from %v, want %v, a cell for each frame and rectangle, the cyan's pivot sharing its cell", sources, want)
	}
}

// isMixOf answers whether shown is a mix of two opaque colours, first and
// second, each of a channel of its own: neither alone, each lighting its
// channel by a third at least.
func isMixOf(shown, first, second color.NRGBA) bool {
	lit := func(channel uint8) bool { return channel >= 85 }
	firstChannels := [3]uint8{first.R, first.G, first.B}
	secondChannels := [3]uint8{second.R, second.G, second.B}
	shownChannels := [3]uint8{shown.R, shown.G, shown.B}
	for channel := range 3 {
		inFirst := firstChannels[channel] == 255
		inSecond := secondChannels[channel] == 255
		if (inFirst || inSecond) != lit(shownChannels[channel]) {
			return false
		}
	}
	return shown.A == 255 && shown != first && shown != second
}

func TestAWrappingImagesGutterHoldsItsOppositeEdgeAlongTheAxesItWraps(t *testing.T) {
	// The tile of 4 texels, red at its top left, is drawn at 40 units,
	// linearly filtered, plain and wrapping along x, along y and along
	// both, and probed half a unit inside its left edge's top half and its
	// top edge's left half, where it reads its gutter: that edge's red
	// continued, or, along an axis it wraps, mixed with its opposite edge,
	// green at its right and blue at its bottom. The sheet's first frame,
	// red, wrapping along both, reads only its own red, wrapped within the
	// frame, never the file's opposite edges.
	files := fstest.MapFS{}
	addSheetsFiles(t, files)
	tiles := []struct {
		name                string
		wrap                canvas.Wrap
		leftMixes, topMixes bool
	}{
		{"the plain tile", canvas.WrapNone, false, false},
		{"the tile wrapping along x", canvas.WrapX, true, false},
		{"the tile wrapping along y", canvas.WrapY, false, true},
		{"the tile wrapping along both", canvas.WrapBoth, true, true},
	}
	wrappedFrame := canvas.ImageFromFile(sheetsPath, canvas.ImageOptions{Sheet: canvas.Grid{Columns: 2, Rows: 2}, SourceRect: m.Recti{Width: 8, Height: 8}, Wrap: canvas.WrapBoth})
	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(list canvas.DrawList) {
		for index, tile := range tiles {
			image := canvas.ImageFromFile(tilePath, canvas.ImageOptions{Wrap: tile.wrap})
			list.DrawSprite(canvas.Sprite{Image: image, Transform: m.Translation2x3(float32(-120+50*index), 0), Size: m.Vec2{X: 40, Y: 40}})
		}
		list.DrawSprite(canvas.Sprite{Image: wrappedFrame, Transform: m.Translation2x3(100, 0), Size: m.Vec2{X: 20, Y: 20}})
	}, &fileMounter{files: files})

	for index, tile := range tiles {
		centre := m.Vec2{X: float32(-120 + 50*index)}
		for _, edge := range []struct {
			name     string
			point    m.Vec2
			mixes    bool
			opposite color.NRGBA
		}{
			{"left edge", centre.Add(m.Vec2{X: -19.5, Y: -10}), tile.leftMixes, opaqueGreen},
			{"top edge", centre.Add(m.Vec2{X: -10, Y: -19.5}), tile.topMixes, opaqueBlue},
		} {
			shown := picture.at(edge.point)
			switch {
			case edge.mixes && !isMixOf(shown, opaqueRed, edge.opposite):
				t.Errorf("%s's %s, at %v, is %v, want red mixed with its opposite edge's %v", tile.name, edge.name, edge.point, shown, edge.opposite)
			case !edge.mixes && shown != opaqueRed:
				t.Errorf("%s's %s, at %v, is %v, want its red continued, %v", tile.name, edge.name, edge.point, shown, opaqueRed)
			}
		}
	}
	picture.expect(t, []probe{
		{"the wrapping frame's left edge", m.Vec2{X: 90.5}, opaqueRed},
		{"the wrapping frame's top edge", m.Vec2{X: 100, Y: -9.5}, opaqueRed},
		{"the wrapping frame's right edge", m.Vec2{X: 109.5}, opaqueRed},
		{"the wrapping frame's bottom edge", m.Vec2{X: 100, Y: 9.5}, opaqueRed},
	})
}

func TestAFramePastItsSheetsLastOrARectanglePastItsFileDrawsNothingReportedOnceAndAgainOnceUnloaded(t *testing.T) {
	// The sheet's frames 4 and 9, past its last, and a rectangle reaching
	// past the file's right edge draw nothing and pack nothing, reported
	// once for the file's path, while the sheet's first frame beside them
	// draws. Unloading the path in the third frame makes the next draw look
	// again, and report again.
	files := fstest.MapFS{}
	addSheetsFiles(t, files)
	pastFile := canvas.ImageFromFile(sheetsPath, canvas.ImageOptions{SourceRect: m.Recti{X: 8, Y: 4, Width: 8, Height: 8}})
	h := canvastest.NewHarness(t)
	tool := &inspectionTool{duringFrame: func(frameIndex int, _ metronome.Beat, assets canvas.Assets, _ render.Frame) {
		if frameIndex == 2 {
			assets.Unload(sheetsPath)
		}
	}}
	h.Spawn(nil, nil, &fileMounter{files: files}, tool)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.ExpectReport(canvas.ReportKindFrameOutOfBounds)
	record := func(list canvas.DrawList) {
		list.DrawSprite(canvas.Sprite{Image: sheetsSheet, Frame: 4})
		list.DrawSprite(canvas.Sprite{Image: sheetsSheet, Frame: 0, Order: 1})
		list.DrawSprite(canvas.Sprite{Image: sheetsSheet, Frame: 9, Order: 2})
		list.DrawSprite(canvas.Sprite{Image: pastFile, Order: 3})
	}
	for range 2 {
		h.Record(record)
		h.Step()
	}

	if outOfBoundsCount := countReports(h, canvas.ReportKindFrameOutOfBounds); outOfBoundsCount != 1 {
		t.Errorf("the frames out of bounds were reported %d times in two frames, want once for the file's path", outOfBoundsCount)
	}
	for index, frame := range h.Frames() {
		var drawnFrames []uint32
		for _, inspectedBatch := range frame.Cameras[0].Batches {
			for _, entry := range inspectedBatch.Entries {
				drawnFrames = append(drawnFrames, entry.Sprite.Frame)
			}
		}
		if want := []uint32{0}; !slices.Equal(drawnFrames, want) {
			t.Errorf("frame %d drew the sprites of frames %v, want only the sheet's first, %v", index, drawnFrames, want)
		}
	}
	renderFrames := findRenderFrames(h.Frames())
	labels := findTextureLabels(renderFrames...)
	var packings []atlasPacking
	for _, frame := range renderFrames {
		packings = append(packings, findPackings(t, frame, labels)...)
	}
	if len(packings) != 1 || packings[0].source != sheetsPath {
		t.Errorf("the cells filled were %+v, want the sheet's first frame's alone", packings)
	}
	for range 2 {
		h.Record(record)
		h.Step()
	}
	if outOfBoundsCount := countReports(h, canvas.ReportKindFrameOutOfBounds); outOfBoundsCount != 2 {
		t.Errorf("the frames out of bounds were reported %d times once the path was unloaded, want twice", outOfBoundsCount)
	}
}

func TestImageSizeAnswersAFramesSizeOrARectanglesAndFalseForOnePastItsFile(t *testing.T) {
	h := canvastest.NewHarness(t)
	images := []canvas.Image{
		sheetsSheet,
		sheetsYellow,
		canvas.ImageFromFile(sheetsPath, canvas.ImageOptions{Sheet: canvas.Grid{Columns: 3}}),
		canvas.ImageFromFile(sheetsPath, canvas.ImageOptions{SourceRect: m.Recti{X: 8, Y: 4, Width: 8, Height: 8}}),
	}
	type sizeAnswer struct {
		size m.Vec2
		read bool
	}
	var answers []sizeAnswer
	tool := &inspectionTool{duringFrame: func(frameIndex int, _ metronome.Beat, assets canvas.Assets, _ render.Frame) {
		if frameIndex > 0 {
			return
		}
		for _, image := range images {
			size, read := assets.ImageSize(image)
			answers = append(answers, sizeAnswer{size, read})
		}
	}}
	files := fstest.MapFS{}
	addSheetsFiles(t, files)
	h.Spawn(nil, nil, &fileMounter{files: files}, tool)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()

	want := []sizeAnswer{{m.Vec2{X: 4, Y: 4}, true}, {m.Vec2{X: 4, Y: 4}, true}, {m.Vec2{X: 4, Y: 8}, true}, {}}
	if !slices.Equal(answers, want) {
		t.Errorf("ImageSize answered %v, want %v: a frame's size, a rectangle's, a strip's frame's of 3 columns and no rows, and false for a rectangle past the file", answers, want)
	}
}
