package internal

import (
	"math"
	"testing"
)

func TestWrapInlineTextWrapsWordsAndCollapsesWhitespace(t *testing.T) {
	lines := WrapInlineText(ParseInlineText("one   two\tthree"), 7, measuredInlineRunes)
	assertInlineLines(t, lines, "one two", "three")
}

func TestWrapInlineTextPreservesExplicitLines(t *testing.T) {
	lines := WrapInlineText(ParseInlineText("one\n\ntwo"), 10, measuredInlineRunes)
	assertInlineLines(t, lines, "one", "", "two")
}

func TestWrapInlineTextMovesOversizedWordBeforeSplitting(t *testing.T) {
	lines := WrapInlineText(ParseInlineText("one abcdef"), 4, measuredInlineRunes)
	assertInlineLines(t, lines, "one", "abcd", "ef")
}

func TestWrapInlineTextKeepsIconIndivisibleWithinWord(t *testing.T) {
	lines := WrapInlineText(ParseInlineText("ab${icon.png}cd"), 3, measuredInlineRunes)
	assertInlineLines(t, lines, "ab${icon.png}", "cd")
}

func TestWrapInlineTextLetsOversizedUnitOverflow(t *testing.T) {
	measure := func(segments []InlineSegment) float32 {
		width := measuredInlineRunes(segments)
		for _, segment := range segments {
			if segment.Icon {
				width += 4
			}
		}
		return width
	}
	lines := WrapInlineText(ParseInlineText("${icon.png}a"), 3, measure)
	assertInlineLines(t, lines, "${icon.png}", "a")
}

func TestWrapInlineTextSplitsAtUnicodeRuneBoundaries(t *testing.T) {
	lines := WrapInlineText(ParseInlineText("a界b"), 2, measuredInlineRunes)
	assertInlineLines(t, lines, "a界", "b")
}

func TestWrapInlineTextIgnoresInvalidWidths(t *testing.T) {
	for _, width := range []float32{0, -1, float32(math.NaN()), float32(math.Inf(1))} {
		lines := WrapInlineText(ParseInlineText("one two"), width, measuredInlineRunes)
		assertInlineLines(t, lines, "one two")
	}
}

func measuredInlineRunes(segments []InlineSegment) float32 {
	var width float32
	for _, segment := range segments {
		if segment.Icon {
			width++
			continue
		}
		width += float32(len([]rune(segment.Text)))
	}
	return width
}

func assertInlineLines(t *testing.T, lines [][]InlineSegment, want ...string) {
	t.Helper()
	if len(lines) != len(want) {
		t.Fatalf("line count = %d, want %d (%q)", len(lines), len(want), want)
	}
	for index, line := range lines {
		var got string
		for _, segment := range line {
			if segment.Icon {
				got += "${" + segment.Text + "}"
				continue
			}
			got += segment.Text
		}
		if got != want[index] {
			t.Fatalf("line %d = %q, want %q", index, got, want[index])
		}
	}
}

func TestParseInlineTextReadsAnIconScaleSuffix(t *testing.T) {
	lines := ParseInlineText("a${icon.png|1.5}b${icon.png}")
	if len(lines) != 1 || len(lines[0]) != 4 {
		t.Fatalf("segments = %+v, want a, icon, b, icon", lines)
	}
	scaled, plain := lines[0][1], lines[0][3]
	if !scaled.Icon || scaled.Text != "icon.png" || scaled.IconScale() != 1.5 {
		t.Fatalf("scaled icon = %+v, want icon.png at 1.5", scaled)
	}
	if !plain.Icon || plain.Text != "icon.png" || plain.IconScale() != 1 {
		t.Fatalf("plain icon = %+v, want icon.png at 1", plain)
	}
}

// A suffix that is no positive number is left on the path, so the lookup
// reports the token as a missing sprite instead of it drawing at some size.
func TestParseInlineTextKeepsAnInvalidScaleOnThePath(t *testing.T) {
	for _, token := range []string{"icon.png|", "icon.png|big", "icon.png|0", "icon.png|-2", "icon.png|NaN", "icon.png|Inf"} {
		lines := ParseInlineText("${" + token + "}")
		segment := lines[0][0]
		if segment.Text != token || segment.IconScale() != 1 {
			t.Fatalf("%q parsed as %+v, want the whole token as the path at scale 1", token, segment)
		}
	}
}

func TestWrapInlineTextKeepsAnIconsScale(t *testing.T) {
	lines := WrapInlineText(ParseInlineText("one ${icon.png|2}two"), 4, measuredInlineRunes)
	segment := lines[1][0]
	if !segment.Icon || segment.IconScale() != 2 {
		t.Fatalf("wrapped icon = %+v, want its scale of 2 kept", segment)
	}
}
