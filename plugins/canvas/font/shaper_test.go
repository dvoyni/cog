package font_test

import (
	"math"
	"os"
	"slices"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas/font"
)

// builtInAdvance is the advance of every glyph of JetBrains Mono, a
// monospaced font, at an em of 10.
const builtInAdvance = 6

func TestARightToLeftParagraphIsLaidOutFromTheRightItsDigitsLeftToRight(t *testing.T) {
	set := loadDefaultFont(t, "notosansarabic.ttf")
	var shaper font.Shaper
	digitWidth := shaper.LayOut(font.Input{Set: set, Runes: []rune("1"), Em: 10}).LineWidths[0]
	layout := shaper.LayOut(font.Input{Set: set, Runes: []rune("ب 123"), Em: 10})
	ascent := set.Default().Metrics(10).Ascent
	expectPositionNear(t, "beh's position, at the right", layout.ClusterPositions[0], m.Vec2{X: layout.LineWidths[0], Y: ascent})
	for digit := range 3 {
		expectPositionNear(t, "a digit's position, from the left", layout.ClusterPositions[2+digit], m.Vec2{X: float32(digit) * digitWidth, Y: ascent})
	}
	expectPositionNear(t, "the end's position, at the left", layout.ClusterPositions[5], m.Vec2{Y: ascent})
}

func TestLinesWrapAtSpacesAndAlignInTheWrapWidth(t *testing.T) {
	set := loadDefaultFont(t, "")
	metrics := set.Default().Metrics(10)
	var shaper font.Shaper
	for align, offset := range map[font.Align]float32{font.AlignStart: 0, font.AlignCenter: 5, font.AlignEnd: 10} {
		layout := shaper.LayOut(font.Input{Set: set, Runes: []rune("hello world"), Em: 10, WrapWidth: 40, Align: align})
		expectWidths(t, "hello world wrapped", layout.LineWidths, 5*builtInAdvance, 5*builtInAdvance)
		expectPositionNear(t, "its size", layout.Size, m.Vec2{X: 40, Y: 2 * metrics.LineHeight})
		expectPositionNear(t, "h's position", layout.ClusterPositions[0], m.Vec2{X: offset, Y: metrics.Ascent})
		expectPositionNear(t, "w's position, starting the second line", layout.ClusterPositions[6], m.Vec2{X: offset, Y: metrics.Ascent + metrics.LineHeight})
		if pen := layout.Glyphs[7].Pen; !isNearVec2(pen, layout.ClusterPositions[7]) {
			t.Errorf("o's pen is %v, want its cluster's position, %v", pen, layout.ClusterPositions[7])
		}
	}
	layout := shaper.LayOut(font.Input{Set: set, Runes: []rune("abcdefgh"), Em: 10, WrapWidth: 20})
	expectWidths(t, "a word too long for its line, broken between its clusters", layout.LineWidths, 3*builtInAdvance, 3*builtInAdvance, 2*builtInAdvance)
	layout = shaper.LayOut(font.Input{Set: set, Runes: []rune("ab\nc\n"), Em: 10, LineHeight: 2})
	expectWidths(t, "ab, c and an empty line", layout.LineWidths, 2*builtInAdvance, builtInAdvance, 0)
	expectNear(t, "the height of three lines of twice the font's", layout.Size.Y, 6*metrics.LineHeight)
	expectNear(t, "the baseline, the leading split above and below", layout.Baseline, (2*metrics.LineHeight-metrics.Ascent-metrics.Descent)/2+metrics.Ascent)
}

func TestALigatureSharesItsWidthByItsClustersAndAClusterOfGlyphsLeadsBackToItsFirstRune(t *testing.T) {
	var shaper font.Shaper
	// Noto Sans JP draws "ffi" as one glyph.
	layout := shaper.LayOut(font.Input{Set: loadDefaultFont(t, "notosansjp.ttf"), Runes: []rune("ffi"), Em: 10})
	if len(layout.Glyphs) != 1 || layout.Glyphs[0].Rune != 0 {
		t.Errorf("ffi is drawn by %+v, want one glyph of rune 0", layout.Glyphs)
	}
	for cluster := range 4 {
		expectNear(t, "a letter's position, a third of the ligature past the one before", layout.ClusterPositions[cluster].X, layout.LineWidths[0]*float32(cluster)/3)
	}
	// A combining acute accent after an x is one cluster of two glyphs,
	// which JetBrains Mono shapes with no offset: the accent's pen lies past
	// the x's advance, where the cluster ends.
	layout = shaper.LayOut(font.Input{Set: loadDefaultFont(t, ""), Runes: []rune("x́"), Em: 10})
	if len(layout.Glyphs) != 2 || layout.Glyphs[0].Rune != 0 || layout.Glyphs[1].Rune != 0 {
		t.Fatalf("x and its accent are drawn by %+v, want two glyphs of rune 0", layout.Glyphs)
	}
	if accent := layout.Glyphs[1].Pen.X; accent != layout.ClusterPositions[1].X || accent <= layout.Glyphs[0].Pen.X {
		t.Errorf("the accent's pen is at %v, want past the x's advance, at %v", accent, layout.ClusterPositions[1].X)
	}
}

