package canvas

import "github.com/dvoyni/cog/libs/m"

// Text is a run of text: its string, the font set and size it is shaped
// in, and how its lines wrap, align and are spaced, which FontLibrary.Layout
// lays it out by; and, for DrawText, an entry drawing its glyphs through a
// transform about a pivot of its layout's box, as a sprite draws its image.
// It is a literal, like every other entry: canvas.Text{String: name,
// Transform: m.Translation2x3(x, y), Pivot: canvas.PivotBottom, Layer: l}.
type Text struct {
	// String is the run's text. A newline in it is a hard break, as are a
	// carriage return, a line or paragraph separator and Unicode's other
	// mandatory breaks (UAX #14), and each paragraph between them takes its
	// own direction.
	String string
	// Spans, when given, are the run instead of its String: their texts and
	// icons end to end, shaped and wrapped as one paragraph or more, so a
	// coloured word wraps with its sentence. Each is drawn in its own tint,
	// and DrawText copies them, so a caller may reuse the slice once it
	// returns.
	Spans []Span
	// Font is the font set the run is shaped in, zero meaning the default
	// set. A set canvas does not hold is laid out as the default set,
	// reported once as ReportKindFontSetNotHeld.
	Font FontSet
	// Size is the run's em in world units, zero meaning Config.TextSize.
	Size float32
	// Language is the tag of the language the run is written in, matched
	// exactly, zero meaning the current language: a character is drawn by
	// the first of its set's fonts for the language that has it, else by
	// the set's default font, and the language goes to the shaper, so a
	// font draws its own forms of it. A name in another language takes its
	// own.
	Language string
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
	// Transform maps the run's own space, its layout's box from its top
	// left, y pointing down, about Pivot, into the world, a zero one read
	// as the identity, so a run turns and scales as a sprite does.
	Transform m.Mat2x3
	// Pivot is the point of the layout's box the run is placed, turned and
	// scaled about, zero meaning its centre, its Texels in em: PivotBottom
	// stands a label over a unit, and PivotTopLeft lays out a UI.
	Pivot Pivot
	// Tint is the fill's colour: straight alpha, premultiplied by canvas,
	// zero meaning white, and the colour of each span of no tint of its
	// own. An outline's colour is its profile's, faded by
	// the tint's alpha alone.
	Tint m.Color
	// Material is the 2D material the run's glyphs are drawn with, zero
	// meaning TextMaterial, sampling the glyph atlas: each glyph is a quad
	// of its distance field, its instance data's width the field's spread
	// in field texels, half its font's field range, and an icon a quad of
	// its region of the sRGB sprite atlas, canvas_sprite_atlas, its width
	// zero.
	Material Material
	// Clip is a rectangle of the world outside which the run draws nothing,
	// zero meaning none, as a Shape's Clip is.
	Clip m.Rect
	// Filter is how the glyphs' fields are read between their texels, zero
	// meaning their font's, linear for a distance field, never
	// Config.DefaultFilter, since nearest filtering breaks a field.
	Filter Filter
	// Layer and Order are the run's sort key, as a Shape's are: its glyphs
	// draw together, in order, at its place among the frame's entries.
	Layer int32
	Order int32
}

// Span is a part of a run of text: its text, drawn in its tint, or an icon.
// canvas.Span{Text: "fire", Tint: red} colours a word, and
// canvas.Span{Icon: "button-a"} places the icon registered as "button-a".
type Span struct {
	// Text is the span's text, shaped with the spans beside it, so a
	// ligature or a word may run across them.
	Text string
	// Tint is the colour of the span's text, and of its icon when the icon
	// is Tinted: straight alpha, zero meaning the run's Tint.
	Tint m.Color
	// Icon, when set, makes the span the icon registered under it with
	// WriteFontLibrary.RegisterIcon, its Text unread: the font's cap height
	// times its Scale tall, its width by its image's aspect, its bottom on
	// the baseline moved by its Offset, and a line breaks around it as
	// around a letter, never inside it. A name no icon is registered under
	// draws the default font's missing-glyph box, reported once for the
	// name as ReportKindIconNotRegistered.
	Icon string
}

// TextMaterial is canvas's text material, a run's when its Material is zero:
// each glyph's distance field filled with the run's tint, small text, whose
// field spans more than 2 texels a pixel, read at four points a pixel and
// its unturned glyphs' pens placed on whole pixels, over a Profile,
// its argument, drawn under the fill in the same entry, so each label's
// outline lies under its own fill and every label batches: an outline,
// halo, glow or drop shadow, its distances in the font's field texels, half
// the font's field range reaching furthest, and its colour faded by the
// tint's alpha alone; its Spread is ignored, the field's own read instead.
// Its zero Profile draws the fill alone. An icon is drawn from the sRGB
// sprite atlas, which it binds beside the glyph atlas, so a run with icons
// is one batch, multiplied by its tint and with no profile. canvas holds it, so it is never
// released and its parameters are canvas's.
var TextMaterial = Material{index: 2}

// textMaterialDescriptor is what canvas makes TextMaterial of.
var textMaterialDescriptor = MaterialDescriptor{
	Label:              "canvas text",
	Shader:             textShaderPath,
	VertexEntryPoint:   textVertexEntryPoint,
	FragmentEntryPoint: textFragmentEntryPoint,
	Argument:           ArgumentOf[Profile](),
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
