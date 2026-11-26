package canvas_test

import (
	"image/color"
	"math"
	"strconv"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/canvas/font"
	"github.com/dvoyni/cog/plugins/platform"
)

// bitmapFontConfig is canvas's config whose default font, JetBrains Mono,
// is a bitmap font of 10 pixels to the em: its 1000 units to the em are
// then 100 a pixel, so its advance of 600 is 6 whole pixels.
var bitmapFontConfig = &canvas.Config{DefaultFontSet: font.SetDescriptor{Default: font.Font{BitmapEm: 10}}}

func TestABitmapFontsRunIsLaidOutAndMeasuredAtAWholeMultipleOfItsPixelSize(t *testing.T) {
	// Each size rounds to the nearest whole multiple of the font's 10
	// pixels, 10 at least: a run of two letters, 0.6 em each, is laid out
	// that wide, and the font measured at that em.
	sizes := []struct {
		size, drawn float32
	}{
		{0, 20}, // Config.TextSize, 16
		{3, 10},
		{14.9, 10},
		{15, 20},
		{23, 20},
		{26, 30},
	}
	h := canvastest.NewHarness(t)
	layouts := make([]canvas.TextLayout, len(sizes))
	metrics := make([]font.Metrics, len(sizes))
	h.Spawn(nil, bitmapFontConfig, &fontLibraryUser{read: func(fonts canvas.FontLibrary, frame int) {
		for index, size := range sizes {
			layouts[index] = fonts.Layout(canvas.Text{String: "Hi", Size: size.size})
			metrics[index] = fonts.Metrics(canvas.FontSet{}, size.size, "")
		}
	}})
	h.Step()

	for index, size := range sizes {
		if width := layouts[index].Size().X; math.Abs(float64(width-1.2*size.drawn)) > 1e-3 {
			t.Errorf("a run of size %v is laid out %v wide, want %v, at an em of %v", size.size, width, 1.2*size.drawn, size.drawn)
		}
		// Metrics reads no Config.TextSize for a size of zero.
		if size.size != 0 {
			expectMetrics(t, "the bitmap font", metrics[index], builtInFontUnits, size.drawn)
		}
	}
}

func TestABitmapFontsRunReadsItsGlyphsWithNearestFilteringUnlessItNamesAFilter(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, bitmapFontConfig)
	h.OpenWindow(platform.Size{Width: 320, Height: 240})
	h.Record(func(list canvas.CommandList) {
		list.DrawText(canvas.Text{String: "a"})
		list.DrawText(canvas.Text{String: "b", Filter: canvas.FilterLinear, Order: 1})
	})

	h.Step()

	batches := h.Frames()[0].Cameras[0].Batches
	if len(batches) != 2 || batches[0].Filter != canvas.FilterNearest || batches[1].Filter != canvas.FilterLinear {
		t.Errorf("a run of no filter, then one of FilterLinear, drew batches %+v, want one of FilterNearest, then one of FilterLinear", batches)
	}
}

func TestABitmapFontsGlyphsLandOnWholePixelsUnderAPixelPerfectCamera(t *testing.T) {
	// 80×60 art pixels expanded into the window of 320×240 points, four
	// points a unit, cleared to black: a run of the bitmap font of size 23,
	// drawn at twice its pixel size, two units a pixel of it, at a
	// fractional position, and the same run outlined 36 units to its
	// right. Each unit holds one colour, each pixel of the font two units
	// square, so a run of one colour other than black spans an even number
	// of units, and the outline, a distance effect, draws nothing.
	const size = 23
	leftOrigin := m.Vec2{X: -30.3, Y: -20.6}
	rightOrigin := leftOrigin.Add(m.Vec2{X: 36})
	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), bitmapFontConfig, func(list canvas.CommandList) {
		list.SetCamera(canvas.Camera{Size: m.Vec2{X: 80, Y: 60}, PixelPerfect: true, Clear: true, ClearColor: m.Color{A: 1}})
		list.DrawText(canvas.Text{String: "Hi", Size: size, Pivot: canvas.PivotTopLeft, Transform: m.Translation2x3(leftOrigin.X, leftOrigin.Y)})
		list.DrawTextWith(canvas.Text{String: "Hi", Size: size, Pivot: canvas.PivotTopLeft, Transform: m.Translation2x3(rightOrigin.X, rightOrigin.Y)}, canvas.Outline(2, orange))
	})

	// The run's box, 1.2 em wide and 1.32 em tall at an em of 20, and a
	// unit about it.
	const firstColumn = -32
	const firstRow = -22
	const columns = 28
	const rows = 30
	unitColours := make([][]color.NRGBA, rows)
	litUnitCount := 0
	for row := range rows {
		unitColours[row] = make([]color.NRGBA, columns)
		for column := range columns {
			unit := m.Vec2{X: float32(firstColumn + column), Y: float32(firstRow + row)}
			unitColour, uniform := picture.findUnitColour(unit)
			if !uniform {
				t.Fatalf("the unit at %v holds more than one colour, want each pixel of the font on whole units", unit)
			}
			if outlinedColour, _ := picture.findUnitColour(unit.Add(m.Vec2{X: 36})); outlinedColour != unitColour {
				t.Errorf("the outlined run's unit at %v is %v, want the plain run's, %v, a bitmap font drawing no distance effect", unit.Add(m.Vec2{X: 36}), outlinedColour, unitColour)
			}
			if unitColour != opaqueBlack {
				litUnitCount++
			}
			unitColours[row][column] = unitColour
		}
	}
	if litUnitCount < 20 {
		t.Fatalf("the run lit %d units, want its glyphs", litUnitCount)
	}
	expectEvenRuns := func(name string, lineColours []color.NRGBA) {
		t.Helper()
		runStart := 0
		for index := 1; index <= len(lineColours); index++ {
			if index < len(lineColours) && lineColours[index] == lineColours[runStart] {
				continue
			}
			if lineColours[runStart] != opaqueBlack && (index-runStart)%2 != 0 {
				t.Errorf("%s holds %d units of %v from its unit %d, want an even number, each pixel of the font two units across", name, index-runStart, lineColours[runStart], runStart)
			}
			runStart = index
		}
	}
	for row := range rows {
		expectEvenRuns("row "+strconv.Itoa(firstRow+row), unitColours[row])
	}
	for column := range columns {
		lineColours := make([]color.NRGBA, rows)
		for row := range rows {
			lineColours[row] = unitColours[row][column]
		}
		expectEvenRuns("column "+strconv.Itoa(firstColumn+column), lineColours)
	}
}