func TestTrackingSpacesLettersHalfOnEachSideAndNoneAtTheLinesEnds(t *testing.T) {
	var shaper font.Shaper
	layout := shaper.LayOut(font.Input{Set: loadDefaultFont(t, ""), Runes: []rune("abc"), Em: 10, Tracking: 0.1})
	expectWidths(t, "abc tracked", layout.LineWidths, 3*builtInAdvance+2)
	expectNear(t, "a's position", layout.ClusterPositions[0].X, 0)
	expectNear(t, "b's position, half a tracking past a", layout.ClusterPositions[1].X, builtInAdvance+0.5)
}

func TestAnObjectIsABoxOfItsWidthRaisedThatALineBreaksAroundAsAroundALetter(t *testing.T) {
	set := loadDefaultFont(t, "")
	ascent := set.Default().Metrics(10).Ascent
	var shaper font.Shaper
	layout := shaper.LayOut(font.Input{Set: set, Runes: []rune("ab?c"), Objects: []font.Object{{Rune: 2, Width: 15, Raise: 2}}, Em: 10})
	expectWidths(t, "two letters, the object and a letter", layout.LineWidths, 3*builtInAdvance+15)
	if object := layout.Glyphs[2]; object.Font != nil || object.Rune != 2 || !isNearVec2(object.Pen, m.Vec2{X: 2 * builtInAdvance, Y: ascent - 2}) {
		t.Errorf("the object is laid out as %+v, want a glyph of no font of rune 2 at (%v, %v)", object, 2*builtInAdvance, ascent-2)
	}
	expectNear(t, "c's position, past the object", layout.ClusterPositions[3].X, 2*builtInAdvance+15)
	// "a ?c" fits the line but for c, and its object goes down with it.
	layout = shaper.LayOut(font.Input{Set: set, Runes: []rune("a ?c"), Objects: []font.Object{{Rune: 2, Width: builtInAdvance}}, Em: 10, WrapWidth: 20})
	expectWidths(t, "a, then the object and c", layout.LineWidths, builtInAdvance, 2*builtInAdvance)
	// An object of no width is the default font's missing glyph.
	layout = shaper.LayOut(font.Input{Set: set, Runes: []rune("?"), Objects: []font.Object{{Rune: 0}}, Em: 10})
	if missing := layout.Glyphs[0]; missing.Font != set.Default() || missing.ID != 0 || layout.LineWidths[0] <= 0 {
		t.Errorf("an object of no width is laid out as %+v, %v wide, want the default font's missing glyph", missing, layout.LineWidths[0])
	}
}

func TestEachCharacterIsShapedInTheFontDrawingIt(t *testing.T) {
	set, _ := font.Load(makeFontFiles(t), font.SetDescriptor{Languages: []font.LanguageFonts{
		{Languages: []string{"zh-Hans"}, Fonts: []font.Font{{Path: "go.ttf"}, {Path: "hanzi.ttf"}}},
	}})
	var shaper font.Shaper
	layout := shaper.LayOut(font.Input{Set: set, Runes: []rune("a今ᄀ"), Language: "zh-Hans", Em: 10})
	var paths []string
	for _, glyph := range layout.Glyphs {
		paths = append(paths, glyph.Font.Font().Path)
	}
	if !slices.Equal(paths, []string{"go.ttf", "hanzi.ttf", ""}) || layout.Glyphs[2].ID != 0 {
		t.Errorf("a, 今 and ᄀ are drawn by %q, the last glyph %d, want go.ttf, hanzi.ttf and the default font's missing glyph", paths, layout.Glyphs[2].ID)
	}
	metrics := set.LanguageFont("zh-Hans").Metrics(10)
	expectNear(t, "the line's height, the font measuring zh-Hans's", layout.Size.Y, metrics.LineHeight)
}

