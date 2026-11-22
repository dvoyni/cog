package canvas_test

import (
	"math"
	"os"
	"sync"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
)

// The fonts of testdata/fonts, subsets of Noto Sans Arabic and Noto Sans
// JP holding the characters these tests lay out.
const (
	arabicFontPath   = "notosansarabic.ttf"
	japaneseFontPath = "notosansjp.ttf"
)

// builtInAdvance is the advance of every glyph of JetBrains Mono, a
// monospaced font, in em.
const builtInAdvance = 0.6

// layOutTexts answers each of texts laid out by FontLibrary's reading side,
// with no frame, under canvas configured by config, the fonts of
// testdata/fonts mounted, and the default set's measures at size 10.
func layOutTexts(t *testing.T, config *canvas.Config, texts ...canvas.Text) ([]canvas.TextLayout, canvas.FontMetrics) {
	t.Helper()
	h := canvastest.NewHarness(t)
	var layouts []canvas.TextLayout
	var metrics canvas.FontMetrics
	h.Spawn(nil, config, &fileMounter{files: os.DirFS("testdata/fonts")}, &fontLibraryUser{read: func(fonts canvas.FontLibrary, frame int) {
		for _, text := range texts {
			layouts = append(layouts, fonts.Layout(text))
		}
		metrics = fonts.Metrics(canvas.FontSet{}, 10, "")
	}})
	h.Step()
	return layouts, metrics
}

// makeDefaultFontConfig answers canvas's configuration of a default set of the
// font at path alone.
func makeDefaultFontConfig(path string) *canvas.Config {
	return &canvas.Config{DefaultFontSet: canvas.FontSetDescriptor{Default: canvas.Font{Path: path}}}
}

// findBaseline answers the first baseline of a line height lineHeight times
// metrics' line height, its leading split above and below.
func findBaseline(metrics canvas.FontMetrics, lineHeight float32) float32 {
	return (metrics.LineHeight*lineHeight-metrics.Ascent-metrics.Descent)/2 + metrics.Ascent
}

// expectNear fails t unless got is want within a thousandth of a world unit.
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
	if !(math.Abs(float64(got.X-want.X)) <= 1e-3 && math.Abs(float64(got.Y-want.Y)) <= 1e-3) {
		t.Errorf("%s: %v, want %v", label, got, want)
	}
}

// expectLineWidths fails t unless layout's lines are as many as want, each
// of its width.
func expectLineWidths(t *testing.T, label string, layout canvas.TextLayout, want ...float32) {
	t.Helper()
	if layout.LineCount() != len(want) {
		t.Errorf("%s: %d lines, want %d", label, layout.LineCount(), len(want))
		return
	}
	for line, width := range want {
		expectNear(t, label+": line width", layout.LineWidth(line), width)
	}
}

func TestLatinRunIsMeasuredByItsFontsAdvancesOnItsFirstBaseline(t *testing.T) {
	layouts, metrics := layOutTexts(t, nil, canvas.Text{String: "hello world", Size: 10})
	layout := layouts[0]
	advance := float32(builtInAdvance * 10)
	expectLineWidths(t, "hello world", layout, 11*advance)
	expectPositionNear(t, "its size", layout.Size(), m.Vec2{X: 11 * advance, Y: metrics.LineHeight})
	expectNear(t, "its baseline", layout.Baseline(), metrics.Ascent)
	expectPositionNear(t, "w's position", layout.ClusterPosition(6), m.Vec2{X: 6 * advance, Y: metrics.Ascent})
	expectPositionNear(t, "the end's position", layout.ClusterPosition(11), m.Vec2{X: 11 * advance, Y: metrics.Ascent})
}

func TestLatinRunWrapsAtSpacesAndAlignsInItsWrapWidth(t *testing.T) {
	advance := float32(builtInAdvance * 10)
	run := canvas.Text{String: "hello world", Size: 10, WrapWidth: 40}
	start := run
	center := run
	center.Align = canvas.TextAlignCenter
	end := run
	end.Align = canvas.TextAlignEnd
	layouts, metrics := layOutTexts(t, nil, start, center, end)
	for index, offset := range []float32{0, (40 - 5*advance) / 2, 40 - 5*advance} {
		layout := layouts[index]
		expectLineWidths(t, "hello world wrapped", layout, 5*advance, 5*advance)
		expectPositionNear(t, "its size", layout.Size(), m.Vec2{X: 40, Y: 2 * metrics.LineHeight})
		expectPositionNear(t, "h's position", layout.ClusterPosition(0), m.Vec2{X: offset, Y: metrics.Ascent})
		expectPositionNear(t, "w's position, starting the second line", layout.ClusterPosition(6), m.Vec2{X: offset, Y: metrics.Ascent + metrics.LineHeight})
	}
}

