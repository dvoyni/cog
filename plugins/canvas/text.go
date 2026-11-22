package canvas

// Text is a run of text: its string, the font set and size it is shaped
// in, and how its lines wrap, align and are spaced. FontLibrary.Layout lays
// it out.
type Text struct {
	// String is the run's text. A newline in it is a hard break, as are a
	// carriage return, a line or paragraph separator and Unicode's other
	// mandatory breaks (UAX #14), and each paragraph between them takes its
	// own direction.
	String string
	// Font is the font set the run is shaped in, zero meaning the default
	// set. A set canvas does not hold is laid out as the default set,
	// reported once as ReportKindFontSetNotHeld.
	Font FontSet
	// Size is the run's em in world units, zero meaning Config.TextSize.
	Size float32
	// WrapWidth is the width, in world units, its lines break at, on
	// Unicode's line-break opportunities (UAX #14), so text in Chinese or
	// Japanese breaks between ideographs and never starts a line with
	// closing punctuation; a word too long for a line of its own breaks
	// between grapheme clusters. Zero means no wrapping.
	WrapWidth float32
	// Align is how the lines align in the layout's box.
	Align TextAlign
	// LineHeight multiplies the font's line height, its ascent, descent and
	// line gap, zero meaning 1; the leading it adds or takes is split above
	// and below each line.
	LineHeight float32
	// Tracking is the space added between letters, in em, zero meaning none.
	Tracking float32
}

// TextAlign is how a run's lines align in its layout's box. A paragraph's
// direction is its first strong character's, so a paragraph in Arabic or
// Hebrew starts at the right.
type TextAlign uint8

const (
	// TextAlignStart aligns each line to its paragraph's start: the left of
	// a left-to-right paragraph, the right of a right-to-left one.
	TextAlignStart TextAlign = iota
	// TextAlignCenter centres each line.
	TextAlignCenter
	// TextAlignEnd aligns each line to its paragraph's end.
	TextAlignEnd
)