func TestAGlyphIsRasterizedOverTheTexelsItsOutlineSpans(t *testing.T) {
	// JetBrains Mono's full block spans -10 to 610 of its 1000 units to the
	// em across, -300 to 1020 up: at 100 texels to the em, -1 to 61 and -102
	// to 30 down, each on a whole texel, all covered.
	set := loadDefaultFont(t, "")
	var shaper font.Shaper
	block := shaper.LayOut(font.Input{Set: set, Runes: []rune("█ "), Em: 1}).Glyphs
	coverage := shaper.Rasterize(set.Default(), block[0].ID, 100, 2)
	wantBounds := m.Rect{X: -0.03, Y: -1.04, Width: 0.66, Height: 1.36}
	if coverage.Width != 66 || coverage.Height != 136 || !isNearRect(coverage.Bounds, wantBounds) {
		t.Fatalf("the block covers %d by %d texels over %+v em, want 66 by 136 over %+v", coverage.Width, coverage.Height, coverage.Bounds, wantBounds)
	}
	for y := range coverage.Height {
		for x := range coverage.Width {
			want := byte(0)
			if x >= 2 && x < 64 && y >= 2 && y < 134 {
				want = 255
			}
			if got := coverage.Texels[y*coverage.Width+x]; got != want {
				t.Fatalf("texel (%d, %d) is covered %d, want %d", x, y, got, want)
			}
		}
	}
	if space := shaper.Rasterize(set.Default(), block[1].ID, 100, 2); space.Texels != nil {
		t.Errorf("a space covers %d texels, want none", len(space.Texels))
	}
}

func TestARunLaidOutAfterClearFacesIsAsBefore(t *testing.T) {
	set := loadDefaultFont(t, "")
	var shaper font.Shaper
	input := font.Input{Set: set, Runes: []rune("hello world"), Em: 10, WrapWidth: 40}
	before := shaper.LayOut(input)
	shaper.ClearFaces()
	after := shaper.LayOut(input)
	if !slices.Equal(before.LineWidths, after.LineWidths) || !slices.Equal(before.ClusterPositions, after.ClusterPositions) || !slices.Equal(before.Glyphs, after.Glyphs) {
		t.Errorf("the run laid out after ClearFaces is %+v, want it as before, %+v", after, before)
	}
}

// loadDefaultFont answers a set of the font of testdata/fonts at path
// alone, JetBrains Mono for an empty path.
func loadDefaultFont(t *testing.T, path string) *font.Set {
	t.Helper()
	set, failures := font.Load(os.DirFS("../testdata/fonts"), font.SetDescriptor{Default: font.Font{Path: path}})
	if len(failures) != 0 {
		t.Fatal(failures)
	}
	return set
}

// expectNear fails t unless got is want within a thousandth.
func expectNear(t *testing.T, label string, got, want float32) {
	t.Helper()
	if !isNear(got, want) {
		t.Errorf("%s: %v, want %v", label, got, want)
	}
}

// expectPositionNear fails t unless got is want within a thousandth on each
// axis.
func expectPositionNear(t *testing.T, label string, got, want m.Vec2) {
	t.Helper()
	if !isNearVec2(got, want) {
		t.Errorf("%s: %v, want %v", label, got, want)
	}
}

// expectWidths fails t unless got are as many as want, each its width.
func expectWidths(t *testing.T, label string, got []float32, want ...float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: %d lines of %v, want %d", label, len(got), got, len(want))
		return
	}
	for line, width := range want {
		expectNear(t, label+": line width", got[line], width)
	}
}

// isNear answers whether got is want within a thousandth, neither not a
// number.
func isNear(got, want float32) bool {
	return math.Abs(float64(got-want)) <= 1e-3
}

// isNearVec2 answers whether got is want within a thousandth on each axis.
func isNearVec2(got, want m.Vec2) bool {
	return isNear(got.X, want.X) && isNear(got.Y, want.Y)
}

// isNearRect answers whether got is want within a thousandth on each side.
func isNearRect(got, want m.Rect) bool {
	return isNearVec2(m.Vec2{X: got.X, Y: got.Y}, m.Vec2{X: want.X, Y: want.Y}) && isNearVec2(m.Vec2{X: got.Width, Y: got.Height}, m.Vec2{X: want.Width, Y: want.Height})
}

func TestAnObjectOfNoRuneOrARuneLaidOutTwicePanics(t *testing.T) {
	set := loadDefaultFont(t, "")
	for name, objects := range map[string][]font.Object{
		"past the runes": {{Rune: 2}},
		"before them":    {{Rune: -1}},
		"laid out twice": {{Rune: 1}, {Rune: 1, Width: 5}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("objects %s laid out without a panic, want one", name)
				}
			}()
			var shaper font.Shaper
			shaper.LayOut(font.Input{Set: set, Runes: []rune("ab"), Objects: objects, Em: 10})
		}()
	}
}
