package canvas

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"math"
	"os"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas/font"
	"golang.org/x/image/font/gofont/goregular"
)

// builtInAdvance is the advance of every glyph of JetBrains Mono, a
// monospaced font, in em.
const builtInAdvance = 0.6

func TestARunOfNoSizeIsLaidOutAtConfigsTextSizeOf16ByDefault(t *testing.T) {
	for textSize, want := range map[float32]float32{0: 16, 20: 20} {
		library, _, _ := makeLibraryOverFiles(t, &Config{TextSize: textSize}, fstest.MapFS{})
		expectLineWidths(t, "a at the configured text size", library.Layout(Text{String: "a"}), builtInAdvance*want)
	}
}

func TestADestroyedSetIsLaidOutAsTheDefaultSetReportedOnce(t *testing.T) {
	// The default set's JetBrains Mono has no ideographs, so it lays the
	// destroyed set's out as its missing-glyph box.
	library, writer, logged := makeLibraryOverFiles(t, &Config{}, os.DirFS("testdata/fonts"))
	set := writer.MakeFontSet(font.SetDescriptor{Default: font.Font{Path: "notosansjp.ttf"}})
	run := Text{String: "日", Font: set, Size: 10}
	made := library.Layout(run).LineWidth(0)
	writer.DestroyFontSet(set)
	destroyed := library.Layout(run).LineWidth(0)
	again := library.Layout(run).LineWidth(0)

	expectNear(t, "an ideograph of the set made", made, 10)
	expectNear(t, "an ideograph of the set destroyed", destroyed, builtInAdvance*10)
	expectNear(t, "an ideograph of the set destroyed, laid out again", again, builtInAdvance*10)
	if count := logged.countKind(ReportKindFontSetNotHeld); count != 1 {
		t.Errorf("%d font sets not held reported, want the destroyed set once", count)
	}
}

func TestACharacterIsLaidOutInTheFirstOfItsLanguagesFontsThatHasItThenInTheDefaultFont(t *testing.T) {
	// zh-Hans is served by Noto Sans Arabic, which has W but no ideographs,
	// then by Noto Sans SC, which has 今 but no arrows; JetBrains Mono, the
	// default font, has the arrow. The run's own language picks them,
	// though the current one has no fonts, and its first font measures the
	// line.
	library, writer, _ := makeLibraryOverFiles(t, &Config{}, os.DirFS("testdata/fonts"))
	set := writer.MakeFontSet(font.SetDescriptor{Languages: []font.LanguageFonts{
		{Languages: []string{"zh-Hans"}, Fonts: []font.Font{{Path: "notosansarabic.ttf"}, {Path: "notosanssc.ttf"}}},
	}})
	arabic := writer.MakeFontSet(font.SetDescriptor{Default: font.Font{Path: "notosansarabic.ttf"}})
	chinese := writer.MakeFontSet(font.SetDescriptor{Default: font.Font{Path: "notosanssc.ttf"}})
	arabicW := library.Layout(Text{String: "W", Font: arabic, Size: 10}).LineWidth(0)
	ideograph := library.Layout(Text{String: "今", Font: chinese, Size: 10}).LineWidth(0)
	if arabicW == builtInAdvance*10 {
		t.Fatal("Noto Sans Arabic's W is as wide as JetBrains Mono's, so the widths tell no font from the other")
	}

	layout := library.Layout(Text{String: "W今→", Font: set, Size: 10, Language: "zh-Hans"})
	expectLineWidths(t, "W, 今 and an arrow, each in its own font", layout, arabicW+ideograph+builtInAdvance*10)
	expectNear(t, "the line's height, Noto Sans Arabic's", layout.Size().Y, library.Metrics(set, 10, "zh-Hans").LineHeight)
}

func TestARunLaidOutAgainInAnotherLanguageTakesThatLanguagesFont(t *testing.T) {
	// The set's default font is Go Regular, and Japanese's JetBrains Mono:
	// an m laid out in English, then again once the language is Japanese,
	// is as wide as JetBrains Mono's advance.
	library, writer, _ := makeLibraryOverFiles(t, &Config{Language: "en"}, fstest.MapFS{"go.ttf": {Data: goregular.TTF}})
	set := writer.MakeFontSet(font.SetDescriptor{
		Default:   font.Font{Path: "go.ttf"},
		Languages: []font.LanguageFonts{{Languages: []string{"ja"}, Fonts: []font.Font{{}}}},
	})
	run := Text{String: "m", Font: set, Size: 10}
	english := library.Layout(run).LineWidth(0)
	writer.SetLanguage("ja")
	japanese := library.Layout(run).LineWidth(0)

	if english == japanese {
		t.Errorf("the m is %v wide in English and Japanese both, want Japanese's font's width", english)
	}
	expectNear(t, "the m in Japanese", japanese, builtInAdvance*10)
}

