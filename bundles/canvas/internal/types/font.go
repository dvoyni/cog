package types

import (
	"fmt"
	"image"
	"image/color"
	"io/fs"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type fontKey struct {
	path string
	px   int
}

// Font is one font baked at one pixel size: its face, the glyphs rasterized from
// it so far, and its line height in those pixels.
type Font struct {
	Face       font.Face
	Glyphs     map[rune]Glyph
	LineHeight float32
}

// FontStore caches parsed font sources by path and baked faces by path and
// pixel size.
type FontStore struct {
	fonts   map[fontKey]*Font
	sources map[string]*opentype.Font
}

// NewFontStore builds an empty store.
func NewFontStore() *FontStore {
	return &FontStore{fonts: map[fontKey]*Font{}, sources: map[string]*opentype.Font{}}
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

// Face bakes (or reuses) the font at path at px pixels, parsing its source on
// first use. It returns nil when the file cannot be read or parsed.
func (s *FontStore) Face(filesystem storage.FileSystem, path string, px int) *Font {
	key := fontKey{path: path, px: px}
	if cached, ok := s.fonts[key]; ok {
		return cached
	}
	parsed, ok := s.sources[path]
	if !ok {
		data, err := fs.ReadFile(filesystem, path)
		if err != nil {
			return nil
		}
		parsed, err = opentype.Parse(data)
		if err != nil {
			return nil
		}
		s.sources[path] = parsed
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{
		Size: float64(px), DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		return nil
	}
	result := &Font{
		Face: face, Glyphs: map[rune]Glyph{},
		LineHeight: float32(face.Metrics().Height.Ceil()),
	}
	s.fonts[key] = result
	return result
}

// LoadGlyph returns one rune of a baked face, rasterizing it into the glyph atlas
// on first use.
func LoadGlyph(atlas *Atlas, fontPath string, px int, character rune, face *Font, resources *gfx.ResourceQueue) (Glyph, bool) {
	if glyph, ok := face.Glyphs[character]; ok {
		return glyph, true
	}
	dot := fixed.Point26_6{Y: face.Face.Metrics().Ascent}
	bounds, mask, maskPoint, advance, ok := face.Face.Glyph(dot, character)
	if !ok {
		face.Glyphs[character] = Glyph{}
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
		key := fmt.Sprintf("\x01%s\x00%d\x00%d", fontPath, px, character)
		entry, placed := atlas.insert(key, atlasGlyph, width, height, pixels, 1, false, resources)
		if !placed {
			return Glyph{}, false
		}
		glyph.Entry = entry
		glyph.Visible = true
	}
	face.Glyphs[character] = glyph
	return glyph, true
}

// GlyphLineWidth is the advance width of one line of a baked face in its own
// pixels, rasterizing any glyph it has not met yet.
func GlyphLineWidth(atlas *Atlas, fontPath string, px int, face *Font, text string, resources *gfx.ResourceQueue) float32 {
	var width float32
	var previous rune
	first := true
	for _, character := range text {
		glyph, ok := LoadGlyph(atlas, fontPath, px, character, face, resources)
		if !ok {
			continue
		}
		if !first {
			width += float32(face.Face.Kern(previous, character)) / 64
		}
		width += glyph.Advance
		previous = character
		first = false
	}
	return width
}

// ClearFontFaces closes baked faces but keeps parsed sources for re-baking.
func ClearFontFaces(fonts *FontStore) {
	for _, cached := range fonts.fonts {
		_ = cached.Face.Close()
	}
	clear(fonts.fonts)
}
