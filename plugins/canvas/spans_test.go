package canvas_test

import (
	"image/color"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
)

// The pictures of icons makeIconFiles holds: a white square of 16 texels, a
// white one twice as wide, and an opaque green square of 16 texels.
const (
	squareIconPath = "square.png"
	wideIconPath   = "wide.png"
	greenIconPath  = "green.png"
)

// makeIconFiles answers the files of the icons' pictures these tests
// register.
func makeIconFiles(t testing.TB) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	addSolidFile(t, files, squareIconPath, 16, 16, white)
	addSolidFile(t, files, wideIconPath, 32, 16, white)
	addSolidFile(t, files, greenIconPath, 16, 16, opaqueGreen)
	return files
}

// builtInCapHeight is JetBrains Mono's cap height, in em.
const builtInCapHeight = 0.73

func TestRegisteringAnIconAgainLaysOutAgainTheLayoutsHoldingItAlone(t *testing.T) {
	// The prompt is the square icon in frame 0, laid out 7.3 units wide,
	// and the wide one from frame 1, which forgets the prompt's layout, laid
	// out anew 14.6 units wide; the label naming no icon keeps its layout.
	capHeight := float32(builtInCapHeight * 10)
	prompt := canvas.Text{Spans: []canvas.Span{{Icon: "prompt"}}, Size: 10}
	label := canvas.Text{Spans: []canvas.Span{{Text: "jump"}}, Size: 10}
	var widths []float32
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: makeIconFiles(t)}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			fonts.RegisterIcon("prompt", canvas.IconDescriptor{Image: canvas.ImageFromFile([]string{squareIconPath, wideIconPath}[frame])})
		},
		read: func(fonts canvas.FontLibrary, frame int) {
			widths = append(widths, fonts.Layout(prompt).LineWidth(0))
			fonts.Layout(label)
		},
	})
	h.Step()
	h.Step()

	frames := h.Frames()
	if shaped := findFramesLayingOut(frames, prompt); !slices.Equal(shaped, []int{0, 1}) {
		t.Errorf("the prompt is shaped in frames %v, want 0 and 1, its layout forgotten as its icon is registered again", shaped)
	}
	expectNear(t, "the prompt of the square icon", widths[0], capHeight)
	expectNear(t, "the prompt of the wide icon", widths[1], 2*capHeight)
	if shaped := findFramesLayingOut(frames, label); !slices.Equal(shaped, []int{0}) {
		t.Errorf("the label is shaped in frames %v, want frame 0 alone, its layout kept", shaped)
	}
}

func TestAnIconTheTextMaterialCannotDrawIsRefusedReportedOnceAndItsNameLeftUnregistered(t *testing.T) {
	// An icon of linear colour, which the sRGB sprite atlas does not hold,
	// one of a file that is missing, and one of a frame past its sheet are
	// refused, and their names lay out as no name registered does.
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindIconRefused)
	h.ExpectReport(canvas.ReportKindIconNotRegistered)
	var layout canvas.TextLayout
	h.Spawn(nil, nil, &fileMounter{files: makeIconFiles(t)}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			fonts.RegisterIcon("linear", canvas.IconDescriptor{Image: canvas.ImageFromFile(squareIconPath, canvas.ImageOptions{ColorSpace: canvas.ColorSpaceLinear})})
			fonts.RegisterIcon("missing", canvas.IconDescriptor{Image: canvas.ImageFromFile("missing.png")})
			fonts.RegisterIcon("past", canvas.IconDescriptor{Image: canvas.ImageFromFile(wideIconPath), Frame: 1})
		},
		read: func(fonts canvas.FontLibrary, frame int) {
			layout = fonts.Layout(canvas.Text{Spans: []canvas.Span{{Icon: "linear"}, {Icon: "missing"}, {Icon: "past"}}, Size: 10})
		},
	})
	h.Step()

	expectLineWidths(t, "three missing glyphs", layout, 3*builtInAdvance*10)
	if count := countReports(h, canvas.ReportKindIconRefused); count != 3 {
		t.Errorf("%d icons refused reported, want each of 3 once", count)
	}
}

// countSpanPixels answers how many pixels of picture within rect, a
// rectangle of the world, white text, of equal channels, lights, and how
// many red text lights, red alone.
func (p windowPicture) countSpanPixels(rect m.Rect) (white, red int) {
	from := p.pixel(rect.Min())
	to := p.pixel(rect.Max())
	for y := from.Y; y < to.Y; y++ {
		for x := from.X; x < to.X; x++ {
			pixel := p.picture.NRGBAAt(x, y)
			switch {
			case pixel.R > 0 && pixel.G == 0 && pixel.B == 0:
				red++
			case pixel.R > 0 && pixel.R == pixel.G && pixel.G == pixel.B:
				white++
			}
		}
	}
	return white, red
}

func TestAColouredWordWrapsWithItsParagraphInItsOwnTint(t *testing.T) {
	// "ab " in the run's white and "cd" in red: the red word goes to the
	// second line alone in red.
	expectRedWordOnSecondLine(t, canvas.Text{Spans: []canvas.Span{{Text: "ab "}, {Text: "cd", Tint: m.Color{R: 1, A: 1}}}})
}

