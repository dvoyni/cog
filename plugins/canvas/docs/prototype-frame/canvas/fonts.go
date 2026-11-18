package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/kernel"
	"prototype.invalid/canvasframe/platform"
)

// Font is a font file in a font set and the options it is drawn with.
type Font struct {
	Path string
	// Bitmap is a bitmap font's pixel size, for pixel fonts; zero draws the
	// font's glyphs as distance fields.
	Bitmap float32
	// FieldEm and FieldRange are its distance field's em and range in pixels,
	// zero meaning 48 and 16. A CJK font takes a larger em, such as 64.
	FieldEm, FieldRange float32
	// Index is a collection's face, zero meaning its first.
	Index uint32
	// Variations are a variable font's axis settings, zero meaning its
	// default instance.
	Variations [4]Variation
}

// Variation is one axis setting of a variable font:
// Variation{Tag: canvas.Tag("wght"), Value: 700}.
type Variation struct {
	Tag   [4]byte
	Value float32
}

// Tag answers an OpenType tag's four bytes, such as "wght"; a name of other
// than four bytes is reported once and answers the zero tag.
func Tag(name string) [4]byte {
	var tag [4]byte
	if len(name) != 4 {
		tables.report("Tag: " + name + " is not four bytes")
		return tag
	}
	copy(tag[:], name)
	return tag
}

// LanguageFonts are the fonts serving Languages, tags matched exactly: one
// font may serve several languages.
type LanguageFonts struct {
	Languages []string
	Fonts     []Font
}

// FontSetDescriptor describes a font set: a default font, and the fonts
// serving each language. A character is drawn by the current language's
// fonts that have it, then by the default font, then as its missing-glyph
// box.
type FontSetDescriptor struct {
	Label     string
	Default   Font
	Languages []LanguageFonts
}

// IconDescriptor is a picture text places like a glyph.
type IconDescriptor struct {
	Image Image
	Frame uint32
	// Scale multiplies the font's cap height, zero meaning 1.
	Scale float32
	// Offset moves its baseline, in em.
	Offset float32
	// Tinted takes the span's tint; otherwise the icon takes the run's alpha
	// alone.
	Tinted bool
}

// Fonts is canvas's resource of font sets, the current language, icons and
// colours: an interface of its reads alone, so a task holding its read lock
// cannot make a set.
type Fonts interface {
	// Language answers the current language.
	Language() string
	// Layout answers text shaped, wrapped and measured, as DrawText would draw
	// it: measuring and drawing read the same layout.
	Layout(text Text) Layout
	// Preload makes the glyphs of text in set ahead of their first draw.
	Preload(set FontSet, text string)
	// Metrics answers the measures, at size, of set's font serving language,
	// empty meaning the current one.
	Metrics(set FontSet, size float32, language string) Metrics
}

// WriteFonts is Fonts' writing side, answered only from a write handle.
type WriteFonts interface {
	Fonts
	// MakeFontSet makes a set and loads every font file it names, now, in
	// the calling task: at a start or behind a loading screen.
	MakeFontSet(descriptor FontSetDescriptor) FontSet
	// DestroyFontSet destroys set; its glyphs leave the cache.
	DestroyFontSet(set FontSet)
	// SetLanguage sets the current language.
	SetLanguage(tag string)
	// RegisterIcon registers icon under name, loading and packing its image
	// now; registering a name again replaces it everywhere it is drawn.
	RegisterIcon(name string, icon IconDescriptor)
	// RegisterColor names color for markup's \c:name{…}.
	RegisterColor(name string, color m.Color)
}

// ElevateFonts answers Fonts' writing side from a write handle on it, and a
// read handle on platform's FS, which making a set reads.
func ElevateFonts(fonts kernel.Write[Fonts], files kernel.Read[platform.FS]) WriteFonts {
	return fonts.Get().(*fontsResource)
}

// Metrics are a font's measures at a size, in world units.
type Metrics struct {
	Ascent, Descent, LineGap, LineHeight, XHeight, CapHeight float32
}
