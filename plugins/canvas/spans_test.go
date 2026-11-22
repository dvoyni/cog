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
// white one twice as wide, a white one twice as tall, and an opaque green
// square of 16 texels.
const (
	squareIconPath = "square.png"
	wideIconPath   = "wide.png"
	tallIconPath   = "tall.png"
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
	addSolidFile(t, files, tallIconPath, 16, 32, white)
	addSolidFile(t, files, greenIconPath, 16, 16, opaqueGreen)
	return files
}

// builtInCapHeight is JetBrains Mono's cap height, in em.
const builtInCapHeight = 0.73

// layOutWithIcons answers each of texts laid out by FontLibrary's reading
// side, as layOutTexts lays them out, in a frame whose write task registers
// each of icons under its name first, under canvas configured by default
// on h, a harness not yet spawned, the files of makeIconFiles mounted.
func layOutWithIcons(t *testing.T, h *canvastest.Harness, icons map[string]canvas.IconDescriptor, texts ...canvas.Text) []canvas.TextLayout {
	t.Helper()
	var layouts []canvas.TextLayout
	h.Spawn(nil, nil, &fileMounter{files: makeIconFiles(t)}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			for name, icon := range icons {
				fonts.RegisterIcon(name, icon)
			}
		},
		read: func(fonts canvas.FontLibrary, frame int) {
			for _, text := range texts {
				layouts = append(layouts, fonts.Layout(text))
			}
		},
	})
	h.Step()
	return layouts
}

func TestSpansAreShapedAndWrappedAsTheirTextsEndToEndAre(t *testing.T) {
	// A sentence cut into spans, one of them tinted, wraps at 60 units as
	// the sentence written whole does: each line as wide, each cluster in
	// its place.
	sentence := canvas.Text{String: "Press the red button now", Size: 10, WrapWidth: 60}
	spanned := sentence
	spanned.String = ""
	spanned.Spans = []canvas.Span{{Text: "Press the "}, {Text: "red", Tint: orange}, {Text: " button now"}}
	layouts, _ := layOutTexts(t, nil, sentence, spanned)
	whole, spans := layouts[0], layouts[1]

	if whole.LineCount() < 2 || spans.LineCount() != whole.LineCount() {
		t.Fatalf("the spans lay out on %d lines, the sentence on %d, want as many, more than one", spans.LineCount(), whole.LineCount())
	}
	for line := range whole.LineCount() {
		expectNear(t, "a line of the spans", spans.LineWidth(line), whole.LineWidth(line))
	}
	for cluster := range len(sentence.String) + 1 {
		expectPositionNear(t, "a cluster of the spans", spans.ClusterPosition(cluster), whole.ClusterPosition(cluster))
	}
}

func TestAnIconIsTheCapHeightTimesItsScaleTallItsWidthByItsFramesAspect(t *testing.T) {
	// At an em of 10 units, JetBrains Mono's cap height is 7.3: the wide
	// icon, of a frame twice as wide as tall, is 14.6 units wide between a
	// and b; the tall one, of a frame half as wide, at twice the cap height
	// is 7.3; and the second frame of the wide picture cut into two
	// columns, a square, is 7.3.
	capHeight := float32(builtInCapHeight * 10)
	h := canvastest.NewHarness(t)
	layouts := layOutWithIcons(t, h, map[string]canvas.IconDescriptor{
		"wide":   {Image: canvas.ImageFromFile(wideIconPath)},
		"tall":   {Image: canvas.ImageFromFile(tallIconPath), Scale: 2},
		"column": {Image: canvas.ImageFromFile(wideIconPath, canvas.ImageOptions{Sheet: canvas.Grid{Columns: 2}}), Frame: 1},
	},
		canvas.Text{Spans: []canvas.Span{{Text: "a"}, {Icon: "wide"}, {Text: "b"}}, Size: 10},
		canvas.Text{Spans: []canvas.Span{{Icon: "tall"}}, Size: 10},
		canvas.Text{Spans: []canvas.Span{{Icon: "column"}}, Size: 10},
	)

	expectLineWidths(t, "a, the wide icon and b", layouts[0], 2*builtInAdvance*10+2*capHeight)
	baseline := findBaseline(builtInFontUnits.atSize(10), 1)
	expectPositionNear(t, "b, after the wide icon", layouts[0].ClusterPosition(2), m.Vec2{X: builtInAdvance*10 + 2*capHeight, Y: baseline})
	expectLineWidths(t, "the tall icon at twice the cap height", layouts[1], capHeight)
	expectLineWidths(t, "a square frame of the wide picture", layouts[2], capHeight)
}

func TestALineBreaksAroundAnIconAsAroundALetterNeverBetweenItAndTheLetterAfterIt(t *testing.T) {
	// "ab ", a square icon and an s, wrapped at 28 units at an em of 10:
	// "ab " and the icon fit, 25.3 units, and the s does not, so the line
	// breaks at the space, before the icon, never between the icon and the
	// s, as Unicode's line breaking would around the object replacement
	// character.
	capHeight := float32(builtInCapHeight * 10)
	h := canvastest.NewHarness(t)
	layouts := layOutWithIcons(t, h, map[string]canvas.IconDescriptor{"square": {Image: canvas.ImageFromFile(squareIconPath)}},
		canvas.Text{Spans: []canvas.Span{{Text: "ab "}, {Icon: "square"}, {Text: "s"}}, Size: 10, WrapWidth: 28},
	)

	expectLineWidths(t, "ab, then the icon and its s", layouts[0], 2*builtInAdvance*10, capHeight+builtInAdvance*10)
}

