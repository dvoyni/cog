package internal

import (
	"image"
	"image/color"
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Font is one font baked at one pixel size: its face, the glyphs rasterized from
// it so far, and its line height in those pixels. It is what the face cache
// holds, and it is held by pointer because Glyphs is a mutable per-face memo.
//
// A face is also the head of a fallback chain. Glyphs holds what text set in
// this face draws, whichever font on the chain a glyph was rasterized from, and
// owners remembers which font that was, so a character is looked up along the
// chain once per face and language rather than once per frame.
type Font struct {
	Face       font.Face
	Glyphs     map[GlyphKey]Glyph
	LineHeight float32

	path   string
	px     int
	source *opentype.Font
	buf    sfnt.Buffer
	owners map[GlyphKey]*Font
}

// GlyphKey names one memoised glyph of a face: the character, and the language
// of the $[language]{...} span it was set in, empty outside one. The language is
// part of the key because it picks the fallback font, and a Han character drawn
// for Japanese is a different glyph from the same character drawn for Chinese.
type GlyphKey struct {
	Character rune
	Language  string
}

// has reports whether the font maps character to a glyph of its own. A font
// answers a character it lacks with glyph 0, its missing-glyph box, rather than
// with a failure, so this is the only way to tell.
func (f *Font) has(character rune) bool {
	index, err := f.source.GlyphIndex(&f.buf, character)
	return err == nil && index != 0
}

// Glyph is one rasterized rune: where it sits in the glyph atlas, its offset
// from the pen, and its advance. A rune with no pixels, such as a space, is not
// Visible and has no atlas entry.
type Glyph struct {
	Entry   AtlasEntry
	Offset  m.Vec2
	Advance float32
	Visible bool
}

// ResolveFontPath reads an empty path as a request for the built-in default
// font, which is what makes debug text free of assets and a zero-value ui.Font
// legible. Only the empty path is substituted: a named font that is missing or
// unparseable keeps failing, so a broken asset stays visible rather than
// quietly rendering in a different typeface.
func ResolveFontPath(path string) string {
	if path == "" {
		return DefaultFontPath
	}
	return path
}

// GlyphFonts is what resolving a character to the font that draws it needs
// inside one handler: the Lookup holding the chain and the faces, and the kernel
// and filesystem a fallback face is baked through. It is built per handler and
// never stored.
type GlyphFonts struct {
	k      kernel.Kernel
	fsys   fs.FS
	lookup *Lookup
}

// faceFor returns the face that draws character in text set in face: face
// itself when it has the character, and otherwise the first font of the chain
// language selects that does, baked at face's size. A character no font on the
// chain has is drawn by face, as its missing-glyph box.
//
// The answer is memoised on face, so it holds until the face is freed. A changed
// fallback language therefore reaches characters already drawn only once the
// glyphs are cleared, which is what ClearGlyphsCmd is for.
func (g GlyphFonts) faceFor(face *Font, character rune, language string) *Font {
	key := GlyphKey{Character: character, Language: language}
	if owner, ok := face.owners[key]; ok {
		return owner
	}
	owner := face
	if !face.has(character) && g.lookup != nil {
		for _, path := range g.lookup.fallbackChain(language) {
			if path == face.path {
				continue
			}
			if candidate := g.lookup.font(g.k, path, face.px, g.fsys); candidate != nil && candidate.has(character) {
				owner = candidate
				break
			}
		}
	}
	face.owners[key] = owner
	return owner
}

// LoadGlyph returns one rune of a baked face, set in language, rasterizing it
// into the glyph atlas on first use from whichever font of the chain draws it.
//
// A glyph from a fallback font is rasterized against face's ascent, not its own,
// so every glyph of a line sits on face's baseline.
//
// The glyph side is a packer with no table: face.Glyphs is the index, and always
// was. What the packer places is memoised there and nowhere else.
func LoadGlyph(fonts GlyphFonts, character rune, language string, face *Font, resources *gfx.ResourceQueue) (Glyph, bool) {
	key := GlyphKey{Character: character, Language: language}
	if glyph, ok := face.Glyphs[key]; ok {
		return glyph, true
	}
	owner := fonts.faceFor(face, character, language)
	dot := fixed.Point26_6{Y: face.Face.Metrics().Ascent}
	bounds, mask, maskPoint, advance, ok := owner.Face.Glyph(dot, character)
	if !ok {
		face.Glyphs[key] = Glyph{}
		return Glyph{}, false
	}
	glyph := Glyph{
		Offset:  m.Vec2{X: float32(bounds.Min.X), Y: float32(bounds.Min.Y)},
		Advance: float32(advance) / 64,
	}
	width, height := bounds.Dx(), bounds.Dy()
	if width > 0 && height > 0 {
		coverage := image.NewAlpha(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				_, _, _, alpha := mask.At(maskPoint.X+x, maskPoint.Y+y).RGBA()
				coverage.SetAlpha(x, y, color.Alpha{A: uint8(alpha >> 8)})
			}
		}
		pixels := make([]byte, width*height*4)
		for i := 0; i < width*height; i++ {
			pixels[i*4+0] = 255
			pixels[i*4+1] = 255
			pixels[i*4+2] = 255
			pixels[i*4+3] = coverage.Pix[i]
		}
		entry, refusal := fonts.lookup.glyphPacker.insert(insertion{
			pixels: pixels, width: width, height: height, padding: 1,
		}, resources)
		if refusal != packPlaced {
			return Glyph{}, false
		}
		glyph.Entry = entry
		glyph.Visible = true
	}
	face.Glyphs[key] = glyph
	return glyph, true
}

// pairKern is the kerning between two adjacent characters drawn by owner and
// next. Kerning is a table inside one font, so a pair that crosses from one
// font of the chain to another has none.
func pairKern(owner, next *Font, previous, character rune) float32 {
	if owner != next {
		return 0
	}
	return float32(next.Face.Kern(previous, character)) / 64
}

// GlyphLineWidth is the advance width of one line of a baked face, set in
// language, in its own pixels, rasterizing any glyph it has not met yet.
func GlyphLineWidth(fonts GlyphFonts, face *Font, text, language string, resources *gfx.ResourceQueue) float32 {
	var width float32
	var previous rune
	var previousOwner *Font
	for _, character := range text {
		glyph, ok := LoadGlyph(fonts, character, language, face, resources)
		if !ok {
			continue
		}
		owner := fonts.faceFor(face, character, language)
		if previousOwner != nil {
			width += pairKern(previousOwner, owner, previous, character)
		}
		width += glyph.Advance
		previous, previousOwner = character, owner
	}
	return width
}

// MeasureLine returns the logical advance width of one line set in language,
// kerning included, each character measured in the font of the chain that draws
// it.
func MeasureLine(fonts GlyphFonts, face *Font, text, language string) float32 {
	var width float32
	var previous rune
	var previousOwner *Font
	for _, character := range text {
		owner := fonts.faceFor(face, character, language)
		if previousOwner != nil {
			width += pairKern(previousOwner, owner, previous, character)
		}
		if advance, ok := owner.Face.GlyphAdvance(character); ok {
			width += float32(advance) / 64
		}
		previous, previousOwner = character, owner
	}
	return width
}