func TestWordTooLongForItsLineBreaksBetweenGraphemeClusters(t *testing.T) {
	advance := float32(builtInAdvance * 10)
	layouts, _ := layOutTexts(t, nil, canvas.Text{String: "abcdefgh", Size: 10, WrapWidth: 20})
	expectLineWidths(t, "abcdefgh wrapped at 3 letters", layouts[0], 3*advance, 3*advance, 2*advance)
}

func TestNewlineIsAHardBreak(t *testing.T) {
	advance := float32(builtInAdvance * 10)
	layouts, metrics := layOutTexts(t, nil, canvas.Text{String: "ab\ncde\n", Size: 10})
	layout := layouts[0]
	expectLineWidths(t, "ab, cde and an empty line", layout, 2*advance, 3*advance, 0)
	expectPositionNear(t, "its size", layout.Size(), m.Vec2{X: 3 * advance, Y: 3 * metrics.LineHeight})
	expectPositionNear(t, "the first newline's position", layout.ClusterPosition(2), m.Vec2{X: 2 * advance, Y: metrics.Ascent})
	expectPositionNear(t, "c's position", layout.ClusterPosition(3), m.Vec2{X: 0, Y: metrics.Ascent + metrics.LineHeight})
	expectPositionNear(t, "the end's position", layout.ClusterPosition(7), m.Vec2{X: 0, Y: metrics.Ascent + 2*metrics.LineHeight})
}

func TestCarriageReturnAndNewlineIsOneHardBreakAsALineSeparatorIs(t *testing.T) {
	advance := float32(builtInAdvance * 10)
	layouts, metrics := layOutTexts(t, nil, canvas.Text{String: "ab\r\nc\u2028de", Size: 10})
	layout := layouts[0]
	expectLineWidths(t, "ab, c and de", layout, 2*advance, advance, 2*advance)
	expectPositionNear(t, "the carriage return and newline's position", layout.ClusterPosition(2), m.Vec2{X: 2 * advance, Y: metrics.Ascent})
	expectPositionNear(t, "c's position, one cluster after it", layout.ClusterPosition(3), m.Vec2{X: 0, Y: metrics.Ascent + metrics.LineHeight})
	expectPositionNear(t, "the line separator's position", layout.ClusterPosition(4), m.Vec2{X: advance, Y: metrics.Ascent + metrics.LineHeight})
	expectPositionNear(t, "the end's position", layout.ClusterPosition(7), m.Vec2{X: 2 * advance, Y: metrics.Ascent + 2*metrics.LineHeight})
}

// TestLayoutsMadeAtOnceAreEachAsOneMadeAlone lays runs out at once, as
// tasks holding FontLibrary's read lock may, of two weights of JetBrains
// Mono, which share one parsed font, and of a font of its own; run under
// -race, it breaks a face or shaper shared between layouts.
func TestLayoutsMadeAtOnceAreEachAsOneMadeAlone(t *testing.T) {
	h := canvastest.NewHarness(t)
	sets := make([]canvas.FontSet, 3)
	var mismatchCount int
	h.Spawn(nil, nil, &fileMounter{files: os.DirFS("testdata/fonts")}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			sets[1] = fonts.MakeFontSet(canvas.FontSetDescriptor{Default: canvas.Font{Variations: [4]canvas.Variation{{Axis: canvas.AxisTagFromName("wght"), Value: 800}}}})
			sets[2] = fonts.MakeFontSet(canvas.FontSetDescriptor{Default: canvas.Font{Path: japaneseFontPath}})
		},
		read: func(fonts canvas.FontLibrary, frame int) {
			text := canvas.Text{String: "日本 ffi words, wrapped", Size: 10, WrapWidth: 60}
			alone := make([]m.Vec2, len(sets))
			for index, set := range sets {
				text.Font = set
				alone[index] = fonts.Layout(text).Size()
			}
			var group sync.WaitGroup
			var lock sync.Mutex
			for worker := range 8 {
				group.Go(func() {
					text := text
					text.Font = sets[worker%len(sets)]
					for range 50 {
						if fonts.Layout(text).Size() != alone[worker%len(sets)] {
							lock.Lock()
							mismatchCount++
							lock.Unlock()
						}
					}
				})
			}
			group.Wait()
		},
	})
	h.Step()
	if mismatchCount != 0 {
		t.Errorf("%d runs laid out at once measured otherwise than alone, want none", mismatchCount)
	}
}

