package canvas

// Font is a font file in a font set and the options it is drawn with,
// loaded when its set is made.
type Font struct {
	// Path is the font file's path in platform's FS: a TrueType or OpenType
	// font, or a collection of them, .ttc or .otc. An empty Path is canvas's
	// built-in JetBrains Mono.
	Path string
	// Index is the font of a collection drawn, zero meaning its first; a
	// file of one font holds only that.
	Index uint32
	// Variations are a variable font's axis settings, zero meaning its
	// default instance: a zero Variation sets no axis, and an axis no
	// Variation sets keeps its default.
	Variations [4]Variation
	// FieldEm is the em, in field texels, the font's glyphs are made
	// distance fields at, zero meaning Config.GlyphFieldEm: a font of thin
	// strokes, such as CJK's, takes a larger one, so a stroke spans two
	// texels. FieldRange is how far a field reaches across a glyph's edge,
	// half outside it and half inside, in field texels, zero meaning
	// Config.GlyphFieldRange: half of it bounds an outline's width.
	FieldEm    int
	FieldRange int
}

// Variation is one axis setting of a variable font, its Value in the axis's
// design units: Variation{Axis: canvas.AxisTagFromName("wght"), Value: 700}.
// A Variation of the zero Axis and a non-zero Value names no axis, and is
// ignored, reported once for its font's path as ReportKindVariationIgnored
// when its set is made.
type Variation struct {
	Axis  AxisTag
	Value float32
}
