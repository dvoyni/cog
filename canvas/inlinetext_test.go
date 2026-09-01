package canvas

import (
	"math"
	"testing"
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