func TestRegisteringAnIconAgainLaysOutAgainTheLayoutsHoldingItAlone(t *testing.T) {
	// The prompt is the square icon in frame 0, laid out 7.3 units wide,
	// and the wide one from frame 1, which forgets the prompt's layout, laid
	// out anew 14.6 units wide; the label naming no icon keeps its layout.
	capHeight := float32(builtInCapHeight * 10)
	prompt := canvas.Text{Spans: []canvas.Span{{Icon: "prompt"}}, Size: 10}
	label := canvas.Text{Spans: []canvas.Span{{Text: "jump"}}, Size: 10}
	var promptsCached, labels []*canvas.CachedLayout
	var widths []float32
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: makeIconFiles(t)}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			fonts.RegisterIcon("prompt", canvas.IconDescriptor{Image: canvas.ImageFromFile([]string{squareIconPath, wideIconPath}[frame])})
		},
		read: func(fonts canvas.FontLibrary, frame int) {
			promptsCached = append(promptsCached, canvas.FindCachedLayout(fonts, prompt))
			widths = append(widths, fonts.Layout(prompt).LineWidth(0))
			fonts.Layout(label)
			labels = append(labels, canvas.FindCachedLayout(fonts, label))
		},
	})
	h.Step()
	h.Step()

	if promptsCached[0] != nil || promptsCached[1] != nil {
		t.Errorf("the prompt's layout is cached as %v before it is laid out in each frame, want none, forgotten as its icon is registered again", promptsCached)
	}
	expectNear(t, "the prompt of the square icon", widths[0], capHeight)
	expectNear(t, "the prompt of the wide icon", widths[1], 2*capHeight)
	if labels[0] == nil || labels[1] != labels[0] {
		t.Errorf("the label's layout is %p, then %p, want the first kept", labels[0], labels[1])
	}
}

func TestAnIconOfANameNotRegisteredIsTheDefaultFontsMissingGlyphReportedOnce(t *testing.T) {
	// JetBrains Mono's missing glyph advances 0.6 em, as its letters do,
	// between a and b, in two runs, and the name is reported once.
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindIconNotRegistered)
	layouts := layOutWithIcons(t, h, nil,
		canvas.Text{Spans: []canvas.Span{{Text: "a"}, {Icon: "nothing"}, {Text: "b"}}, Size: 10},
		canvas.Text{Spans: []canvas.Span{{Icon: "nothing"}}, Size: 20},
	)

	expectLineWidths(t, "a, the missing glyph and b", layouts[0], 3*builtInAdvance*10)
	expectLineWidths(t, "the missing glyph alone", layouts[1], builtInAdvance*20)
	if count := countReports(h, canvas.ReportKindIconNotRegistered); count != 1 {
		t.Errorf("%d icons not registered reported, want the name once", count)
	}
}

func TestAnIconTheTextMaterialCannotDrawIsRefusedReportedOnceAndItsNameLeftUnregistered(t *testing.T) {
	// An icon of linear colour, which the sRGB sprite atlas does not hold,
	// one of a file that is missing, and one of a frame past its sheet are
	// refused, and their names lay out as no name registered does.
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindIconRefused)
	h.ExpectReport(canvas.ReportKindIconNotRegistered)
	layouts := layOutWithIcons(t, h, map[string]canvas.IconDescriptor{
		"linear":  {Image: canvas.ImageFromFile(squareIconPath, canvas.ImageOptions{ColorSpace: canvas.ColorSpaceLinear})},
		"missing": {Image: canvas.ImageFromFile("missing.png")},
		"past":    {Image: canvas.ImageFromFile(wideIconPath), Frame: 1},
	},
		canvas.Text{Spans: []canvas.Span{{Icon: "linear"}, {Icon: "missing"}, {Icon: "past"}}, Size: 10},
	)

	expectLineWidths(t, "three missing glyphs", layouts[0], 3*builtInAdvance*10)
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
	// "ab " in the run's white and "cd" in red, at an em of 40 units,
	// wrapped at 100: the red word goes to the second line, 52.8 units
	// below the first, alone in red.
	picture := captureDrawing(t, func(list canvas.DrawList) {
		list.DrawText(canvas.Text{
			Spans:     []canvas.Span{{Text: "ab "}, {Text: "cd", Tint: m.Color{R: 1, A: 1}}},
			Size:      40,
			WrapWidth: 100,
			Pivot:     canvas.PivotTopLeft,
			Transform: m.Translation2x3(-100, -60),
		})
	})

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
	picture := captureHarnessDrawing(t, h, nil, func(canvas.DrawList) {}, &fileMounter{files: makeIconFiles(t)}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			if frame == 0 {
				fonts.RegisterIcon("green", canvas.IconDescriptor{Image: canvas.ImageFromFile(greenIconPath)})
				fonts.RegisterIcon("white", canvas.IconDescriptor{Image: canvas.ImageFromFile(squareIconPath), Tinted: true, Offset: 0.25})
			}
		},
		draw: func(list canvas.DrawList, frame int) {
			list.DrawText(text)
		},
	})

	side := float32(builtInCapHeight * 40)
	baseline := -60 + findBaseline(builtInFontUnits.atSize(40), 1)
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
	picture := captureHarnessDrawing(t, h, nil, func(list canvas.DrawList) {
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
