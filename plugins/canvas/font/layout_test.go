package font_test

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas/font"
)

func TestALatinRunIsMeasuredByItsFontsAdvancesOnItsFirstBaseline(t *testing.T) {
	set := loadDefaultFont(t, "")
	metrics := set.Default().Metrics(10)
	var shaper font.Shaper
	layout := shaper.LayOut(font.Input{Set: set, Runes: []rune("hello world"), Em: 10})
	expectWidths(t, "hello world", layout.LineWidths, 11*builtInAdvance)
	expectPositionNear(t, "its size", layout.Size, m.Vec2{X: 11 * builtInAdvance, Y: metrics.LineHeight})
	expectNear(t, "its baseline", layout.Baseline, metrics.Ascent)
	expectPositionNear(t, "w's position", layout.ClusterPositions[6], m.Vec2{X: 6 * builtInAdvance, Y: metrics.Ascent})
	expectPositionNear(t, "the end's position", layout.ClusterPositions[11], m.Vec2{X: 11 * builtInAdvance, Y: metrics.Ascent})
}

func TestANewlineACarriageReturnAndNewlineAndALineSeparatorAreEachOneHardBreak(t *testing.T) {
	set := loadDefaultFont(t, "")
	metrics := set.Default().Metrics(10)
	var shaper font.Shaper
	layout := shaper.LayOut(font.Input{Set: set, Runes: []rune("ab\ncde\n"), Em: 10})
	expectWidths(t, "ab, cde and an empty line", layout.LineWidths, 2*builtInAdvance, 3*builtInAdvance, 0)
	expectPositionNear(t, "its size", layout.Size, m.Vec2{X: 3 * builtInAdvance, Y: 3 * metrics.LineHeight})
	expectPositionNear(t, "the first newline's position", layout.ClusterPositions[2], m.Vec2{X: 2 * builtInAdvance, Y: metrics.Ascent})
	expectPositionNear(t, "c's position", layout.ClusterPositions[3], m.Vec2{Y: metrics.Ascent + metrics.LineHeight})
	expectPositionNear(t, "the end's position", layout.ClusterPositions[7], m.Vec2{Y: metrics.Ascent + 2*metrics.LineHeight})

	layout = shaper.LayOut(font.Input{Set: set, Runes: []rune("ab\r\nc de"), Em: 10})
	expectWidths(t, "ab, c and de", layout.LineWidths, 2*builtInAdvance, builtInAdvance, 2*builtInAdvance)
	expectPositionNear(t, "the carriage return and newline's position", layout.ClusterPositions[2], m.Vec2{X: 2 * builtInAdvance, Y: metrics.Ascent})
	expectPositionNear(t, "c's position, one cluster after it", layout.ClusterPositions[3], m.Vec2{Y: metrics.Ascent + metrics.LineHeight})
	expectPositionNear(t, "the line separator's position", layout.ClusterPositions[4], m.Vec2{X: builtInAdvance, Y: metrics.Ascent + metrics.LineHeight})
	expectPositionNear(t, "the end's position", layout.ClusterPositions[7], m.Vec2{X: 2 * builtInAdvance, Y: metrics.Ascent + 2*metrics.LineHeight})
}

func TestLineHeightSpacesTrackedLinesWithTheLeadingSplitAboveAndBelow(t *testing.T) {
	set := loadDefaultFont(t, "")
	metrics := set.Default().Metrics(10)
	baseline := (2*metrics.LineHeight-metrics.Ascent-metrics.Descent)/2 + metrics.Ascent
	var shaper font.Shaper
	layout := shaper.LayOut(font.Input{Set: set, Runes: []rune("abc\nd"), Em: 10, LineHeight: 2, Tracking: 0.1})
	expectWidths(t, "abc tracked, then d", layout.LineWidths, 3*builtInAdvance+2, builtInAdvance)
	expectPositionNear(t, "its size", layout.Size, m.Vec2{X: 3*builtInAdvance + 2, Y: 4 * metrics.LineHeight})
	expectPositionNear(t, "b's position, half a tracking past a", layout.ClusterPositions[1], m.Vec2{X: builtInAdvance + 0.5, Y: baseline})
	expectPositionNear(t, "d's position", layout.ClusterPositions[4], m.Vec2{Y: baseline + 2*metrics.LineHeight})
}