// expectRedWordOnSecondLine draws text, "ab " in the run's white and "cd"
// in red, at an em of 40 units, wrapped at 100, its box's top left at
// (-100, -60), and fails t unless the first line lights white alone and the
// second, 52.8 units below it, red alone.
func expectRedWordOnSecondLine(t *testing.T, text canvas.Text) {
	t.Helper()
	text.Size = 40
	text.WrapWidth = 100
	text.Pivot = canvas.PivotTopLeft
	text.Transform = m.Translation2x3(-100, -60)
	picture := captureDrawing(t, func(list canvas.CommandList) { list.DrawText(text) })

	lineHeight := builtInFontUnits.atSize(40).LineHeight
	firstWhite, firstRed := picture.countSpanPixels(m.Rect{X: -100, Y: -60, Width: 100, Height: lineHeight})
	secondWhite, secondRed := picture.countSpanPixels(m.Rect{X: -100, Y: -60 + lineHeight, Width: 100, Height: lineHeight})
	if firstWhite == 0 || firstRed != 0 {
		t.Errorf("the first line lights %d white pixels and %d red, want ab in white alone", firstWhite, firstRed)
	}
	if secondRed == 0 || secondWhite != 0 {
		t.Errorf("the second line lights %d red pixels and %d white, want cd in red alone", secondRed, secondWhite)
	}
}

func TestAnIconSitsOnTheBaselineAtTheCapHeightInOneBatchWithItsGlyphs(t *testing.T) {
	// At an em of 40 units, its box's top left at (-100, -60): the green
	// icon, untinted, keeps its colour in the red run, 29.2 units square,
	// its bottom on the baseline, 40.8 units below the box's top, nothing
	// drawn just below it or above it; the white one, tinted, takes its
	// span's blue, raised a quarter of an em, 10 units, above the baseline,
	// nothing drawn just below it. The run's glyph and icons are one batch of
	// three instances.
	h := canvastest.NewHarness(t)
	text := canvas.Text{
		Spans:     []canvas.Span{{Icon: "green"}, {Icon: "white", Tint: m.Color{B: 1, A: 1}}, {Text: "a"}},
		Size:      40,
		Tint:      m.Color{R: 1, A: 1},
		Pivot:     canvas.PivotTopLeft,
		Transform: m.Translation2x3(-100, -60),
	}
	picture := captureHarnessDrawing(t, h, nil, func(canvas.CommandList) {}, &fileMounter{files: makeIconFiles(t)}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			if frame == 0 {
				fonts.RegisterIcon("green", canvas.IconDescriptor{Image: canvas.ImageFromFile(greenIconPath)})
				fonts.RegisterIcon("white", canvas.IconDescriptor{Image: canvas.ImageFromFile(squareIconPath), Tinted: true, Offset: 0.25})
			}
		},
		draw: func(list canvas.CommandList, frame int) {
			list.DrawText(text)
		},
	})

	side := float32(builtInCapHeight * 40)
	metrics := builtInFontUnits.atSize(40)
	baseline := -60 + (metrics.LineHeight-metrics.Ascent-metrics.Descent)/2 + metrics.Ascent
	middle := baseline - side/2
	picture.expect(t, []probe{
		{name: "the green icon's middle", point: m.Vec2{X: -100 + side/2, Y: middle}, want: opaqueGreen},
		{name: "the raised tinted icon's middle", point: m.Vec2{X: -100 + 1.5*side, Y: middle - 10}, want: opaqueBlue},
		{name: "below the raised tinted icon", point: m.Vec2{X: -100 + 1.5*side, Y: baseline - 5}, want: opaqueBlack},
		{name: "below the green icon", point: m.Vec2{X: -100 + side/2, Y: baseline + 2}, want: opaqueBlack},
		{name: "above the green icon", point: m.Vec2{X: -100 + side/2, Y: baseline - side - 2}, want: opaqueBlack},
	})
	frames := h.Frames()
	batches := frames[len(frames)-1].Cameras[0].Batches
	if len(batches) != 1 || batches[0].Material != canvas.TextMaterial || batches[0].InstanceCount != 3 {
		t.Errorf("the run drew batches %+v, want one of TextMaterial, of its glyph and two icons", batches)
	}
}

func TestAnIconOfANameNotRegisteredDrawsTheMissingGlyphBoxReportedOnce(t *testing.T) {
	// Each of two runs naming an icon no one registered draws JetBrains
	// Mono's missing-glyph box, frame after frame, and the name is
	// reported once.
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindIconNotRegistered)
	picture := captureHarnessDrawing(t, h, nil, func(list canvas.CommandList) {
		list.DrawText(canvas.Text{Spans: []canvas.Span{{Icon: "button-a"}}, Size: 40, Transform: m.Translation2x3(-40, 0)})
		list.DrawText(canvas.Text{Spans: []canvas.Span{{Icon: "button-a"}}, Size: 40, Transform: m.Translation2x3(40, 0)})
	})

	for _, centre := range []m.Vec2{{X: -40}, {X: 40}} {
		inks := picture.findColumnInk(centre.Add(m.Vec2{X: -10}), centre.Add(m.Vec2{X: 10}), -15, 15)
		if slices.Max(inks) < 255 {
			t.Errorf("the run at %v drew columns of ink %v, want JetBrains Mono's missing-glyph box", centre, inks)
		}
	}
	if count := countReports(h, canvas.ReportKindIconNotRegistered); count != 1 {
		t.Errorf("%d icons not registered reported, want the name once", count)
	}
}
