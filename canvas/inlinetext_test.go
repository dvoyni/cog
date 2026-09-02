package canvas

import (
	"math"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/storage"
	"golang.org/x/image/font/gofont/goregular"
)

func TestWrapInlineTextWrapsWordsAndCollapsesWhitespace(t *testing.T) {
	lines := wrapInlineText(parseInlineText("one   two\tthree"), 7, measuredInlineRunes)
	assertInlineLines(t, lines, "one two", "three")
}

func TestWrapInlineTextPreservesExplicitLines(t *testing.T) {
	lines := wrapInlineText(parseInlineText("one\n\ntwo"), 10, measuredInlineRunes)
	assertInlineLines(t, lines, "one", "", "two")
}

func TestWrapInlineTextMovesOversizedWordBeforeSplitting(t *testing.T) {
	lines := wrapInlineText(parseInlineText("one abcdef"), 4, measuredInlineRunes)
	assertInlineLines(t, lines, "one", "abcd", "ef")
}

func TestWrapInlineTextKeepsIconIndivisibleWithinWord(t *testing.T) {
	lines := wrapInlineText(parseInlineText("ab${icon.png}cd"), 3, measuredInlineRunes)
	assertInlineLines(t, lines, "ab${icon.png}", "cd")
}

func TestWrapInlineTextLetsOversizedUnitOverflow(t *testing.T) {
	measure := func(segments []inlineSegment) float32 {
		width := measuredInlineRunes(segments)
		for _, segment := range segments {
			if segment.icon {
				width += 4
			}
		}
		return width
	}
	lines := wrapInlineText(parseInlineText("${icon.png}a"), 3, measure)
	assertInlineLines(t, lines, "${icon.png}", "a")
}

func TestWrapInlineTextSplitsAtUnicodeRuneBoundaries(t *testing.T) {
	lines := wrapInlineText(parseInlineText("a界b"), 2, measuredInlineRunes)
	assertInlineLines(t, lines, "a界", "b")
}

func TestWrapInlineTextIgnoresInvalidWidths(t *testing.T) {
	for _, width := range []float32{0, -1, float32(math.NaN()), float32(math.Inf(1))} {
		lines := wrapInlineText(parseInlineText("one two"), width, measuredInlineRunes)
		assertInlineLines(t, lines, "one two")
	}
}

func measuredInlineRunes(segments []inlineSegment) float32 {
	var width float32
	for _, segment := range segments {
		if segment.icon {
			width++
			continue
		}
		width += float32(len([]rune(segment.text)))
	}
	return width
}

func assertInlineLines(t *testing.T, lines [][]inlineSegment, want ...string) {
	t.Helper()
	if len(lines) != len(want) {
		t.Fatalf("line count = %d, want %d (%q)", len(lines), len(want), want)
	}
	for index, line := range lines {
		var got string
		for _, segment := range line {
			if segment.icon {
				got += "${" + segment.text + "}"
				continue
			}
			got += segment.text
		}
		if got != want[index] {
			t.Fatalf("line %d = %q, want %q", index, got, want[index])
		}
	}
}

// Layout measures text with the face at its logical size while drawing rasterizes
// at the device size, and hinted advances do not agree between the two. Wrapping
// must follow the layout metrics, or a line arranged to exactly fit its measured
// width drops its last word onto a line the element was never sized for.
func TestWrapMeasureFollowsLayoutMetrics(t *testing.T) {
	const path = "font.ttf"
	const size = 18
	filesystem := storage.NewFileSystem("test", fstest.MapFS{path: &fstest.MapFile{Data: goregular.TTF}})
	fonts := newFontStore()
	logical := fonts.face(filesystem, path, size)
	raster := fonts.face(filesystem, path, size*2)
	if logical == nil || raster == nil {
		t.Fatal("test font could not be loaded")
	}

	const text = "a line that exactly fills its measured width"
	lines := parseInlineText(text)
	arranged := measureLine(logical, text)
	rasterized := func(segments []inlineSegment) float32 {
		var width float32
		for _, segment := range segments {
			width += measureLine(raster, segment.text) * 0.5
		}
		return width
	}
	if rasterized(lines[0]) <= arranged {
		t.Skip("test text no longer exercises rasterization drift for this font")
	}
	if got := wrapInlineText(lines, arranged, rasterized); len(got) != 2 {
		t.Fatalf("rasterized wrapping produced %d lines, want the 2 the drift causes", len(got))
	}

	wrap := (&Plugin{}).wrapMeasure(nil, nil, filesystem, fonts, path, size, rasterized)
	assertInlineLines(t, wrapInlineText(lines, arranged, wrap), text)
}