func TestAnArabicRunIsShapedJoinedLaidOutFromTheRightAndWrappedAtSpaces(t *testing.T) {
	set := loadDefaultFont(t, "notosansarabic.ttf")
	metrics := set.Default().Metrics(10)
	var shaper font.Shaper
	isolated := shaper.LayOut(font.Input{Set: set, Runes: []rune("ب"), Em: 10}).LineWidths[0]
	if joined := shaper.LayOut(font.Input{Set: set, Runes: []rune("ببب"), Em: 10}).LineWidths[0]; !(joined < 3*isolated) {
		t.Errorf("three behs joined are %v wide, want narrower than three isolated, %v", joined, 3*isolated)
	}
	const word = "كلمة"
	words := []rune(word + " " + word + " " + word)
	wordWidth := shaper.LayOut(font.Input{Set: set, Runes: []rune(word), Em: 10}).LineWidths[0]
	layout := shaper.LayOut(font.Input{Set: set, Runes: words, Em: 10})
	if len(layout.LineWidths) != 1 {
		t.Fatalf("three words unwrapped are %d lines, want 1", len(layout.LineWidths))
	}
	width := layout.LineWidths[0]
	expectPositionNear(t, "the first word's first letter's position, at the right", layout.ClusterPositions[0], m.Vec2{X: width, Y: metrics.Ascent})
	expectPositionNear(t, "the first word's end, a word's width left of the right", layout.ClusterPositions[4], m.Vec2{X: width - wordWidth, Y: metrics.Ascent})
	for cluster := range 3 {
		if !(layout.ClusterPositions[cluster+1].X < layout.ClusterPositions[cluster].X) {
			t.Errorf("letter %d at %v, want left of letter %d at %v", cluster+1, layout.ClusterPositions[cluster+1], cluster, layout.ClusterPositions[cluster])
		}
	}
	// Wrapped at a word and a half, each word is a line of its own, aligned
	// at its start, the right, or at its end, the left.
	wrapWidth := wordWidth * 1.5
	for align, right := range map[font.Align]float32{font.AlignStart: wrapWidth, font.AlignEnd: wordWidth} {
		layout := shaper.LayOut(font.Input{Set: set, Runes: words, Em: 10, WrapWidth: wrapWidth, Align: align})
		expectWidths(t, "three words wrapped", layout.LineWidths, wordWidth, wordWidth, wordWidth)
		for line, cluster := range []int{0, 5, 10} {
			expectPositionNear(t, "a line's first letter's position", layout.ClusterPositions[cluster], m.Vec2{X: right, Y: metrics.Ascent + float32(line)*metrics.LineHeight})
		}
	}
}

func TestDigitsBeforeTheFirstStrongRightToLeftLetterStillRunRightToLeft(t *testing.T) {
	// The digits come first, but beh is the first strong character, so the
	// paragraph runs right to left, the digits at its right.
	set := loadDefaultFont(t, "notosansarabic.ttf")
	ascent := set.Default().Metrics(10).Ascent
	var shaper font.Shaper
	digitWidth := shaper.LayOut(font.Input{Set: set, Runes: []rune("1"), Em: 10}).LineWidths[0]
	layout := shaper.LayOut(font.Input{Set: set, Runes: []rune("123 ب"), Em: 10})
	for digit := range 3 {
		expectPositionNear(t, "a digit's position before beh, from the right", layout.ClusterPositions[digit], m.Vec2{X: layout.LineWidths[0] - float32(3-digit)*digitWidth, Y: ascent})
	}
}

func TestAJapaneseRunBreaksBetweenIdeographsNeverBeforeClosingPunctuation(t *testing.T) {
	set := loadDefaultFont(t, "notosansjp.ttf")
	metrics := set.Default().Metrics(10)
	var shaper font.Shaper
	for align, offset := range map[font.Align]float32{font.AlignStart: 0, font.AlignCenter: 10} {
		layout := shaper.LayOut(font.Input{Set: set, Runes: []rune("日本語日本語"), Em: 10, WrapWidth: 40, Align: align})
		expectWidths(t, "six ideographs wrapped at 4", layout.LineWidths, 40, 20)
		expectPositionNear(t, "the fifth ideograph's position, starting the second line", layout.ClusterPositions[4], m.Vec2{X: offset, Y: metrics.Ascent + metrics.LineHeight})
	}
	// Six ideographs fit the line, but the full stop after the sixth may not
	// start the next, so the sixth goes down with it.
	layout := shaper.LayOut(font.Input{Set: set, Runes: []rune("日本語の文章。次の文"), Em: 10, WrapWidth: 60})
	expectWidths(t, "the sentence wrapped", layout.LineWidths, 50, 50)
	expectPositionNear(t, "章's position, starting the second line", layout.ClusterPositions[5], m.Vec2{Y: metrics.Ascent + metrics.LineHeight})
	expectPositionNear(t, "the full stop's position, after 章", layout.ClusterPositions[6], m.Vec2{X: 10, Y: metrics.Ascent + metrics.LineHeight})
}
