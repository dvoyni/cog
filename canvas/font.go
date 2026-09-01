package canvas

import (
	"fmt"
	"github.com/dvoyni/cog/m"
	"image"
	"image/color"
	"io/fs"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/storage"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type fontKey struct {
	path string
	px   int
}

type canvasFont struct {
	face       font.Face
	glyphs     map[rune]canvasGlyph
	lineHeight float32
}

type fontStore struct {
	fonts   map[fontKey]*canvasFont
	sources map[string]*opentype.Font
}

func newFontStore() *fontStore {
	return &fontStore{fonts: map[fontKey]*canvasFont{}, sources: map[string]*opentype.Font{}}
}

type canvasGlyph struct {
	entry   atlasEntry
	offset  m.Vec2
	advance float32
	visible bool
}

func (s *fontStore) face(filesystem storage.ReadFS, path string, px int) *canvasFont {
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
	result := &canvasFont{
		face: face, glyphs: map[rune]canvasGlyph{},
		lineHeight: float32(face.Metrics().Height.Ceil()),
	}
	s.fonts[key] = result
	return result
}

func (p *Plugin) glyph(atlas *atlas, fontPath string, px int, character rune, face *canvasFont, resources *gfx.ResourceQueue) (canvasGlyph, bool) {
	if glyph, ok := face.glyphs[character]; ok {
		return glyph, true
	}
	dot := fixed.Point26_6{Y: face.face.Metrics().Ascent}
	bounds, mask, maskPoint, advance, ok := face.face.Glyph(dot, character)
	if !ok {
		face.glyphs[character] = canvasGlyph{}
		return canvasGlyph{}, false
	}
	glyph := canvasGlyph{
		offset:  m.Vec2{X: float32(bounds.Min.X), Y: float32(bounds.Min.Y)},
		advance: float32(advance) / 64,
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
			return canvasGlyph{}, false
		}
		glyph.entry = entry
		glyph.visible = true
	}
	face.glyphs[character] = glyph
	return glyph, true
}

func (p *Plugin) glyphLineWidth(atlas *atlas, fontPath string, px int, face *canvasFont, text string, resources *gfx.ResourceQueue) float32 {
	var width float32
	var previous rune
	first := true
	for _, character := range text {
		glyph, ok := p.glyph(atlas, fontPath, px, character, face, resources)
		if !ok {
			continue
		}
		if !first {
			width += float32(face.face.Kern(previous, character)) / 64
		}
		width += glyph.advance
		previous = character
		first = false
	}
	return width
}