func TestSpansAreShapedAndWrappedAsTheirTextsEndToEndAre(t *testing.T) {
	// A sentence cut into spans, one of them tinted, wraps at 60 units as
	// the sentence written whole does: each line as wide, each cluster in
	// its place.
	library, _, _ := makeLibraryOverFiles(t, &Config{}, fstest.MapFS{})
	sentence := Text{String: "Press the red button now", Size: 10, WrapWidth: 60}
	spanned := sentence
	spanned.String = ""
	spanned.Spans = []Span{{Text: "Press the "}, {Text: "red", Tint: m.Color{R: 1, G: 0.5, A: 1}}, {Text: " button now"}}
	whole := library.Layout(sentence)
	spans := library.Layout(spanned)

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
	library, writer, _ := makeLibraryOverFiles(t, &Config{}, makeIconPictures(t))
	writer.RegisterIcon("wide", IconDescriptor{Image: ImageFromFile("wide.png")})
	writer.RegisterIcon("tall", IconDescriptor{Image: ImageFromFile("tall.png"), Scale: 2})
	writer.RegisterIcon("column", IconDescriptor{Image: ImageFromFile("wide.png", ImageOptions{Sheet: Grid{Columns: 2}}), Frame: 1})
	metrics := library.Metrics(FontSet{}, 10, "")

	between := library.Layout(Text{Spans: []Span{{Text: "a"}, {Icon: "wide"}, {Text: "b"}}, Size: 10})
	expectLineWidths(t, "a, the wide icon and b", between, 2*builtInAdvance*10+2*metrics.CapHeight)
	baseline := (metrics.LineHeight-metrics.Ascent-metrics.Descent)/2 + metrics.Ascent
	expectPositionNear(t, "b, after the wide icon", between.ClusterPosition(2), m.Vec2{X: builtInAdvance*10 + 2*metrics.CapHeight, Y: baseline})
	expectLineWidths(t, "the tall icon at twice the cap height", library.Layout(Text{Spans: []Span{{Icon: "tall"}}, Size: 10}), metrics.CapHeight)
	expectLineWidths(t, "a square frame of the wide picture", library.Layout(Text{Spans: []Span{{Icon: "column"}}, Size: 10}), metrics.CapHeight)
}

func TestALineBreaksAroundAnIconAsAroundALetterNeverBetweenItAndTheLetterAfterIt(t *testing.T) {
	// "ab ", a square icon and an s, wrapped at 28 units at an em of 10:
	// "ab " and the icon fit, 25.3 units, and the s does not, so the line
	// breaks at the space, before the icon, never between the icon and the
	// s, as Unicode's line breaking would around the object replacement
	// character.
	library, writer, _ := makeLibraryOverFiles(t, &Config{}, makeIconPictures(t))
	writer.RegisterIcon("square", IconDescriptor{Image: ImageFromFile("square.png")})
	capHeight := library.Metrics(FontSet{}, 10, "").CapHeight

	layout := library.Layout(Text{Spans: []Span{{Text: "ab "}, {Icon: "square"}, {Text: "s"}}, Size: 10, WrapWidth: 28})
	expectLineWidths(t, "ab, then the icon and its s", layout, 2*builtInAdvance*10, capHeight+builtInAdvance*10)
}

func TestAnIconOfANameNotRegisteredIsTheDefaultFontsMissingGlyphReportedOnce(t *testing.T) {
	// JetBrains Mono's missing glyph advances 0.6 em, as its letters do,
	// between a and b, in two runs, and the name is reported once.
	library, _, logged := makeLibraryOverFiles(t, &Config{}, fstest.MapFS{})
	between := library.Layout(Text{Spans: []Span{{Text: "a"}, {Icon: "nothing"}, {Text: "b"}}, Size: 10})
	alone := library.Layout(Text{Spans: []Span{{Icon: "nothing"}}, Size: 20})

	expectLineWidths(t, "a, the missing glyph and b", between, 3*builtInAdvance*10)
	expectLineWidths(t, "the missing glyph alone", alone, builtInAdvance*20)
	if count := logged.countKind(ReportKindIconNotRegistered); count != 1 {
		t.Errorf("%d icons not registered reported, want the name once", count)
	}
}