func TestLineHeightAndTrackingSpaceLinesAndLetters(t *testing.T) {
	advance := float32(builtInAdvance * 10)
	layouts, metrics := layOutTexts(t, nil, canvas.Text{String: "abc\nd", Size: 10, LineHeight: 2, Tracking: 0.1})
	layout := layouts[0]
	expectLineWidths(t, "abc tracked", layout, 3*advance+2, advance)
	expectPositionNear(t, "its size", layout.Size(), m.Vec2{X: 3*advance + 2, Y: 4 * metrics.LineHeight})
	expectNear(t, "its baseline", layout.Baseline(), findBaseline(metrics, 2))
	expectPositionNear(t, "b's position, half a tracking past a", layout.ClusterPosition(1), m.Vec2{X: advance + 0.5, Y: findBaseline(metrics, 2)})
	expectPositionNear(t, "d's position", layout.ClusterPosition(4), m.Vec2{X: 0, Y: findBaseline(metrics, 2) + 2*metrics.LineHeight})
}

func TestZeroSizeIsConfigsTextSizeOf16ByDefault(t *testing.T) {
	layouts, _ := layOutTexts(t, nil, canvas.Text{String: "a"})
	expectLineWidths(t, "a at the default text size", layouts[0], builtInAdvance*16)
	layouts, _ = layOutTexts(t, &canvas.Config{TextSize: 20}, canvas.Text{String: "a"})
	expectLineWidths(t, "a at a text size of 20", layouts[0], builtInAdvance*20)
}

func TestArabicRunIsShapedJoinedAndLaidOutFromTheRight(t *testing.T) {
	const word = "كلمة"
	words := word + " " + word + " " + word
	layouts, metrics := layOutTexts(t, makeDefaultFontConfig(arabicFontPath),
		canvas.Text{String: word, Size: 10},
		canvas.Text{String: "ب", Size: 10},
		canvas.Text{String: "ببب", Size: 10},
		canvas.Text{String: words, Size: 10},
	)
	wordWidth := layouts[0].LineWidth(0)
	if isolated, joined := layouts[1].LineWidth(0), layouts[2].LineWidth(0); !(joined < 3*isolated) {
		t.Errorf("three behs joined are %v wide, want narrower than three isolated, %v", joined, 3*isolated)
	}
	layout := layouts[3]
	if layout.LineCount() != 1 {
		t.Fatalf("three words unwrapped are %d lines, want 1", layout.LineCount())
	}
	width := layout.LineWidth(0)
	expectPositionNear(t, "the first word's first letter's position, at the right", layout.ClusterPosition(0), m.Vec2{X: width, Y: metrics.Ascent})
	expectPositionNear(t, "the first word's end, a word's width left of the right", layout.ClusterPosition(4), m.Vec2{X: width - wordWidth, Y: metrics.Ascent})
	for cluster := range 3 {
		if !(layout.ClusterPosition(cluster+1).X < layout.ClusterPosition(cluster).X) {
			t.Errorf("letter %d at %v, want left of letter %d at %v", cluster+1, layout.ClusterPosition(cluster+1), cluster, layout.ClusterPosition(cluster))
		}
	}
}

func TestArabicRunWrapsAtSpacesAndAlignsStartAtTheRight(t *testing.T) {
	const word = "كلمة"
	words := word + " " + word + " " + word
	layouts, metrics := layOutTexts(t, makeDefaultFontConfig(arabicFontPath), canvas.Text{String: word, Size: 10})
	wordWidth := layouts[0].LineWidth(0)
	wrapWidth := wordWidth * 1.5
	run := canvas.Text{String: words, Size: 10, WrapWidth: wrapWidth}
	start := run
	end := run
	end.Align = canvas.TextAlignEnd
	layouts, _ = layOutTexts(t, makeDefaultFontConfig(arabicFontPath), start, end)
	for index, right := range []float32{wrapWidth, wordWidth} {
		layout := layouts[index]
		expectLineWidths(t, "three words wrapped", layout, wordWidth, wordWidth, wordWidth)
		for line, cluster := range []int{0, 5, 10} {
			expectPositionNear(t, "a line's first letter's position", layout.ClusterPosition(cluster), m.Vec2{X: right, Y: metrics.Ascent + float32(line)*metrics.LineHeight})
		}
	}
}