func TestMarkupIsLaidOutAsTheSpansItReadsAsAndAsWrittenWhenItDoesNotParse(t *testing.T) {
	// ab \c:#f00{cd} wraps at 20 units as its spans do; a\xb, its
	// backslash before an x, is laid out as its four characters, in two
	// runs, reported once.
	library, _, logged := makeLibraryOverFiles(t, &Config{}, fstest.MapFS{})
	markup := library.Layout(Text{String: `ab \c:#f00{cd}`, Markup: true, Size: 10, WrapWidth: 20})
	spans := library.Layout(Text{Spans: []Span{{Text: "ab "}, {Text: "cd", Tint: m.Color{R: 1, A: 1}}}, Size: 10, WrapWidth: 20})
	expectLineWidths(t, "the markup wrapped", markup, 2*builtInAdvance*10, 2*builtInAdvance*10)
	for cluster := range 6 {
		expectPositionNear(t, "a cluster of the markup", markup.ClusterPosition(cluster), spans.ClusterPosition(cluster))
	}

	malformed := library.Layout(Text{String: `a\xb`, Markup: true, Size: 10})
	library.Layout(Text{String: `a\xb`, Markup: true, Size: 20})
	expectLineWidths(t, "the markup that does not parse, as written", malformed, 4*builtInAdvance*10)
	if count := logged.countKind(ReportKindMarkupMalformed); count != 1 {
		t.Errorf("%d markups that do not parse reported, want the string once", count)
	}
}

func TestRunsAreLaidOutApartByAllTheirLayoutTakesAndTogetherOtherwise(t *testing.T) {
	// Each run differs from the first in one thing its layout takes; a span
	// of ab, the same span tinted, and markup reading as it share one
	// layout, since a tint is drawn, not laid out; each run laid out twice
	// is laid out once.
	library, writer, _ := makeLibraryOverFiles(t, &Config{}, fstest.MapFS{})
	other := writer.MakeFontSet(font.SetDescriptor{})
	runs := []Text{
		{String: "ab", Size: 10},
		{String: "ab", Size: 20},
		{String: "ab", Size: 10, WrapWidth: 50},
		{String: "ab", Size: 10, Align: TextAlignCenter},
		{String: "ab", Size: 10, LineHeight: 2},
		{String: "ab", Size: 10, Tracking: 0.1},
		{String: "ab", Size: 10, Language: "ja"},
		{String: "ab", Size: 10, Font: other},
		{Spans: []Span{{Text: "a"}, {Text: "b"}}, Size: 10},
		{Spans: []Span{{Text: "ab"}}, Size: 10},
		{Spans: []Span{{Text: "ab", Tint: m.Color{R: 1, A: 1}}}, Size: 10},
		{String: "ab", Markup: true, Size: 10},
	}
	for range 2 {
		for _, run := range runs {
			library.Layout(run)
		}
	}
	if count := len(library.layouts.layouts); count != 10 {
		t.Errorf("the runs are laid out as %d layouts, want 10: one of each but the span of ab, tinted or in markup", count)
	}
}

// makeLibraryOverFiles answers a font library of config, its default set
// JetBrains Mono, as it is until canvas starts, its writing side over
// files, through which a test makes its sets, and the errors its reports
// logged.
func makeLibraryOverFiles(t *testing.T, config *Config, files fs.FS) (*fontLibrary, fontLibraryWriter, *errorRecorder) {
	t.Helper()
	canvasReporter, logged := newRecordingReporter(t)
	library := newFontLibrary(canvasReporter, config)
	return library, makeFontLibraryWriter(library, files), logged
}

// makeIconPictures answers files holding the pictures icons are registered
// of: a white square of 16 texels, square.png, one twice as wide, wide.png,
// and one twice as tall, tall.png.
func makeIconPictures(t *testing.T) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	for path, size := range map[string]image.Point{"square.png": {X: 16, Y: 16}, "wide.png": {X: 32, Y: 16}, "tall.png": {X: 16, Y: 32}} {
		picture := image.NewNRGBA(image.Rectangle{Max: size})
		for y := range size.Y {
			for x := range size.X {
				picture.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, picture); err != nil {
			t.Fatal(err)
		}
		files[path] = &fstest.MapFile{Data: encoded.Bytes()}
	}
	return files
}

// expectNear fails t unless got is want within a thousandth of a
// world unit.
func expectNear(t *testing.T, label string, got, want float32) {
	t.Helper()
	if !(math.Abs(float64(got-want)) <= 1e-3) {
		t.Errorf("%s: %v, want %v", label, got, want)
	}
}

// expectPositionNear fails t unless got is want within a thousandth of a
// world unit on each axis.
func expectPositionNear(t *testing.T, label string, got, want m.Vec2) {
	t.Helper()
	if !nearVec2(got, want) {
		t.Errorf("%s: %v, want %v", label, got, want)
	}
}

// expectLineWidths fails t unless layout's lines are as many as want,
// each of its width.
func expectLineWidths(t *testing.T, label string, layout TextLayout, want ...float32) {
	t.Helper()
	if layout.LineCount() != len(want) {
		t.Errorf("%s: %d lines, want %d", label, layout.LineCount(), len(want))
		return
	}
	for line, width := range want {
		expectNear(t, label+": line width", layout.LineWidth(line), width)
	}
}