func TestDigitsInAnArabicRunReadLeftToRight(t *testing.T) {
	layouts, metrics := layOutTexts(t, makeDefaultFontConfig(arabicFontPath),
		canvas.Text{String: "ب 123", Size: 10},
		canvas.Text{String: "123 ب", Size: 10},
		canvas.Text{String: "1", Size: 10},
	)
	digitWidth := layouts[2].LineWidth(0)
	behFirst := layouts[0]
	expectPositionNear(t, "beh's position, at the right", behFirst.ClusterPosition(0), m.Vec2{X: behFirst.LineWidth(0), Y: metrics.Ascent})
	for digit := range 3 {
		expectPositionNear(t, "a digit's position after beh, from the left", behFirst.ClusterPosition(2+digit), m.Vec2{X: float32(digit) * digitWidth, Y: metrics.Ascent})
	}
	// The digits come first, but beh is the first strong character, so the
	// paragraph still runs right to left, the digits at its right.
	digitsFirst := layouts[1]
	for digit := range 3 {
		expectPositionNear(t, "a digit's position before beh, from the right", digitsFirst.ClusterPosition(digit), m.Vec2{X: digitsFirst.LineWidth(0) - float32(3-digit)*digitWidth, Y: metrics.Ascent})
	}
}

func TestLigatureSharesItsWidthEvenlyByItsGraphemeClusters(t *testing.T) {
	// Noto Sans JP draws "ffi" as one glyph, a ligature.
	layouts, metrics := layOutTexts(t, makeDefaultFontConfig(japaneseFontPath), canvas.Text{String: "ffi", Size: 10})
	layout := layouts[0]
	width := layout.LineWidth(0)
	for cluster := range 4 {
		expectPositionNear(t, "a letter's position, a third of the ligature past the one before", layout.ClusterPosition(cluster), m.Vec2{X: width * float32(cluster) / 3, Y: metrics.Ascent})
	}
}

func TestJapaneseRunBreaksBetweenIdeographsAndAligns(t *testing.T) {
	layouts, metrics := layOutTexts(t, makeDefaultFontConfig(japaneseFontPath),
		canvas.Text{String: "日本語日本語", Size: 10, WrapWidth: 40},
		canvas.Text{String: "日本語日本語", Size: 10, WrapWidth: 40, Align: canvas.TextAlignCenter},
	)
	expectLineWidths(t, "six ideographs wrapped at 4", layouts[0], 40, 20)
	expectPositionNear(t, "the fifth ideograph's position, starting the second line", layouts[0].ClusterPosition(4), m.Vec2{X: 0, Y: metrics.Ascent + metrics.LineHeight})
	expectPositionNear(t, "the fifth ideograph's position, centred", layouts[1].ClusterPosition(4), m.Vec2{X: 10, Y: metrics.Ascent + metrics.LineHeight})
}

func TestJapaneseLineNeverStartsWithClosingPunctuation(t *testing.T) {
	// Six ideographs fit the line, but the full stop after the sixth may not
	// start the next, so the sixth goes down with it.
	layouts, metrics := layOutTexts(t, makeDefaultFontConfig(japaneseFontPath), canvas.Text{String: "日本語の文章。次の文", Size: 10, WrapWidth: 60})
	layout := layouts[0]
	expectLineWidths(t, "the sentence wrapped", layout, 50, 50)
	expectPositionNear(t, "章's position, starting the second line", layout.ClusterPosition(5), m.Vec2{X: 0, Y: metrics.Ascent + metrics.LineHeight})
	expectPositionNear(t, "the full stop's position, after 章", layout.ClusterPosition(6), m.Vec2{X: 10, Y: metrics.Ascent + metrics.LineHeight})
}

func TestDestroyedFontSetIsLaidOutAsTheDefaultSetReportedOnce(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindFontSetNotHeld)
	var set canvas.FontSet
	var widths []float32
	h.Spawn(nil, nil, &fileMounter{files: os.DirFS("testdata/fonts")}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			switch frame {
			case 0:
				set = fonts.MakeFontSet(canvas.FontSetDescriptor{Default: canvas.Font{Path: japaneseFontPath}})
			case 1:
				fonts.DestroyFontSet(set)
			}
		},
		read: func(fonts canvas.FontLibrary, frame int) {
			widths = append(widths, fonts.Layout(canvas.Text{String: "日", Font: set, Size: 10}).LineWidth(0))
			if frame == 1 {
				widths = append(widths, fonts.Layout(canvas.Text{String: "日", Font: set, Size: 10}).LineWidth(0))
			}
		},
	})
	h.Step()
	h.Step()
	if len(widths) != 3 {
		t.Fatalf("%d layouts, want 3", len(widths))
	}
	expectNear(t, "an ideograph of the set made", widths[0], 10)
	if widths[1] == widths[0] || widths[2] != widths[1] {
		t.Errorf("an ideograph of the set destroyed is %v wide, then %v, want the default set's width both times, not the set's %v", widths[1], widths[2], widths[0])
	}
	if count := countReports(h, canvas.ReportKindFontSetNotHeld); count != 1 {
		t.Errorf("%d font sets not held reported, want the destroyed set once", count)
	}
}
